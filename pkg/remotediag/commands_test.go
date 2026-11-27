package remotediag

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDiagnosticQuotedLogPathCannotExecuteShell(t *testing.T) {
	if _, err := os.Stat("/usr/bin/timeout"); err != nil {
		t.Skip("GNU timeout unavailable")
	}
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash unavailable")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "error'$(touch SHOULD_NOT_EXIST).log")
	require.NoError(t, os.WriteFile(path, []byte("verified evidence\n"), 0600))
	cmd, _, err := command(Request{Ident: "host", Check: "log_tail", LogPath: path}, connection{LogPaths: []string{path}}, time.Now(), 2)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	process := exec.CommandContext(ctx, "/bin/sh", "-c", cmd)
	process.Dir = dir
	out, err := process.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Equal(t, "verified evidence\n", string(out))
	_, err = os.Stat(filepath.Join(dir, "SHOULD_NOT_EXIST"))
	require.True(t, os.IsNotExist(err))
}

func TestDiagnosticHandshakeTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, e := ln.Accept()
		if e == nil {
			accepted <- c
		}
	}()
	c := connection{Ident: "host", GroupID: 1, Target: endpoint{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, Username: "diagnose", Password: "secret", Fingerprint: "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}}
	r := New(Config{Enable: true, ConnectionsFile: connectionFile(t, c), TimeoutSeconds: 1})
	start := time.Now()
	result, err := r.Run(context.Background(), Request{Ident: "host", Check: "overview"}, func(int64) error { return nil })
	require.Error(t, err)
	require.True(t, result.Incomplete)
	require.Less(t, time.Since(start), 4*time.Second)
	select {
	case peer := <-accepted:
		peer.Close()
	default:
	}
}

func TestDiagnosticJournalWindowAndApplicableInputs(t *testing.T) {
	now := time.Now()
	_, r, err := command(Request{Check: "journal"}, connection{}, now, 30)
	require.NoError(t, err)
	require.Equal(t, int64(1800), r.Until-r.Since)
	for _, request := range []Request{
		{Check: "journal", Since: now.Unix(), Until: now.Add(10 * time.Minute).Unix()},
		{Check: "disk", Since: 1},
		{Check: "processes", Service: "nginx"},
		{Check: "service"},
		{Check: "overview", LogPath: "/var/log/messages"},
		{Check: "journal", Lines: -1},
	} {
		_, _, err := command(request, connection{}, now, 30)
		require.Error(t, err)
	}
}
