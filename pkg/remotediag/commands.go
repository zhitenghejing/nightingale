package remotediag

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

type Request struct {
	Ident     string `json:"ident"`
	Check     string `json:"check"`
	Service   string `json:"service,omitempty"`
	LogPath   string `json:"log_path,omitempty"`
	Since     int64  `json:"since,omitempty"`
	Until     int64  `json:"until,omitempty"`
	Lines     int    `json:"lines,omitempty"`
	Interface string `json:"interface,omitempty"`
	Severity  string `json:"severity,omitempty"`
	Query     string `json:"query,omitempty"`
}

var serviceName = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.@:\-]{0,199}$`)

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

// Bash runs on the remote Linux target. Both saved checks and composed queries
// use a clean environment, a fixed executable search path and a timeout.
func command(r Request, c connection, now time.Time, timeout int) (string, Request, error) {
	if r.Check == linuxCLICheck {
		if r.Service != "" || r.LogPath != "" || r.Interface != "" || r.Severity != "" || r.Since != 0 || r.Until != 0 || r.Lines != 0 {
			return "", r, fmt.Errorf("Linux CLI queries use only ident, check and query")
		}
		body, err := linuxQuery(r.Query, c.LogPaths, now)
		if err != nil {
			return "", r, err
		}
		return boundedLinuxCommand(body, timeout), r, nil
	}
	if r.Lines == 0 {
		r.Lines = 200
	}
	if r.Lines < 1 || r.Lines > 1000 {
		return "", r, fmt.Errorf("lines must be between 1 and 1000")
	}
	if r.Interface != "" || r.Severity != "" || r.Query != "" {
		return "", r, fmt.Errorf("query applies to linux_cli and huawei_cli; interface and severity apply to Huawei checks")
	}
	if r.Service != "" && !serviceName.MatchString(r.Service) {
		return "", r, fmt.Errorf("invalid service unit")
	}
	if r.Service != "" && r.Check != "service" && r.Check != "journal" || r.LogPath != "" && r.Check != "log_tail" || (r.Since != 0 || r.Until != 0) && r.Check != "journal" {
		return "", r, fmt.Errorf("parameters do not apply to this diagnostic check")
	}
	var body string
	switch r.Check {
	case "overview":
		body = "date -u '+%Y-%m-%dT%H:%M:%SZ'; uname -sr; uptime; free -m"
	case "processes":
		// sed drains the pipe: head would SIGPIPE ps on larger machines and
		// pipefail would incorrectly report a successful snapshot as failed.
		body = fmt.Sprintf("ps -eo pid,ppid,user,comm,pcpu,pmem,etime --sort=-pcpu | sed -n '1,%dp'", r.Lines+1)
	case "disk":
		body = "df -hT --; df -i --"
	case "service":
		if r.Service == "" {
			return "", r, fmt.Errorf("service is required")
		}
		body = "systemctl --no-pager show --property=" + strings.Join(linuxServiceProperties, ",") + " -- " + quote(r.Service)
	case "journal":
		if r.Until == 0 {
			r.Until = now.Unix()
		}
		if r.Since == 0 {
			r.Since = r.Until - 1800
		}
		if r.Since <= 0 || r.Until <= r.Since || r.Until-r.Since > 86400 || r.Until > now.Unix()+300 {
			return "", r, fmt.Errorf("journal requires a positive time window of at most 24 hours, not in the future")
		}
		body = fmt.Sprintf("journalctl --utc --no-pager --output=short-iso --lines=%d --since=%s --until=%s", r.Lines, quote("@"+fmt.Sprint(r.Since)), quote("@"+fmt.Sprint(r.Until)))
		if r.Service != "" {
			body += " --unit=" + quote(r.Service)
		}
	case "log_tail":
		if r.LogPath == "" || !slices.Contains(c.LogPaths, r.LogPath) {
			return "", r, fmt.Errorf("log path is not approved for this target")
		}
		body = fmt.Sprintf("tail -n %d -- %s", r.Lines, quote(r.LogPath))
	default:
		return "", r, fmt.Errorf("unknown diagnostic check; use linux_cli, overview, processes, disk, service, journal or log_tail")
	}
	return boundedLinuxCommand(body, timeout), r, nil
}

func boundedLinuxCommand(body string, timeout int) string {
	// The target's timeout bounds remote work even when SSH disconnects. Client
	// cancellation closes transport too, but cannot prove remote termination.
	return fmt.Sprintf("/usr/bin/env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin LC_ALL=C TZ=UTC SYSTEMD_PAGER=cat PAGER=cat /usr/bin/timeout --signal=TERM --kill-after=2s %ds /bin/bash --noprofile --norc -o pipefail -c %s", timeout, quote(body))
}
