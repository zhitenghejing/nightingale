package remoteaccess

import (
	"encoding/base64"
	"fmt"
	"net"
	"path/filepath"
	"strings"
)

const (
	PermConfigure   = "/remote-access/connections/write"
	PermCredentials = "/remote-access/credentials/write"
	PermExecute     = "/remote-access/execute"
	PermTerminal    = "/remote-access/terminal"
	ProtocolSSH     = "ssh"
	ProtocolRDP     = "rdp"
)

type Error struct {
	Code    int
	Message string
}

func (e *Error) Error() string     { return e.Message }
func invalid(message string) error { return &Error{400, message} }
func denied() error                { return &Error{403, "SSH access denied"} }
func missing() error               { return &Error{404, "SSH connection or credential not found"} }
func conflict() error              { return &Error{409, "SSH configuration version changed; reload and retry"} }

type CredentialInput struct {
	Name          string `json:"name"`
	GroupID       int64  `json:"group_id"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	Disabled      *bool  `json:"disabled,omitempty"`
	RecordVersion int64  `json:"record_version"`
}
type EndpointInput struct {
	Host          string           `json:"host"`
	Port          int              `json:"port"`
	Fingerprint   string           `json:"fingerprint"`
	CredentialID  string           `json:"credential_id"`
	NewCredential *CredentialInput `json:"new_credential,omitempty"`
}
type ConnectionInput struct {
	Protocol      string         `json:"protocol,omitempty"`
	GroupID       int64          `json:"group_id"`
	Enabled       bool           `json:"enabled"`
	Target        EndpointInput  `json:"target"`
	Jump          *EndpointInput `json:"jump,omitempty"`
	LogPaths      []string       `json:"log_paths"`
	RecordVersion int64          `json:"record_version"`
	deviceScoped  bool
}

// ForDevice disallows borrowing a credential from another device or endpoint.
func (f ConnectionInput) ForDevice() ConnectionInput {
	f.deviceScoped = true
	return f
}

func connectionProtocol(protocol string) string {
	if protocol == "" {
		return ProtocolSSH
	}
	return protocol
}

type ConnectionView struct {
	Ident string `json:"ident"`
	ConnectionInput
	Configured bool `json:"configured"`
	Stale      bool `json:"stale"`
}

// Endpoint and Connection exist only at the SSH transport seam. Secret fields
// cannot accidentally enter a JSON response, diagnostic result or audit entry.
type Endpoint struct {
	Host        string
	Port        int
	Fingerprint string
	Username    string `json:"-"`
	Password    string `json:"-"`
}
type Connection struct {
	Ident    string
	Protocol string
	GroupID  int64
	Target   Endpoint
	Jump     *Endpoint
	LogPaths []string
}

func validateAddress(e EndpointInput) error {
	ip := net.ParseIP(e.Host)
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || e.Port < 1 || e.Port > 65535 {
		return invalid("SSH address must be a fixed IPv4/IPv6 address with a valid port")
	}
	return nil
}
func validateEndpoint(e EndpointInput) error {
	if err := validateAddress(e); err != nil {
		return err
	}
	b, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(e.Fingerprint, "SHA256:"))
	if !strings.HasPrefix(e.Fingerprint, "SHA256:") || err != nil || len(b) != 32 {
		return invalid("a verified SHA256 SSH host fingerprint is required")
	}
	return nil
}
func validateConnection(f ConnectionInput) error {
	if f.Protocol == ProtocolRDP {
		if f.Jump != nil || len(f.LogPaths) != 0 || f.Target.Fingerprint != "" {
			return invalid("RDP connection cannot use SSH settings")
		}
		return validateAddress(f.Target)
	}
	if connectionProtocol(f.Protocol) != ProtocolSSH {
		return invalid("invalid console protocol")
	}
	if err := validateEndpoint(f.Target); err != nil {
		return err
	}
	if f.Jump != nil {
		if err := validateEndpoint(*f.Jump); err != nil {
			return err
		}
	}
	if len(f.LogPaths) > 32 {
		return invalid("at most 32 log paths are allowed")
	}
	for _, p := range f.LogPaths {
		if len(p) > 1024 || !filepath.IsAbs(p) || filepath.Clean(p) != p || strings.ContainsAny(p, "\x00\r\n") {
			return invalid("invalid approved log path")
		}
	}
	return nil
}
func aad(id string, group int64) string {
	return fmt.Sprintf("ops:ssh-password:%s:group:%d", id, group)
}
