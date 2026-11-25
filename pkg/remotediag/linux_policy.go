package remotediag

import (
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const linuxCLICheck = "linux_cli"

// A diagnostic query consists of literal words, pipes and pipeline separators.
// Quoting groups a literal argument; it does not support shell expansion.
var linuxQueryToken = regexp.MustCompile(`^(?:"[^"[:cntrl:]]*"|'[^'[:cntrl:]]*'|[A-Za-z0-9_.:/,@%+=-]+|[|;])`)

var linuxReadOptions = map[string][]string{
	"lscpu":  nil,
	"free":   {"-b", "-k", "-m", "-g", "-h", "-w"},
	"uptime": {"-p", "-s"},
	"uname":  {"-a", "-m", "-n", "-r", "-s", "-v", "-sr"},
	"df":     {"-h", "-T", "-i", "-hT", "-Th", "-P"},
	"lsblk":  {"-b", "-d", "-l", "-n", "-J"},
	"ps":     {"-e", "-A"},
}

var linuxReadFiles = []string{
	"/proc/cpuinfo", "/proc/meminfo", "/proc/loadavg", "/proc/uptime", "/proc/version", "/etc/os-release",
}

var linuxServiceProperties = []string{
	"Id", "LoadState", "ActiveState", "SubState", "Result", "ExecMainCode", "ExecMainStatus",
	"MainPID", "ActiveEnterTimestamp", "InactiveEnterTimestamp",
}

func linuxQuery(query string, logPaths []string, now time.Time) (string, error) {
	const maxQueryBytes = 512
	const maxPipelines = 3
	const maxStages = 4
	if len(query) == 0 || len(query) > maxQueryBytes {
		return "", errors.New("Linux CLI requires a query of 1 to 512 bytes")
	}

	var pipelines, stages, argv []string
	for {
		query = strings.TrimLeft(query, " \t")
		token := linuxQueryToken.FindString(query)
		if query != "" && token == "" {
			return "", errors.New("Linux CLI only accepts literal arguments, | and ;")
		}
		query = query[len(token):]
		switch token {
		case "", "|", ";":
			if len(argv) == 0 {
				return "", errors.New("Linux CLI expects a command at each pipeline stage")
			}
			if len(stages) == maxStages || len(pipelines) == maxPipelines {
				return "", errors.New("Linux CLI allows up to three pipelines with four stages each")
			}
			stage, err := linuxStage(argv, len(stages) == 0, logPaths, now)
			if err != nil {
				return "", err
			}
			stages = append(stages, stage)
			argv = argv[:0]
			if token != "|" {
				pipelines = append(pipelines, strings.Join(stages, " | "))
				stages = stages[:0]
			}
			if token == "" {
				// A failed pipeline must not be masked by a later success.
				return strings.Join(pipelines, " && "), nil
			}
		default:
			if query != "" && !strings.ContainsRune(" \t|;", rune(query[0])) {
				return "", errors.New("Linux CLI arguments must be separated by spaces")
			}
			if token[0] == '"' || token[0] == '\'' {
				token = token[1 : len(token)-1]
			}
			argv = append(argv, token)
		}
	}
}

func linuxStage(argv []string, first bool, logPaths []string, now time.Time) (string, error) {
	name, args := argv[0], argv[1:]
	if first {
		if opts, allowed := linuxReadOptions[name]; allowed && argsIn(args, opts) {
			return buildLinuxArgv(name, args), nil
		}
		if name == "cat" && len(args) == 1 && slices.Contains(linuxReadFiles, args[0]) {
			return buildLinuxArgv(name, args), nil
		}
		if name == "tail" && len(args) == 3 && args[0] == "-n" && linuxLineCount(args[1]) {
			if !slices.Contains(logPaths, args[2]) {
				return "", errors.New("log path is not approved for this target")
			}
			return buildLinuxArgv(name, []string{"-n", args[1], "--", args[2]}), nil
		}
		if name == "systemctl" && len(args) == 4 && args[0] == "show" && args[1] == "-p" &&
			argsIn(strings.Split(args[2], ","), linuxServiceProperties) && serviceName.MatchString(args[3]) {
			return buildLinuxArgv(name, args), nil
		}
		if name == "journalctl" {
			if !linuxJournalAllowed(args, now.Unix()) {
				return "", errors.New("journalctl requires -n 1..1000, with optional -u UNIT, --since @SECONDS, --until @SECONDS, --no-pager or --utc; the time window is at most 24 hours")
			}
			return buildLinuxArgv(name, args), nil
		}
		return "", errors.New("Linux query command or arguments are outside the read-only policy")
	}
	switch name {
	case "grep":
		if len(args) >= 1 && len(args) <= 4 {
			opts, pattern := args[:len(args)-1], args[len(args)-1]
			if pattern != "" && argsIn(opts, []string{"-F", "-E", "-i", "-v"}) {
				// An explicit option terminator prevents a pattern from becoming
				// an option. There are no filename operands after the pattern.
				return buildLinuxArgv(name, opts) + " -- " + quote(pattern), nil
			}
		}
	case "head":
		if len(args) == 2 && args[0] == "-n" && linuxLineCount(args[1]) {
			return buildLinuxArgv(name, args), nil
		}
	case "wc":
		if argsIn(args, []string{"-l", "-w", "-c"}) {
			return buildLinuxArgv(name, args), nil
		}
	}
	return "", errors.New("Linux pipeline filter or arguments are outside the read-only policy")
}

func linuxLineCount(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 1 && n <= 1000 && strconv.Itoa(n) == s
}

func linuxJournalAllowed(args []string, now int64) bool {
	var since, until int64
	withCount := false
	seen := make(map[string]bool, len(args))
	for i := 0; i < len(args); i++ {
		option := args[i]
		if seen[option] {
			return false
		}
		seen[option] = true
		if option == "--no-pager" || option == "--utc" {
			continue
		}
		if i+1 == len(args) {
			return false
		}
		i++
		value := args[i]
		switch option {
		case "-n":
			if !linuxLineCount(value) {
				return false
			}
			withCount = true
		case "-u":
			if !serviceName.MatchString(value) {
				return false
			}
		case "--since", "--until":
			raw, hasPrefix := strings.CutPrefix(value, "@")
			ts, err := strconv.ParseInt(raw, 10, 64)
			if !hasPrefix || err != nil || ts <= 0 || ts > now+300 || strconv.FormatInt(ts, 10) != raw {
				return false
			}
			if option == "--since" {
				since = ts
			} else {
				until = ts
			}
		default:
			return false
		}
	}
	if until == 0 {
		until = now
	}
	return withCount && (since == 0 || until > since && until-since <= 86400)
}

func argsIn(args, options []string) bool {
	for _, arg := range args {
		if !slices.Contains(options, arg) {
			return false
		}
	}
	return true
}

func buildLinuxArgv(name string, args []string) string {
	var command strings.Builder
	command.WriteString(name)
	for _, arg := range args {
		command.WriteByte(' ')
		command.WriteString(quote(arg))
	}
	return command.String()
}
