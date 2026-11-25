package remoteaccess

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
)

type HostKeyInput struct {
	Ident      string          `json:"ident"`
	Endpoint   string          `json:"endpoint"`
	Connection ConnectionInput `json:"connection"`
}
type HostKey struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Algorithm   string `json:"algorithm"`
	Fingerprint string `json:"fingerprint"`
}

var hostKeySlots = make(chan struct{}, 4)

// ProbeHostKey stops the selected endpoint's handshake before authentication.
// A confirmed, pinned jump key is required before sending jump credentials.
// Probing never creates credentials or changes saved host-key pins.
func (s *Store) ProbeHostKey(ctx context.Context, actor int64, f HostKeyInput) (HostKey, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var endpoint EndpointInput
	var jump *Endpoint
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if connectionProtocol(f.Connection.Protocol) != ProtocolSSH {
			return invalid("SSH host-key discovery requires SSH")
		}
		if err := authorizeDraft(tx, actor, f.Ident, f.Connection.GroupID); err != nil {
			return err
		}

		switch f.Endpoint {
		case "jump":
			if f.Connection.Jump == nil {
				return invalid("SSH jump host is required")
			}
			endpoint = *f.Connection.Jump
		case "target":
			endpoint = f.Connection.Target
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
		default:
			return invalid("invalid SSH host-key endpoint")
		}
		return validateAddress(endpoint)
	})
	if err != nil {
		return HostKey{}, err
	}
	select {
	case hostKeySlots <- struct{}{}:
		defer func() { <-hostKeySlots }()
	default:
		return HostKey{}, &Error{429, "SSH host-key discovery is busy; retry later"}
	}
	return discoverHostKey(ctx, endpoint, jump)
}
func discoverHostKey(ctx context.Context, endpoint EndpointInput, jump *Endpoint) (HostKey, error) {
	dialer := &net.Dialer{}
	var via *ssh.Client
	if jump != nil {
		var err error
		via, err = dialPinnedSSH(ctx, *jump, nil)
		if err != nil {
			return HostKey{}, invalid(fmt.Sprintf("SSH jump connection failed: %v", err))
		}
		defer via.Close()
		stop := context.AfterFunc(ctx, func() { via.Close() })
		defer stop()
	}
	address := net.JoinHostPort(endpoint.Host, fmt.Sprint(endpoint.Port))
	var raw net.Conn
	var err error
	if via == nil {
		raw, err = dialer.DialContext(ctx, "tcp", address)
	} else {
		raw, err = via.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return HostKey{}, invalid("SSH endpoint is unreachable")
	}
	defer raw.Close()
	stop := context.AfterFunc(ctx, func() { raw.Close() })
	defer stop()
	var result HostKey
	// Returning an error here ends key exchange before any user authentication.
	cfg := &ssh.ClientConfig{User: "n9e-host-key-discovery", HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
		result = HostKey{Host: endpoint.Host, Port: endpoint.Port, Algorithm: key.Type(), Fingerprint: ssh.FingerprintSHA256(key)}
		return errors.New("host key discovered")
	}}
	conn, _, _, _ := ssh.NewClientConn(raw, address, cfg)
	if conn != nil {
		conn.Close()
	}
	if ctx.Err() != nil {
		return HostKey{}, invalid("SSH host-key discovery cancelled or timed out")
	}
	if result.Fingerprint == "" {
		return HostKey{}, invalid("cannot discover SSH host key")
	}
	return result, nil
}
