# MySQL 8.4 LTS remote monitoring

Select Database / MySQL / 8.4 LTS in the device configuration wizard. Enter the database IP, monitoring username/password and port, then choose an authorized Categraf collector. No local target configuration is required. The same collector can monitor multiple independent endpoints.

The typed recipe gathers global status and global variables. Use a monitoring account with the permissions required by these queries; schema/table/process-list/replication collection is disabled unless explicitly enabled in the package. Passwords are encrypted and never returned by device read APIs. TLS verification uses the collector host trust store and requires a certificate valid for the selected IP.

Required metrics: mysql_up, mysql_global_status_threads_connected and mysql_global_variables_max_connections. The package also documents query count, slow queries, running threads and uptime. Each device receives an independent dashboard and native alert rules. Rules wait for the complete first metric contract; the default notification mode creates events only. Select an authorized native notification rule when delivery is required.

Compatibility evidence: MySQL 8.4.11, pinned Categraf v0.5.18, authenticated HTTP Provider and managed Remote Write fixture. This is not evidence for other database versions or physical hardware.

The following declaration bootstraps the editable draft once. Subsequent editing and publication use the template package stored in the database.

```device-onboarding
{
  "adapter": "mysql",
  "config": "required_metrics = [\"mysql_up\", \"mysql_global_status_threads_connected\", \"mysql_global_variables_max_connections\"]\n",
  "notification": "events_only",
  "execution": "collector",
  "compatibility": "Protocol fixture verified with MySQL 8.4.11 and pinned Categraf v0.5.18. Validate monitoring grants and target TLS certificates in your environment.",
  "fields": [
    {
      "key": "port",
      "label": "MySQL 端口",
      "type": "number",
      "required": true,
      "default": 3306,
      "min": 1,
      "max": 65535
    },
    {
      "key": "username",
      "label": "监控账号",
      "type": "text",
      "required": true
    },
    {
      "key": "password",
      "label": "监控密码",
      "type": "password",
      "required": true,
      "secret": true
    },
    {
      "key": "tls",
      "label": "连接加密",
      "type": "select",
      "required": true,
      "default": "off",
      "options": [
        "off",
        "verify"
      ]
    }
  ],
  "notify": {
    "mode": "events_only",
    "rule_ids": [],
    "message": "【{{告警级别}}】{{设备名称}}: {{规则名称}}\nValue: {{当前值}}; time: {{发生时间}}"
  }
}
```
