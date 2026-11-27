package remotediag

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/crypto/ssh"
)

const huaweiInterfaceCheck = "huawei_interface_config"
const huaweiCLICheck = "huawei_cli"
const interfaceListCommand = "display interface brief"
const interfaceConfigCommand = "display current-configuration interface "

var (
	portNumber = regexp.MustCompile(`^[0-9]{1,4}$`)
	// Only physical interface families and numeric slot/port addresses can reach
	// the command builder. Table entries from the remote CLI use the same parser.
	huaweiPhysicalInterface = regexp.MustCompile(`(?i)^(GigabitEthernet|GE|Ethernet|Eth|XGigabitEthernet|XGE|10GE|25GE|40GE|100GE|200GE|400GE|MultiGE)[ \t]*([0-9]{1,4}/[0-9]{1,4}/[0-9]{1,4}(?:/[0-9]{1,4})?)$`)
	vrpPrompt               = regexp.MustCompile(`^<[A-Za-z0-9_.:-]{1,128}>$`)
	terminalCSI             = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	pagerPrompt             = regexp.MustCompile(`(?i)-{2,}[ \t]*more[ \t]*-{2,}[ \t]*(?:\(.*\))?$`)
	errVRPOutputLimit       = errors.New("Huawei CLI output exceeded the diagnostic limit")
)

type interfaceSelector struct{ name, number string }

type physicalInterface struct{ family, location string }

func (p physicalInterface) name() string { return p.family + p.location }
func (p physicalInterface) port() string {
	return p.location[strings.LastIndexByte(p.location, '/')+1:]
}

func parsePhysicalInterface(value string) (physicalInterface, bool) {
	match := huaweiPhysicalInterface.FindStringSubmatch(value)
	if match == nil {
		return physicalInterface{}, false
	}
	family := strings.ToUpper(match[1])
	switch family {
	case "GIGABITETHERNET":
		family = "GigabitEthernet"
	case "ETHERNET":
		family = "Ethernet"
	case "XGIGABITETHERNET":
		family = "XGigabitEthernet"
	case "MULTIGE":
		family = "MultiGE"
	}
	return physicalInterface{family, match[2]}, true
}
func parseInterfaceSelector(r Request) (interfaceSelector, error) {
	value := r.Interface
	if p, ok := parsePhysicalInterface(value); ok {
		return interfaceSelector{name: p.name()}, nil
	}
	if portNumber.MatchString(value) {
		return interfaceSelector{number: normalizePort(value)}, nil
	}
	return interfaceSelector{}, errors.New("interface must be a Huawei physical interface name or a port number")
}
func normalizePort(value string) string {
	value = strings.TrimLeft(value, "0")
	if value == "" {
		return "0"
	}
	return value
}
func matchingPhysicalInterfaces(output, number string) []string {
	unique := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		// Huawei may mark an administratively stopped interface with a leading *.
		iface, ok := parsePhysicalInterface(strings.TrimPrefix(fields[0], "*"))
		if ok && normalizePort(iface.port()) == number {
			unique[canonicalInterface(iface)] = true
		}
	}
	names := make([]string, 0, len(unique))
	for name := range unique {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
func canonicalInterface(p physicalInterface) string {
	family := p.family
	switch family {
	case "GE":
		family = "GigabitEthernet"
	case "XGE":
		family = "XGigabitEthernet"
	case "ETH":
		family = "Ethernet"
	}
	return family + p.location
}

// runHuawei opens one read-only VRP user-view terminal per request. The selected
// operation builds its command from validated data and interprets its own output.
func runHuawei(ctx context.Context, session *ssh.Session, query huaweiQuery, limit int, result *Result) error {
	if err := session.RequestPty("vt100", 60, 200, ssh.TerminalModes{ssh.ECHO: 1}); err != nil {
		return errors.New("Huawei CLI rejected the SSH terminal request")
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		return errors.New("cannot open Huawei CLI input")
	}
	output, err := session.StdoutPipe()
	if err != nil {
		return errors.New("cannot open Huawei CLI output")
	}
	stderr := &limitedOutput{limit: limit}
	session.Stderr = stderr
	defer func() {
		result.Stderr, result.StderrTruncated = stderr.read()
		result.Incomplete = result.Incomplete || result.StderrTruncated
	}()
	if err = session.Shell(); err != nil {
		return errors.New("Huawei CLI rejected the interactive SSH session")
	}
	cli := vrpTerminal{input: stdin, output: output, limit: limit}
	defer func() { result.StdoutTruncated = cli.truncated }()
	if _, err = cli.readPrompt(ctx, false); err != nil {
		return fmt.Errorf("cannot read Huawei CLI prompt: %w", err)
	}
	return query.run(ctx, &cli, result)
}

func resolveHuaweiInterface(ctx context.Context, cli *vrpTerminal, selector interfaceSelector, result *Result) (physicalInterface, error) {
	name := selector.name
	if name == "" {
		text, err := cli.query(ctx, interfaceListCommand)
		if err != nil {
			return physicalInterface{}, fmt.Errorf("Huawei interface list failed: %w", err)
		}
		matches := matchingPhysicalInterfaces(text, selector.number)
		switch len(matches) {
		case 0:
			return physicalInterface{}, errors.New("no Huawei physical interface matched that port number; specify its full name")
		case 1:
			name = matches[0]
		default:
			result.CandidateInterfaces = matches
			return physicalInterface{}, errors.New("port number matches multiple interfaces; specify its full name")
		}
	}
	// Name is either a parsed input or an entry validated by the same parser.
	iface, _ := parsePhysicalInterface(name)
	return iface, nil
}

func configInterface(text string, iface physicalInterface) string {
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "interface ") {
			continue
		}
		actual, ok := parsePhysicalInterface(strings.TrimSpace(strings.TrimPrefix(line, "interface ")))
		if ok && canonicalInterface(actual) == canonicalInterface(iface) {
			return actual.name()
		}
	}
	return ""
}

