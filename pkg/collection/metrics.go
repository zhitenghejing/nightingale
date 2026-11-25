package collection

import "github.com/ccfos/nightingale/v6/models"

// MetricQueries keeps every graph scoped to the selected device and revision.
func MetricQueries(config TemplateConfig, selector string) map[string]string {
	queries := map[string]string{
		"cpu":         `snmp_cpu_load{` + selector + `}`,
		"memory":      `100 * snmp_storage_used{type=~"\\.?1\\.3\\.6\\.1\\.2\\.1\\.25\\.2\\.1\\.2",` + selector + `} / (snmp_storage_size{type=~"\\.?1\\.3\\.6\\.1\\.2\\.1\\.25\\.2\\.1\\.2",` + selector + `} > 0)`,
		"disk":        `100 * snmp_storage_used{type=~"\\.?1\\.3\\.6\\.1\\.2\\.1\\.25\\.2\\.1\\.4",` + selector + `} / (snmp_storage_size{type=~"\\.?1\\.3\\.6\\.1\\.2\\.1\\.25\\.2\\.1\\.4",` + selector + `} > 0)`,
		"network_in":  `8 * (rate(snmp_traffic64_in_octets{` + selector + `}[5m]) or on(index) rate(snmp_traffic32_in_octets{` + selector + `}[5m]))`,
		"network_out": `8 * (rate(snmp_traffic64_out_octets{` + selector + `}[5m]) or on(index) rate(snmp_traffic32_out_octets{` + selector + `}[5m]))`,
	}
	activeInterfaces := ` and on(index) (snmp_interface_oper_status{` + selector + `} == 1) and on(index) (snmp_interface_admin_status{` + selector + `} == 1) and on(index) ((snmp_interface_type{` + selector + `} == 6) or (snmp_interface_type{` + selector + `} == 71))`
	queries["network_in"] += activeInterfaces
	queries["network_out"] += activeInterfaces

	available := map[string]bool{}
	for _, name := range config.MetricNames() {
		available[name] = true
	}
	storageType := false
	for _, table := range config.Tables {
		if table.Name == "storage" {
			for _, f := range table.Fields {
				storageType = storageType || (f.Name == "type" && f.IsTag)
			}
		}
	}
	covered := map[string]bool{}
	groups := map[string][]string{
		"cpu": {"snmp_cpu_load"}, "memory": {"snmp_storage_size", "snmp_storage_used"},
		"disk":        {"snmp_storage_size", "snmp_storage_used"},
		"network_in":  {"snmp_traffic64_in_octets", "snmp_traffic32_in_octets"},
		"network_out": {"snmp_traffic64_out_octets", "snmp_traffic32_out_octets"},
	}
	for key, names := range groups {
		present := false
		for _, name := range names {
			present = present || available[name]
			covered[name] = true
		}
		if !present {
			delete(queries, key)
		}
	}
	if !available["snmp_interface_type"] || !available["snmp_interface_oper_status"] || !available["snmp_interface_admin_status"] {
		queries["network_in"] = `8 * (rate(snmp_traffic64_in_octets{` + selector + `}[5m]) or on(index) rate(snmp_traffic32_in_octets{` + selector + `}[5m]))`
		queries["network_out"] = `8 * (rate(snmp_traffic64_out_octets{` + selector + `}[5m]) or on(index) rate(snmp_traffic32_out_octets{` + selector + `}[5m]))`
		if !available["snmp_traffic64_in_octets"] && !available["snmp_traffic32_in_octets"] {
			delete(queries, "network_in")
		}
		if !available["snmp_traffic64_out_octets"] && !available["snmp_traffic32_out_octets"] {
			delete(queries, "network_out")
		}
	}
	if !storageType || !available["snmp_storage_size"] || !available["snmp_storage_used"] {
		delete(queries, "memory")
		delete(queries, "disk")
		delete(covered, "snmp_storage_size")
		delete(covered, "snmp_storage_used")
	}
	if queries["network_in"] != "" || queries["network_out"] != "" {
		covered["snmp_interface_type"] = true
		covered["snmp_interface_oper_status"] = true
		covered["snmp_interface_admin_status"] = true
	}
	if queries["memory"] != "" || queries["disk"] != "" {
		covered["snmp_storage_allocation_units"] = true
	}
	if len(queries) == 5 {
		covered["snmp_uptime"] = true
	}
	for _, name := range config.MetricNames() {
		if !covered[name] {
			queries[name] = name + "{" + selector + "}"
		}
	}
	return queries
}

// ProfileMetricQueries accepts only names saved in this device's profile and
// rebinds every vector selector to its current collector and revision.
func (s *Store) ProfileMetricQueries(d models.MonitorDevice, names []string) (map[string]string, error) {
	if len(names) > 16 {
		return nil, bad("too many profile metrics")
	}
	profile, err := s.DeviceProfile(d)
	if err != nil {
		return nil, err
	}
	available := map[string]string{}
	if profile != nil {
		for _, metric := range profile.Metrics {
			available[metric.Name] = metric.Expression
		}
	}
	queries := map[string]string{}
	for _, name := range names {
		if available[name] == "" {
			return nil, bad("unknown profile metric")
		}
		query, err := scopeExpression(available[name], d)
		if err != nil {
			return nil, bad("invalid profile metric expression")
		}
		queries[name] = query
	}
	return queries, nil
}
