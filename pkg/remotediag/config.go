// Package remotediag executes bounded, predefined Linux diagnostics over SSH.
// Connection secrets never cross its interface into model-visible tool arguments.
package remotediag

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	diagconfig "github.com/ccfos/nightingale/v6/pkg/remotediag/config"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
)

type Config = diagconfig.Config

type endpoint struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	Fingerprint string `json:"fingerprint"`
}

type connection struct {
	Ident    string    `json:"ident"`
	GroupID  int64     `json:"group_id"`
	Target   endpoint  `json:"target"`
	Jump     *endpoint `json:"jump,omitempty"`
	LogPaths []string  `json:"log_paths,omitempty"`
}

// load is deliberately per request: revoking a connection or rotating a
// password takes effect without restarting the center. Reject symlinks and
// permissive files; the deployment operator alone provisions these passwords.
func load(path, ident string) (connection, error) {
	f, err := os.Open(path)
	if err != nil {
		return connection{}, fmt.Errorf("remote diagnostics connection file unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	link, linkErr := os.Lstat(path)
	if err != nil || linkErr != nil || !info.Mode().IsRegular() || !os.SameFile(info, link) || info.Mode().Perm()&0077 != 0 || info.Size() > 1<<20 {
		return connection{}, fmt.Errorf("remote diagnostics requires a private regular connection file (0600, maximum 1 MiB)")
	}
	var file struct {
		Connections []connection `json:"connections"`
	}
	dec := json.NewDecoder(io.LimitReader(f, (1<<20)+1))
	dec.DisallowUnknownFields()
	if dec.Decode(&file) != nil || dec.Decode(new(interface{})) != io.EOF {
		return connection{}, fmt.Errorf("invalid remote diagnostics connection file")
	}
	seen := map[string]bool{}
	var selected connection
	for _, c := range file.Connections {
		if c.Ident == "" || seen[c.Ident] || c.GroupID <= 0 || validateEndpoint(c.Target) != nil {
			return connection{}, fmt.Errorf("invalid or duplicate remote diagnostics connection")
		}
		seen[c.Ident] = true
		if c.Jump != nil && validateEndpoint(*c.Jump) != nil {
			return connection{}, fmt.Errorf("invalid remote diagnostics jump connection")
		}
		for _, p := range c.LogPaths {
			if !filepath.IsAbs(p) || filepath.Clean(p) != p || strings.ContainsAny(p, "\x00\r\n") {
				return connection{}, fmt.Errorf("invalid diagnostic log path")
			}
		}
		if c.Ident == ident {
			selected = c
		}
	}
	if selected.Ident == "" {
		return connection{}, fmt.Errorf("SSH diagnostics is not configured for this target")
	}
	return selected, nil
}

func validateEndpoint(e endpoint) error {
	// Literal addresses bind the operator's approved destination, avoiding DNS
	// changes between authorization, tunnelling and host-key verification.
	ip := net.ParseIP(e.Host)
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || e.Port < 1 || e.Port > 65535 || strings.TrimSpace(e.Username) == "" || e.Password == "" {
		return fmt.Errorf("invalid SSH endpoint")
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(e.Fingerprint, "SHA256:"))
	if !strings.HasPrefix(e.Fingerprint, "SHA256:") || err != nil || len(raw) != 32 {
		return fmt.Errorf("SSH host fingerprint must be SHA256")
	}
	return nil
}