type vrpTerminal struct {
	input     io.Writer
	output    io.Reader
	prompt    string
	limit     int
	truncated bool
}

func (t *vrpTerminal) query(ctx context.Context, command string) (string, error) {
	if _, err := io.WriteString(t.input, command+"\r"); err != nil {
		return "", errors.New("cannot send the read-only Huawei command")
	}
	text, err := t.readPrompt(ctx, true)
	if err != nil {
		return trimSessionEcho(text, command), err
	}
	return trimSessionEcho(text, command), nil
}

func trimSessionEcho(text, command string) string {
	text = strings.TrimSpace(text)
	if text == command {
		return ""
	}
	if first, rest, ok := strings.Cut(text, "\n"); ok && strings.TrimSpace(first) == command {
		text = strings.TrimSpace(rest)
	}
	return text
}
func (t *vrpTerminal) readPrompt(ctx context.Context, allowPager bool) (string, error) {
	var received, pages int
	var raw string
	chunk := make([]byte, 2048)
	for {
		n, err := t.output.Read(chunk)
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if received+n > t.limit {
			t.truncated = true
			return completeTerminalLines(raw + string(chunk[:t.limit-received])), errVRPOutputLimit
		}
		received += n
		if n > 0 {
			raw += string(chunk[:n])
			clean := strings.ReplaceAll(terminalCSI.ReplaceAllString(raw, ""), "\r", "")
			clean = strings.TrimRight(clean, " \t\n\b")
			start := strings.LastIndexByte(clean, '\n') + 1
			line := clean[start:]
			if t.prompt == "" && vrpPrompt.MatchString(line) {
				t.prompt = line
				return clean[:start], nil
			}
			if t.prompt != "" && line == t.prompt {
				return clean[:start], nil
			}
			if allowPager {
				if marker := pagerPrompt.FindStringIndex(line); marker != nil {
					pages++
					if pages > 16 {
						t.truncated = true
						return completeTerminalLines(raw), errVRPOutputLimit
					}
					raw = clean[:start+marker[0]]
					if _, e := io.WriteString(t.input, " "); e != nil {
						return "", errors.New("Huawei CLI closed during pagination")
					}
				}
			}
		}
		if err != nil {
			return "", errors.New("SSH terminal closed before the Huawei prompt returned")
		}
	}
}

// On limits only complete lines are usable as partial evidence.
func completeTerminalLines(raw string) string {
	clean := strings.ReplaceAll(terminalCSI.ReplaceAllString(raw, ""), "\r", "")
	if end := strings.LastIndexByte(clean, '\n'); end >= 0 {
		return clean[:end]
	}
	return ""
}
