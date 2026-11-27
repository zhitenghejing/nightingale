package remoteaccess

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// RDPGatewayConfigured keeps the unencrypted guacd transport on the center host.
func RDPGatewayConfigured(address string) bool {
	host, port, err := net.SplitHostPort(address)
	number, portErr := strconv.Atoi(port)
	return err == nil && portErr == nil && number > 0 && number <= 65535 && net.ParseIP(host).IsLoopback()
}

type RDPConsole struct {
	connection Connection
	store      *Store
	actor      int64
	reader     *bufio.Reader
	conn       net.Conn
	close      func()
}

func (s *Store) OpenRDP(ctx context.Context, actor int64, ident, gateway string) (*RDPConsole, error) {
	if !RDPGatewayConfigured(gateway) {
		return nil, &Error{503, "RDP gateway is unavailable"}
	}
	startup, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	connection, err := s.consoleConnection(startup, actor, ident, ProtocolRDP)
	if err != nil {
		return nil, err
	}
	conn, err := (&net.Dialer{}).DialContext(startup, "tcp", gateway)
	if err != nil {
		return nil, &Error{503, "RDP gateway is unreachable"}
	}
	stopStartup := context.AfterFunc(startup, func() { conn.Close() })
	defer stopStartup()
	reader := bufio.NewReader(conn)
	if err := startRDP(conn, reader, connection.Target); err != nil || startup.Err() != nil {
		conn.Close()
		return nil, &Error{502, "RDP gateway could not establish the session"}
	}
	if err := audit(s.DB.WithContext(startup), actor, connection.GroupID, ident, "rdp_console_start"); err != nil {
		conn.Close()
		return nil, err
	}
	closeConsole := sync.OnceFunc(func() {
		conn.Close()
		auditCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = audit(s.DB.WithContext(auditCtx), actor, connection.GroupID, ident, "rdp_console_end")
	})
	stopLifetime := context.AfterFunc(ctx, closeConsole)
	return &RDPConsole{connection: connection, store: s, actor: actor, reader: reader, conn: conn, close: func() {
		stopLifetime()
		closeConsole()
	}}, nil
}

func (c *RDPConsole) Read(p []byte) (int, error) { return c.reader.Read(p) }
func (c *RDPConsole) Close()                     { c.close() }

func (c *RDPConsole) CheckAccess(ctx context.Context) error {
	current, err := c.store.consoleConnection(ctx, c.actor, c.connection.Ident, ProtocolRDP)
	if err != nil {
		return err
	}
	if current.GroupID != c.connection.GroupID || current.Target != c.connection.Target {
		return denied()
	}
	return nil
}

func (c *RDPConsole) Send(input string) error {
	reader := bufio.NewReader(strings.NewReader(input))
	parts, err := readGuacamole(reader)
	if err != nil {
		return err
	}
	if _, err := reader.Peek(1); err != io.EOF {
		return fmt.Errorf("expected one RDP console instruction")
	}
	switch parts[0] {
	case "key", "mouse", "ack", "sync", "nop", "disconnect":
	default:
		return fmt.Errorf("invalid RDP console instruction")
	}
	if err := c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	return writeGuacamole(c.conn, parts...)
}

func startRDP(conn io.Writer, reader *bufio.Reader, target Endpoint) error {
	if err := writeGuacamole(conn, "select", "rdp"); err != nil {
		return err
	}
	args, err := readGuacamole(reader)
	if err != nil || args[0] != "args" {
		return fmt.Errorf("invalid RDP gateway handshake")
	}
	for _, instruction := range [][]string{{"size", "1280", "800", "96"}, {"audio"}, {"video"}, {"image", "image/png", "image/jpeg"}} {
		if err := writeGuacamole(conn, instruction...); err != nil {
			return err
		}
	}
	username, domain := target.Username, ""
	if prefix, user, found := strings.Cut(username, "\\"); found {
		domain, username = prefix, user
	}
	parameters := map[string]string{
		"hostname": target.Host, "port": strconv.Itoa(target.Port), "username": username, "password": target.Password, "domain": domain,
		"security": "any", "cert-tofu": "true", "disable-audio": "true", "disable-copy": "true", "disable-paste": "true",
	}
	values := []string{"connect"}
	for _, key := range args[1:] {
		if strings.HasPrefix(key, "VERSION_") {
			values = append(values, key)
		} else {
			values = append(values, parameters[key])
		}
	}
	if err := writeGuacamole(conn, values...); err != nil {
		return err
	}
	ready, err := readGuacamole(reader)
	if err != nil || ready[0] != "ready" {
		return fmt.Errorf("RDP gateway did not become ready")
	}
	return nil
}

func writeGuacamole(w io.Writer, parts ...string) error {
	var encoded strings.Builder
	for i, part := range parts {
		if i > 0 {
			encoded.WriteByte(',')
		}
		encoded.WriteString(strconv.Itoa(utf8.RuneCountInString(part)))
		encoded.WriteByte('.')
		encoded.WriteString(part)
	}
	encoded.WriteByte(';')
	_, err := io.WriteString(w, encoded.String())
	return err
}

// Only the bounded handshake and client input are parsed here. The desktop
// stream is forwarded as-is to the Apache Guacamole client.
func readGuacamole(r *bufio.Reader) ([]string, error) {
	parts := make([]string, 0, 8)
	limit := 16 << 10
	for len(parts) < 512 {
		length := 0
		for digits := 0; ; digits++ {
			b, err := r.ReadByte()
			if err != nil {
				return nil, err
			}
			if b == '.' && digits > 0 {
				break
			}
			if digits == 5 || b < '0' || b > '9' {
				return nil, fmt.Errorf("invalid Guacamole instruction length")
			}
			length = length*10 + int(b-'0')
		}
		limit -= length
		if limit < 0 {
			return nil, fmt.Errorf("Guacamole instruction is too large")
		}
		var value strings.Builder
		for i := 0; i < length; i++ {
			v, size, err := r.ReadRune()
			if err != nil || size == 1 && v == utf8.RuneError {
				return nil, fmt.Errorf("invalid Guacamole instruction value")
			}
			value.WriteRune(v)
		}
		parts = append(parts, value.String())
		separator, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		if separator == ';' {
			return parts, nil
		}
		if separator != ',' {
			break
		}
	}
	return nil, fmt.Errorf("invalid Guacamole instruction")
}
