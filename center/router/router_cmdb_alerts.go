package router

import (
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"unicode"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/cmdb"
	"github.com/ccfos/nightingale/v6/pkg/ginx"
	"github.com/gin-gonic/gin"
)

type rackAlert struct {
	ID          int64  `json:"id,string"`
	RuleName    string `json:"rule_name"`
	Severity    int    `json:"severity"`
	TriggerTime int64  `json:"trigger_time"`
}

func (rt *Router) cmdbRackAlerts(c *gin.Context, user cmdb.User) {
	c.Header("Cache-Control", "no-store")
	allowed, err := rt.CMDB.Store.CanRead(c.Request.Context(), user)
	ginx.Dangerous(err)
	if !allowed {
		ginx.Bomb(http.StatusForbidden, "没有该 CMDB 操作权限，请联系管理员授权")
	}
	alerts, err := rt.readCMDBRackAlerts(c, user)
	ginx.NewRender(c).Data(alerts, err)
}

func (rt *Router) readCMDBRackAlerts(c *gin.Context, user cmdb.User) (map[string][]rackAlert, error) {
	result := make(map[string][]rackAlert)
	query := rt.Ctx.DB.WithContext(c.Request.Context()).Model(&models.AlertCurEvent{})
	// The detail page requires business-group access even when the global event list is open.
	if !user.Admin {
		groups, err := models.MyBusiGroupIds(rt.Ctx, user.ID)
		if err != nil || len(groups) == 0 {
			return result, err
		}
		query = query.Where("alert_cur_event.group_id IN ?", groups)
	}
	devices, err := rt.CMDB.Store.Assets(false)
	if err != nil {
		return nil, err
	}
	deviceIPs := rackDeviceIPs(devices)
	if len(deviceIPs) == 0 {
		return result, nil
	}
	query, err = rt.scopeDeviceEventDatasources(c, query)
	if err != nil {
		return nil, err
	}
	// Current events are streamed with only the columns used to associate or display them.
	rows, err := query.Select(`alert_cur_event.id, COALESCE(alert_cur_event.rule_name, ''), alert_cur_event.severity,
		COALESCE(alert_cur_event.trigger_time, 0), COALESCE(alert_cur_event.target_ident, ''),
		COALESCE(target.host_ip, ''), COALESCE(alert_cur_event.tags, ''), COALESCE(alert_cur_event.original_tags, '')`).
		Joins("LEFT JOIN target ON target.ident = alert_cur_event.target_ident AND target.ident <> ''").
		Where("alert_cur_event.severity IN ?", []int{1, 2, 3}).
		Order("alert_cur_event.severity, alert_cur_event.trigger_time DESC, alert_cur_event.id DESC").Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var event rackAlert
		var ident, hostIP, tags, originalTags string
		if err := rows.Scan(&event.ID, &event.RuleName, &event.Severity, &event.TriggerTime, &ident, &hostIP, &tags, &originalTags); err != nil {
			return nil, err
		}
		address := rackAlertIP(ident, hostIP, tags, originalTags)
		for _, id := range deviceIPs[address] {
			result[id] = append(result[id], event)
		}
	}
	return result, rows.Err()
}

func rackDeviceIPs(devices []cmdb.Document) map[netip.Addr][]string {
	result := make(map[netip.Addr][]string)
	for _, device := range devices {
		id, _ := device["id"].(string)
		if id == "" {
			continue
		}
		seen := make(map[netip.Addr]bool)
		for _, key := range []string{"businessIp", "managementIp"} {
			text, _ := device[key].(string)
			for _, value := range strings.FieldsFunc(text, func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune(",，、;；", r) }) {
				if addr := parseRackIP(value); addr.IsValid() && !seen[addr] {
					seen[addr] = true
					result[addr] = append(result[addr], id)
				}
			}
		}
	}
	return result
}

func rackAlertIP(ident, hostIP, tags, originalTags string) netip.Addr {
	addresses := make(map[string]netip.Addr)
	// Prefer current address labels, falling back to the original series labels.
	for _, raw := range []string{originalTags, tags} {
		for tag := range strings.SplitSeq(raw, ",,") {
			if key, value, ok := strings.Cut(tag, "="); ok {
				if addr := parseRackIP(value); addr.IsValid() {
					addresses[key] = addr
				}
			}
		}
	}
	// An explicit monitored endpoint takes precedence over the collector's identity or address.
	for _, key := range []string{"snmp_target", "device_ip", "target"} {
		if addr := addresses[key]; addr.IsValid() {
			return addr
		}
	}
	for _, value := range []string{ident, hostIP} {
		if addr := parseRackIP(value); addr.IsValid() {
			return addr
		}
	}
	for _, key := range []string{"ident", "host_ip", "ip", "agent_host", "instance", "host"} {
		if addr := addresses[key]; addr.IsValid() {
			return addr
		}
	}
	return netip.Addr{}
}

func parseRackIP(value string) netip.Addr {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "://") {
		endpoint, err := url.Parse(value)
		if err != nil {
			return netip.Addr{}
		}
		value = endpoint.Hostname()
	} else if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}
	}
	return addr.WithZone("").Unmap()
}
