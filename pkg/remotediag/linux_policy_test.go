package remotediag

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLinuxQueryComposesModelSelectedReads(t *testing.T) {
	now := time.Unix(1770000000, 0)
	for _, tc := range []struct {
		query, command string
	}{
		{`lscpu | grep "Model name"; free -m`, `lscpu | grep -- 'Model name' && free '-m'`},
		{`cat /proc/cpuinfo | grep -i 'model name' | head -n 1`, `cat '/proc/cpuinfo' | grep '-i' -- 'model name' | head '-n' '1'`},
		{`lscpu | grep -F "a'b;$(date)"`, `lscpu | grep '-F' -- 'a'"'"'b;$(date)'`},
		{`tail -n 20 '/srv/app/error log' | wc -l`, `tail '-n' '20' '--' '/srv/app/error log' | wc '-l'`},
		{`systemctl show -p ActiveState,SubState web.service`, `systemctl 'show' '-p' 'ActiveState,SubState' 'web.service'`},
		{`journalctl --utc -n 30 -u web.service --since @1769999900`, `journalctl '--utc' '-n' '30' '-u' 'web.service' '--since' '@1769999900'`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			got, err := linuxQuery(tc.query, []string{"/srv/app/error log"}, now)
			require.NoError(t, err)
			require.Equal(t, tc.command, got)
		})
	}
}

func TestLinuxQueryRejectsUnapprovedCommandsAndOperands(t *testing.T) {
	now := time.Unix(1770000000, 0)
	for _, query := range []string{
		"", "| lscpu", "lscpu |", "lscpu;; free", "lscpu || free", "lscpu\nfree", "lscpu > output",
		"lscpu | grep pattern /etc/shadow", "lscpu | grep -f /etc/shadow",
		"lscpu | grep -F x'ignored'", "lscpu | grep $(date)", "lscpu | grep `date`", "lscpu &",
		"/bin/lscpu", "sudo lscpu", "lscpu; systemctl restart web.service", "lscpu --sysroot /tmp",
		"free -s 1", "cat /etc/shadow", "tail -n 40 /var/log/unapproved.log", "tail -f /var/log/app.log",
		"systemctl show -p Environment web.service", "journalctl --rotate -n 20",
		"journalctl -n 0", "journalctl -n 1001", "journalctl -n 3 --since @1769700000",
		"journalctl -n 3 --since @1770000000 --until @1769990000",
		"lscpu; lscpu; lscpu; lscpu", "lscpu | grep x | grep x | grep x | wc",
	} {
		t.Run(query, func(t *testing.T) {
			_, err := linuxQuery(query, []string{"/var/log/app.log"}, now)
			require.Error(t, err)
		})
	}
	_, _, err := command(Request{Check: linuxCLICheck, Query: "lscpu", Lines: 12}, connection{}, now, 30)
	require.ErrorContains(t, err, "use only ident, check and query")
}
