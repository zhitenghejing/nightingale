# Linux SNMP

Select this component when adding or editing a collection device. Each device receives an independent collection snapshot, metric documentation, native dashboard and native alert rules.

Enable Net-SNMP snmpd with HOST-RESOURCES-MIB and IF-MIB views, and allow UDP 161 from the collector. Credentials and endpoints are configured on the device, never in this template.

Edit the following fenced block to adjust numeric OIDs. Only fields, tables and required_metrics are accepted. Uptime and ownership labels are injected by the server.

```snmp-collection
required_metrics = ["snmp_uptime", "snmp_cpu_load", "snmp_storage_size", "snmp_storage_used"]

[[table]]
name = "cpu"
index_as_tag = true
error_policy = "partial"

[[table.field]]
name = "load"
oid = ".1.3.6.1.2.1.25.3.3.1.2"

[[table]]
name = "storage"
index_as_tag = true
error_policy = "partial"

[[table.field]]
name = "type"
oid = ".1.3.6.1.2.1.25.2.3.1.2"
is_tag = true

[[table.field]]
name = "allocation_units"
oid = ".1.3.6.1.2.1.25.2.3.1.4"

[[table.field]]
name = "size"
oid = ".1.3.6.1.2.1.25.2.3.1.5"

[[table.field]]
name = "used"
oid = ".1.3.6.1.2.1.25.2.3.1.6"

[[table]]
name = "interface"
index_as_tag = true
error_policy = "partial"

[[table.field]]
name = "type"
oid = ".1.3.6.1.2.1.2.2.1.3"

[[table.field]]
name = "oper_status"
oid = ".1.3.6.1.2.1.2.2.1.8"

[[table.field]]
name = "admin_status"
oid = ".1.3.6.1.2.1.2.2.1.7"

[[table]]
name = "traffic64"
index_as_tag = true
error_policy = "partial"

[[table.field]]
name = "in_octets"
oid = ".1.3.6.1.2.1.31.1.1.1.6"

[[table.field]]
name = "out_octets"
oid = ".1.3.6.1.2.1.31.1.1.1.10"

[[table]]
name = "traffic32"
index_as_tag = true
error_policy = "partial"

[[table.field]]
name = "in_octets"
oid = ".1.3.6.1.2.1.2.2.1.10"

[[table.field]]
name = "out_octets"
oid = ".1.3.6.1.2.1.2.2.1.16"
```

CPU is the average processor utilization. Memory uses hrStorageRam; disks use hrStorageFixedDisk per index. Network traffic prefers 64-bit counters, with 32-bit fallback. Unsupported OIDs produce no data and require an agent or template adjustment.

Default alerts: CPU, memory or disk above 90% for five minutes; failed SNMP or no uptime samples for three minutes, sustained for one minute. Native alert events are enabled. Configure notification routing on the independent device rules.

The other three standard tabs contain editable metrics, dashboards and alert rules. Device advanced settings override collection and metric documentation. Native instance editors customize dashboards and alerts. Ordinary saves preserve customizations. Explicit template reapplication retires the old instances and creates new ones, preserving alert history. Disabling or deleting a device suspends its rules.
