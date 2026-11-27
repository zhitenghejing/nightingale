package remoteaccess

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
)

type ConnectionTestInput struct {
	Ident      string          `json:"ident"`
	Connection ConnectionInput `json:"connection"`
}
type ConnectionTestResult struct {
	OK         bool  `json:"ok"`
	ViaJump    bool  `json:"via_jump"`
	DurationMS int64 `json:"duration_ms"`
}

func authorizeDraft(tx *gorm.DB, actor int64, ident string, group int64) error {
	if err := access(tx, actor, group, PermConfigure, true); err != nil {
		return err
	}
	if ident != "" {
		_, groups, err := target(tx, ident)
		if err != nil {
			return err
		}
		if !slices.Contains(groups, group) {
			return denied()
		}
	}
	return nil
}

func validateDraftCredential(tx *gorm.DB, ident string, f ConnectionInput, isJump bool) error {
	if !f.deviceScoped {
		return nil
	}
	e := f.Target
	if isJump && f.Jump != nil {
		e = *f.Jump
	}
	if e.NewCredential != nil {
		return nil
	}
	var bound models.OpsSSHConnection
	if ident == "" || tx.First(&bound, "target_ident = ? AND group_id = ?", ident, f.GroupID).Error != nil || connectionProtocol(bound.Protocol) != connectionProtocol(f.Protocol) {
		return denied()
	}
	t, _, err := target(tx, ident)
	if err != nil || t.Id != bound.TargetID {
		return denied()
	}
	previous := view(bound)
	saved := previous.Target
	if isJump {
		if previous.Jump == nil {
			return denied()
		}
		saved = *previous.Jump
	}
	if e.CredentialID == "" || e.CredentialID != saved.CredentialID || e.Host != saved.Host || e.Port != saved.Port {
		return denied()
	}
	return nil
}
func (s *Store) draftEndpoint(tx *gorm.DB, actor, group int64, e EndpointInput) (Endpoint, error) {
	if err := validateEndpoint(e); err != nil {
		return Endpoint{}, err
	}
	var value secret
	if e.NewCredential != nil {
		if _, err := Permission(tx, actor, PermCredentials); err != nil {
			return Endpoint{}, err
		}
		if e.CredentialID != "" {
			return Endpoint{}, invalid("choose an existing credential or create a new one")
		}
		if e.NewCredential.Disabled != nil && *e.NewCredential.Disabled {
			return Endpoint{}, denied()
		}
		value = secret{Username: e.NewCredential.Username, Password: e.NewCredential.Password}
		if strings.TrimSpace(value.Username) == "" || value.Password == "" || len(value.Username) > 256 || len(value.Password) > 4096 || strings.ContainsAny(value.Username+value.Password, "\x00\r\n") {
			return Endpoint{}, invalid("SSH account and password are required")
		}
	} else {
		var c models.OpsCredential
		if err := tx.First(&c, "id = ?", e.CredentialID).Error; err != nil {
			return Endpoint{}, missing()
		}
		if c.GroupID != group || c.Disabled {
			return Endpoint{}, denied()
		}
		var err error
		value, err = s.secret(c)
		if err != nil {
			return Endpoint{}, err
		}
	}
	return Endpoint{Host: e.Host, Port: e.Port, Fingerprint: e.Fingerprint, Username: value.Username, Password: value.Password}, nil
}

// TestConnection authenticates and closes the SSH transports without opening
// sessions, running commands, or persisting draft credentials or connections.
func (s *Store) TestConnection(ctx context.Context, actor int64, f ConnectionTestInput) (ConnectionTestResult, error) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var target Endpoint
	var jump *Endpoint
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authorizeDraft(tx, actor, f.Ident, f.Connection.GroupID); err != nil {
			return err
		}
		if !f.Connection.Enabled || connectionProtocol(f.Connection.Protocol) != ProtocolSSH {
			return invalid("SSH connection is disabled")
		}
		if err := validateDraftCredential(tx, f.Ident, f.Connection, false); err != nil {
			return err
		}
		var err error
		target, err = s.draftEndpoint(tx, actor, f.Connection.GroupID, f.Connection.Target)
		if err != nil {
			return err
		}
		if f.Connection.Jump != nil {
			if err := validateDraftCredential(tx, f.Ident, f.Connection, true); err != nil {
				return err
			}
			j, err := s.draftEndpoint(tx, actor, f.Connection.GroupID, *f.Connection.Jump)
			if err != nil {
				return err
			}
			jump = &j
		}
		return nil
	})
	if err != nil {
		return ConnectionTestResult{}, err
	}
	select {
	case hostKeySlots <- struct{}{}:
		defer func() { <-hostKeySlots }()
	default:
		return ConnectionTestResult{}, &Error{429, "SSH connection checks are busy; retry later"}
	}
	var via *ssh.Client
	if jump != nil {
		via, err = dialPinnedSSH(ctx, *jump, nil)
		if err != nil {
			return ConnectionTestResult{}, invalid(fmt.Sprintf("SSH jump connection failed: %v", err))
		}
		defer via.Close()
		stop := context.AfterFunc(ctx, func() { via.Close() })
		defer stop()
	}
	client, err := dialPinnedSSH(ctx, target, via)
	if err != nil {
		return ConnectionTestResult{}, invalid(fmt.Sprintf("SSH target connection failed: %v", err))
	}
	defer client.Close()
	if ctx.Err() != nil {
		return ConnectionTestResult{}, invalid("SSH connection test cancelled or timed out")
	}
	return ConnectionTestResult{OK: true, ViaJump: jump != nil, DurationMS: time.Since(started).Milliseconds()}, nil
}
func dialPinnedSSH(ctx context.Context, e Endpoint, via *ssh.Client) (*ssh.Client, error) {
	address := net.JoinHostPort(e.Host, fmt.Sprint(e.Port))
	var raw net.Conn
	var err error
	if via == nil {
		raw, err = (&net.Dialer{}).DialContext(ctx, "tcp", address)
	} else {
		raw, err = via.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return nil, errors.New("SSH destination is unreachable")
	}
	stop := context.AfterFunc(ctx, func() { raw.Close() })
	defer stop()
	cfg := &ssh.ClientConfig{User: e.Username, Auth: []ssh.AuthMethod{ssh.Password(e.Password)}, HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
		if subtle.ConstantTimeCompare([]byte(ssh.FingerprintSHA256(key)), []byte(e.Fingerprint)) != 1 {
			return errors.New("SSH fingerprint mismatch")
		}
		return nil
	}}
	conn, channels, requests, err := ssh.NewClientConn(raw, address, cfg)
	if err != nil {
		raw.Close()
		return nil, errors.New("SSH authentication or host-key verification failed")
	}
	return ssh.NewClient(conn, channels, requests), nil
}
