package collection

import (
	"bytes"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// The typed recipe cannot replace endpoints, credentials or ownership labels.
// These options correspond to the pinned Categraf MySQL input.
type mysqlRecipe struct {
	MetricsPass       []string `toml:"metrics_pass"`
	RequiredMetrics   []string `toml:"required_metrics"`
	GatherProcessList bool     `toml:"gather_processlist_processes_by_state"`
	GatherSchemaSize  bool     `toml:"gather_schema_size"`
	GatherTableSize   bool     `toml:"gather_table_size"`
	GatherSlaveStatus bool     `toml:"gather_slave_status"`
	GatherInnodb      bool     `toml:"gather_innodb_status"`
	GatherBinlog      bool     `toml:"gather_binlog"`
}

func decodeMySQLHistorical(entries map[string]ConfigContent, collector string) (map[string]historicalDevice, error) {
	out := map[string]historicalDevice{}
	for hash, content := range entries {
		if content.Format != "toml" || digest([]byte(content.Config)) != hash {
			return nil, bad("historical content integrity failed")
		}
		var config struct {
			Interval  int             `toml:"interval"`
			Instances []mysqlInstance `toml:"instances"`
		}
		if err := toml.Unmarshal([]byte(content.Config), &config); err != nil || len(config.Instances) != 1 {
			return nil, bad("invalid historical MySQL configuration")
		}
		ins := config.Instances[0]
		if ins.Labels["collection_collector_id"] != collector || ins.Labels["collection_mode"] != "managed" || ins.Labels["collection_device_id"] == "" || ins.Labels["collection_template_version"] == "" {
			return nil, bad("historical MySQL ownership mismatch")
		}
		host, port, err := net.SplitHostPort(ins.Address)
		if err != nil {
			return nil, bad("invalid historical MySQL endpoint")
		}
		p, err := strconv.Atoi(port)
		if err != nil {
			return nil, bad("invalid historical MySQL port")
		}
		labels := map[string]string{}
		for k, v := range ins.Labels {
			if !reservedLabel(k) {
				labels[k] = v
			}
		}
		tls := "off"
		if ins.Parameters == "tls=true" {
			tls = "verify"
		} else if ins.Parameters != "tls=false" {
			return nil, bad("invalid historical MySQL parameters")
		}
		out[ins.Labels["collection_device_id"]] = historicalDevice{address: host, port: p, interval: config.Interval, timeout: ins.TimeoutSeconds, labels: labels, templateID: ins.Labels["collection_template_version"], protocol: "mysql", parameters: map[string]interface{}{"username": ins.Username, "tls": tls}, secrets: map[string]interface{}{"password": ins.Password}}
	}
	return out, nil
}

type mysqlInstance struct {
	MetricsPass              []string          `toml:"metrics_pass"`
	Address                  string            `toml:"address"`
	Username                 string            `toml:"username"`
	Password                 string            `toml:"password"`
	Parameters               string            `toml:"parameters"`
	TimeoutSeconds           int               `toml:"timeout_seconds"`
	Labels                   map[string]string `toml:"labels"`
	GatherProcessList        bool              `toml:"gather_processlist_processes_by_state"`
	GatherSchemaSize         bool              `toml:"gather_schema_size"`
	GatherTableSize          bool              `toml:"gather_table_size"`
	GatherSlaveStatus        bool              `toml:"gather_slave_status"`
	DisableInnodbStatus      bool              `toml:"disable_innodb_status"`
	DisableExtraInnodbStatus bool              `toml:"disable_extra_innodb_status"`
	DisableBinlogs           bool              `toml:"disable_binlogs"`
}

var mysqlMetrics = []string{"mysql_up", "mysql_global_status_threads_connected", "mysql_global_status_threads_running", "mysql_global_status_questions", "mysql_global_status_slow_queries", "mysql_global_status_uptime", "mysql_global_variables_max_connections"}

func parseMySQL(config string) (mysqlRecipe, error) {
	var recipe mysqlRecipe
	if err := toml.NewDecoder(bytes.NewBufferString(config)).DisallowUnknownFields().Decode(&recipe); err != nil {
		return recipe, bad("invalid typed MySQL collection recipe")
	}
	if len(recipe.RequiredMetrics) == 0 {
		recipe.RequiredMetrics = []string{"mysql_up", "mysql_global_status_threads_connected", "mysql_global_variables_max_connections"}
	}
	if len(recipe.MetricsPass) == 0 {
		recipe.MetricsPass = append([]string{}, mysqlMetrics...)
	}
	if len(recipe.MetricsPass) > 512 {
		return recipe, bad("too many MySQL metrics")
	}
	allowed := map[string]bool{}
	for _, name := range recipe.MetricsPass {
		if !labelName.MatchString(name) || !strings.HasPrefix(name, "mysql_") || len(name) > 128 || allowed[name] {
			return recipe, bad("MySQL metric filters require unique exact mysql_ metric names")
		}
		allowed[name] = true
	}
	if !allowed["mysql_up"] {
		return recipe, bad("MySQL metric filters must include mysql_up")
	}
	for _, metric := range recipe.RequiredMetrics {
		if !allowed[metric] {
			return recipe, bad("required MySQL metrics must be included in metrics_pass")
		}
	}
	return recipe, nil
}

func mysqlContract(config string) (TemplateConfig, error) {
	recipe, err := parseMySQL(config)
	return TemplateConfig{RequiredMetrics: recipe.RequiredMetrics, ProducedMetrics: recipe.MetricsPass}, err
}

func compileMySQL(f DeviceInput, id, revision string, params map[string]interface{}, recipeText string, mode ...string) (ConfigContent, error) {
	recipe, err := parseMySQL(recipeText)
	if err != nil {
		return ConfigContent{}, err
	}
	username, _ := params["username"].(string)
	password, _ := params["password"].(string)
	if username == "" || strings.ContainsAny(username, ":@/\x00\r\n") {
		return ConfigContent{}, bad("invalid MySQL monitoring username")
	}
	tls := "false"
	if params["tls"] == "verify" {
		tls = "true"
	} else if params["tls"] != nil && params["tls"] != "off" {
		return ConfigContent{}, bad("unsupported MySQL TLS mode")
	}
	labels := map[string]string{"ident": f.Ident, "collection_device_id": id, "collection_collector_id": f.CollectorID, "collection_revision": revision, "collection_mode": "managed", "collection_template_version": f.TemplateVersionID}
	if len(mode) != 0 && mode[0] == "probe" {
		delete(labels, "collection_device_id")
		labels["probe_id"] = id
		labels["collection_mode"] = "probe"
	}
	for k, v := range f.Labels {
		labels[k] = v
	}
	instance := mysqlInstance{MetricsPass: recipe.MetricsPass, Address: net.JoinHostPort(f.Address, fmt.Sprint(f.Port)), Username: username, Password: password, Parameters: "tls=" + tls, TimeoutSeconds: f.TimeoutSeconds, Labels: labels, GatherProcessList: recipe.GatherProcessList, GatherSchemaSize: recipe.GatherSchemaSize, GatherTableSize: recipe.GatherTableSize, GatherSlaveStatus: recipe.GatherSlaveStatus}
	instance.DisableInnodbStatus = !recipe.GatherInnodb
	instance.DisableExtraInnodbStatus = !recipe.GatherInnodb
	instance.DisableBinlogs = !recipe.GatherBinlog
	config := map[string]interface{}{"interval": f.IntervalSeconds, "instances": []interface{}{instance}}
	b, err := toml.Marshal(config)
	if err != nil {
		return ConfigContent{}, bad("MySQL configuration encoding failed")
	}
	return ConfigContent{Format: "toml", Config: string(b)}, nil
}
