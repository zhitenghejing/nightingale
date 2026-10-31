# Windows SNMP

Select this component when adding or editing a collection device. Each device receives an independent collection snapshot, metric documentation, native dashboard and native alert rules.

Enable Windows SNMP Service, and allow UDP 161 from the collector. Windows exposes HOST-RESOURCES-MIB, SNMPv2-MIB and IF-MIB only. Credentials and endpoints are configured on the device, never in this template.

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

[[table.field]]
name = "speed"
oid = ".1.3.6.1.2.1.31.1.1.1.15"

[[table.field]]
name = "in_errors"
oid = ".1.3.6.1.2.1.2.2.1.14"

[[table.field]]
name = "out_errors"
oid = ".1.3.6.1.2.1.2.2.1.20"

[[table.field]]
name = "in_discards"
oid = ".1.3.6.1.2.1.2.2.1.13"

[[table.field]]
name = "out_discards"
oid = ".1.3.6.1.2.1.2.2.1.19"

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

CPU is the average processor utilization (hrProcessorLoad). Memory uses hrStorageRam; disks use hrStorageFixedDisk per index.

Windows does not implement the 64-bit IF-MIB counters (ifHCInOctets / ifHCOutOctets), so this component collects the 32-bit counters (ifInOctets / ifOutOctets) only. Those counters wrap around on fast links, so traffic readings can be understated. Interface speed comes from ifHighSpeed (Mbps). Interface errors and discards come from ifInErrors / ifOutErrors and ifInDiscards / ifOutDiscards.

Unsupported OIDs produce no data and require an agent or template adjustment.

Default alerts: CPU, memory or disk above 90% for five minutes; failed SNMP or no uptime samples for three minutes, sustained for one minute; interface link down, interface bandwidth above 90% and interface error rate above 2 pps for five minutes. Native alert events are enabled. Configure notification routing on the independent device rules.

The other three standard tabs contain editable metrics, dashboards and alert rules. Device advanced settings override collection and metric documentation. Native instance editors customize dashboards and alerts. Ordinary saves preserve customizations. Explicit template reapplication retires the old instances and creates new ones, preserving alert history. Disabling or deleting a device suspends its rules.
