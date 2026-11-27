package remotediag

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The query policy uses literal commands and, when needed, one typed suffix.
// The operator can add device-specific read commands without changing the tool.
var defaultHuaweiQueries = []string{
	"display version", "display device", "display clock", "display cpu-usage", "display memory-usage",
	"display temperature", "display power", "display fan", "display interface brief", "display interface description",
	"display interface {interface}", "display current-configuration interface {interface}",
	"display port vlan", "display vlan", "display vlan {vlan}", "display logbuffer",
}
var huaweiQueryText = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._:/-]{0,159}$`)
var rejectedHuaweiQuery = regexp.MustCompile(`(?i)^(display (saved-configuration|diagnostic-information|configuration|aaa|local-user|ssh user|current-configuration)( |$))`)
var rejectedHuaweiWord = regexp.MustCompile(`(?i)(^| )(file|redirect|output)( |$)`)
var huaweiCLIError = regexp.MustCompile(`(?im)^(\s*Error:|\s*%\s*(Unrecognized|Incomplete|Ambiguous|Wrong|Too many|Permission)|\s*Permission denied)`)

type huaweiQuery struct {
	command  string
	selector *interfaceSelector
}

func prepareHuaweiQuery(request Request, extra []string) (huaweiQuery, Request, error) {
	var prepared huaweiQuery
	if request.Service != "" || request.LogPath != "" || request.Since != 0 || request.Until != 0 {
		return prepared, request, errors.New("Huawei queries do not accept Linux service, log path or time-window fields")
	}
	legacy := request.Check == huaweiInterfaceCheck
	if legacy {
		if request.Query != "" {
			return prepared, request, errors.New("legacy Huawei interface check does not accept a query")
		}
		request.Query = strings.TrimSpace(interfaceConfigCommand)
	}
	if !huaweiQueryText.MatchString(request.Query) {
		return prepared, request, errors.New("Huawei query must be one display command without terminal or shell operators")
	}
	request.Query = strings.ToLower(strings.Join(strings.Fields(request.Query), " "))
	if !strings.HasPrefix(request.Query, "display ") || rejectedHuaweiWord.MatchString(request.Query) ||
		(rejectedHuaweiQuery.MatchString(request.Query) && !strings.HasPrefix(request.Query, "display current-configuration interface")) {
		return prepared, request, errors.New("Huawei command is outside the read-only query policy")
	}
	if request.Query == "display logbuffer" {
		if request.Severity == "" {
			request.Severity = "error"
		}
		if request.Severity != "error" && request.Severity != "all" {
			return prepared, request, errors.New("Huawei log severity must be error or all")
		}
		if request.Lines == 0 {
			request.Lines = 20
		}
		if request.Lines < 1 || request.Lines > 50 {
			return prepared, request, errors.New("Huawei log entry limit must be between 1 and 50")
		}
	} else if request.Severity != "" || request.Lines != 0 {
		return prepared, request, errors.New("severity and lines apply only to display logbuffer")
	}
	for _, rule := range slices.Concat(defaultHuaweiQueries, extra) {
		rule = strings.ToLower(strings.Join(strings.Fields(rule), " "))
		if strings.HasPrefix(rule, "display current-configuration") && rule != "display current-configuration interface {interface}" {
			continue
		}
		if prefix, ok := strings.CutSuffix(rule, " {interface}"); ok {
			switch {
			case request.Query == prefix && request.Interface != "":
				selector, err := parseInterfaceSelector(request)
				if err != nil {
					return prepared, request, err
				}
				return huaweiQuery{command: prefix, selector: &selector}, request, nil
			case request.Interface == "" && strings.HasPrefix(request.Query, prefix+" "):
				if actual, valid := parsePhysicalInterface(strings.TrimPrefix(request.Query, prefix+" ")); valid {
					return huaweiQuery{command: prefix, selector: &interfaceSelector{name: actual.name()}}, request, nil
				}
			}
			continue
		}
		if request.Interface != "" {
			continue
		}
		if prefix, ok := strings.CutSuffix(rule, " {vlan}"); ok {
			number, found := strings.CutPrefix(request.Query, prefix+" ")
			vlan, err := strconv.Atoi(number)
			if found && portNumber.MatchString(number) && err == nil && vlan >= 1 && vlan <= 4094 {
				return huaweiQuery{command: fmt.Sprintf("%s %d", prefix, vlan)}, request, nil
			}
			continue
		}
		// Current configuration is scoped to one strictly validated interface even
		// when an operator has added an exact literal policy entry by mistake.
		if request.Query == rule {
			return huaweiQuery{command: request.Query}, request, nil
		}
	}
	return prepared, request, errors.New("Huawei query is not allowed by the device read policy or its arguments are invalid")
}

func (q huaweiQuery) run(ctx context.Context, cli *vrpTerminal, result *Result) error {
	command := q.command
	var iface physicalInterface
	if q.selector != nil {
		var err error
		iface, err = resolveHuaweiInterface(ctx, cli, *q.selector, result)
		if err != nil {
			return err
		}
		command += " " + iface.family + " " + iface.location
	}
	// The server supplies the fixed filter, so large buffers are filtered
	// before transfer. Client queries cannot provide their own piped commands.
	deviceFiltered := command == "display logbuffer" && result.Request.Severity == "error"
	if deviceFiltered {
		command += " | include %[0-9][0-9][A-Za-z0-9_-]+/[0-3]/"
	}
	output, err := cli.query(ctx, command)
	if deviceFiltered && err == nil && huaweiCLIError.MatchString(output) {
		// Some releases or account policies do not support the filter.
		deviceFiltered = false
		output, err = cli.query(ctx, q.command)
	}
	if q.command == "display logbuffer" && (err == nil || errors.Is(err, errVRPOutputLimit)) {
		result.Stdout, result.LogBuffer = filterHuaweiLogs(output, result.Request)
		result.LogBuffer.DeviceFiltered = deviceFiltered
		emptyFiltered := deviceFiltered && (output == "" || noLogMatches.MatchString(output) || vrpLogBufferHeader.MatchString(output))
		if err == nil && result.LogBuffer.Scanned == 0 && !emptyFiltered && !emptyLogBuffer.MatchString(output) {
			return errors.New("Huawei CLI did not return a recognized log buffer; check command support and read permissions")
		}
	} else if err == nil {
		if output == "" || huaweiCLIError.MatchString(output) {
			return errors.New("Huawei CLI did not return query data; check command support and read permissions")
		}
		if q.selector != nil {
			result.Interface = canonicalInterface(iface)
		}
		if q.command == strings.TrimSpace(interfaceConfigCommand) {
			actual := configInterface(output, iface)
			if actual == "" {
				return errors.New("Huawei CLI did not return the requested interface configuration; check interface name and read permissions")
			}
			result.Interface = actual
		}
		result.Stdout = output
	}
	if err != nil {
		return fmt.Errorf("Huawei CLI query incomplete: %w", err)
	}
	result.ExitCode = 0
	return nil
}

// Logs are filtered locally so all-severity entries are not sent to the model
// when the user asks for errors. A partial buffer remains incomplete.
type HuaweiLogBuffer struct {
	Severity            string `json:"severity"`
	Scanned             int    `json:"scanned"`
	Matched             int    `json:"matched"`
	Returned            int    `json:"returned"`
	Limited             bool   `json:"limited"`
	DeviceFiltered      bool   `json:"device_filtered"`
	CurrentMessages     *int   `json:"current_messages,omitempty"`
	OverwrittenMessages *int   `json:"overwritten_messages,omitempty"`
	DroppedMessages     *int   `json:"dropped_messages,omitempty"`
}

var vrpLogSeverity = regexp.MustCompile(`%{1,2}[0-9]{2}[A-Za-z0-9_-]+/([0-7])/[A-Za-z0-9_-]+`)
var noLogMatches = regexp.MustCompile(`(?i)^(?:Info:\s*)?No (matching (?:information|records|entries)|information to display)[.]?$`)
var emptyLogBuffer = regexp.MustCompile(`(?im)^\s*Current\s+messages\s*:\s*0\s*$`)
var vrpLogBufferHeader = regexp.MustCompile(`(?im)^Logging buffer configuration and contents\s*:\s*enabled\s*$`)
var vrpLogCount = regexp.MustCompile(`(?i)^(Current|Overwritten|Dropped) messages\s*:\s*([0-9]+)$`)

func filterHuaweiLogs(text string, r Request) (string, *HuaweiLogBuffer) {
	stats := &HuaweiLogBuffer{Severity: r.Severity}
	entries := make([]string, 0, r.Lines)
	for _, line := range strings.Split(text, "\n") {
		if count := vrpLogCount.FindStringSubmatch(strings.TrimSpace(line)); count != nil {
			value, err := strconv.Atoi(count[2])
			if err == nil {
				switch strings.ToLower(count[1]) {
				case "current":
					stats.CurrentMessages = &value
				case "overwritten":
					stats.OverwrittenMessages = &value
				case "dropped":
					stats.DroppedMessages = &value
				}
			}
		}
		m := vrpLogSeverity.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		stats.Scanned++
		if r.Severity == "error" && m[1][0] > '3' {
			continue
		}
		stats.Matched++
		if len(entries) < r.Lines {
			entries = append(entries, line)
		}
	}
	stats.Returned = len(entries)
	stats.Limited = stats.Matched > stats.Returned
	return strings.Join(entries, "\n"), stats
}
