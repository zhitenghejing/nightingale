# Windows SNMP Extended

A native Categraf SNMP v2c integration reconstructed from the supplied base.py collection intent. The Python script is not executed. Select Operating systems, Microsoft, Windows generic, then this integration in device onboarding. Credentials and endpoints belong to the device, never the template.

## Metrics and optional collection
CPU, RAM, volumes, uptime and interface counters form the baseline. Processor model, interface metadata/MAC, processes and IPv4 routes extend it. The tcp, tcp6 and listener tables are disabled initially to avoid high-cardinality connection series. Remove table names from disabled_tables to enable them. Add process, route or interface_info to disable those optional tables. All tables use partial errors; only uptime, CPU and storage metrics are required. Unsupported or disabled optional metrics remain absent instead of fabricated zero values.

## Identity and units
Nightingale adds the authoritative device_name label after verifying ownership for managed SNMP, MySQL and ICMP samples. Rename affects new samples; historical labels remain unchanged. Keep ident and collection_device_id as stable identifiers. Native Agent host metrics are outside this template; authorized Agent ICMP uses its inventory name, ident.
Counters remain bytes, network chart rates use bits per second, ifHighSpeed is multiplied by 1000000. Capacity is size times allocation units. Process CPU is cumulative centiseconds; divide by 100 for seconds and divide its rate by core count for host CPU percent. CPU rows lacking load are excluded from core counts. System uptime uses HOST-RESOURCES-MIB TimeTicks divided by 100.

## Compatibility
Use actual 64-bit counters when available, otherwise 32-bit counters. Fast links may wrap 32-bit counters multiple times between polls. Some Windows agents lack duplex, 64-bit interface counters or modern listener tables. Numeric interface collection is independent of potentially invalid encoded descriptions. Volumes use index and storage type. Process names and SNMP index identify individual processes. Legacy IPv4 listeners use tcpConnState=2; modern listeners use tcpListenerTable. Service names are inferred only from selected destination ports, not application health or traffic accounting.

## Dashboards, alerts and customization
The dashboard covers resources, interfaces, processes, optional TCP details and routing. CPU, RAM and disk utilization above 90 percent for five minutes and SNMP interruption create native events. Configure notification routing separately. Common ICMP probes are independent from SNMP health. Device overrides and pinned versions are retained; publishing does not upgrade existing devices or change Windows_SNMP.

## Collection definition

```snmp-collection
required_metrics = ["snmp_uptime", "snmp_cpu_load", "snmp_storage_size", "snmp_storage_used"]
disabled_tables = ["tcp", "tcp6", "listener"]

[[field]]
name = "system_uptime"
oid = ".1.3.6.1.2.1.25.1.1.0"

[[field]]
name = "memory_total_kib"
oid = ".1.3.6.1.2.1.25.2.2.0"

[[field]]
name = "interface_count"
oid = ".1.3.6.1.2.1.2.1.0"

[[table]]
name = "cpu"
index_as_tag = true
error_policy = "partial"

[[table.field]]
name = "load"
oid = ".1.3.6.1.2.1.25.3.3.1.2"

[[table.field]]
name = "model"
oid = ".1.3.6.1.2.1.25.3.2.1.3"
is_tag = true

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

[[table.field]]
name = "duplex"
oid = ".1.3.6.1.2.1.10.7.2.1.19"

[[table]]
name = "interface_info"
index_as_tag = true
error_policy = "partial"

[[table.field]]
name = "type"
oid = ".1.3.6.1.2.1.2.2.1.3"

[[table.field]]
name = "name"
oid = ".1.3.6.1.2.1.31.1.1.1.1"
is_tag = true

[[table.field]]
name = "alias"
oid = ".1.3.6.1.2.1.31.1.1.1.18"
is_tag = true

[[table.field]]
name = "mac"
oid = ".1.3.6.1.2.1.2.2.1.6"
is_tag = true
conversion = "hwaddr"

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
name = "process"
index_as_tag = true
error_policy = "partial"

[[table.field]]
name = "name"
oid = ".1.3.6.1.2.1.25.4.2.1.2"
is_tag = true

[[table.field]]
name = "process_type"
oid = ".1.3.6.1.2.1.25.4.2.1.6"

[[table.field]]
name = "cpu_time"
oid = ".1.3.6.1.2.1.25.5.1.1.1"

[[table.field]]
name = "memory_kib"
oid = ".1.3.6.1.2.1.25.5.1.1.2"

[[table]]
name = "tcp"
index_as_tag = true
error_policy = "partial"

[[table.field]]
name = "state"
oid = ".1.3.6.1.2.1.6.13.1.1"

[[table.field]]
name = "local_address"
oid = ".1.3.6.1.2.1.6.13.1.2"
is_tag = true

[[table.field]]
name = "local_port"
oid = ".1.3.6.1.2.1.6.13.1.3"
is_tag = true

[[table.field]]
name = "remote_address"
oid = ".1.3.6.1.2.1.6.13.1.4"
is_tag = true

[[table.field]]
name = "remote_port"
oid = ".1.3.6.1.2.1.6.13.1.5"
is_tag = true

[[table]]
name = "tcp6"
index_as_tag = true
error_policy = "partial"

[[table.field]]
name = "state"
oid = ".1.3.6.1.2.1.6.19.1.7"

[[table.field]]
name = "pid"
oid = ".1.3.6.1.2.1.6.19.1.8"

[[table]]
name = "listener"
index_as_tag = true
error_policy = "partial"

[[table.field]]
name = "pid"
oid = ".1.3.6.1.2.1.6.20.1.4"

[[table]]
name = "route"
index_as_tag = true
error_policy = "partial"

[[table.field]]
name = "route_type"
oid = ".1.3.6.1.2.1.4.21.1.8"

[[table.field]]
name = "destination"
oid = ".1.3.6.1.2.1.4.21.1.1"
is_tag = true

[[table.field]]
name = "gateway"
oid = ".1.3.6.1.2.1.4.21.1.7"
is_tag = true

[[table.field]]
name = "mask"
oid = ".1.3.6.1.2.1.4.21.1.11"
is_tag = true
```

References: RFC 2790, RFC 2863 and RFC 4022. Windows service state, event logs, disk IOPS and RAID health are not collected.

## Onboarding declaration

```device-onboarding
{
  "adapter": "snmp_v2c",
  "execution": "collector",
  "reachability": "icmp",
  "config": "required_metrics = [\"snmp_uptime\", \"snmp_cpu_load\", \"snmp_storage_size\", \"snmp_storage_used\"]\ndisabled_tables = [\"tcp\", \"tcp6\", \"listener\"]\n\n[[field]]\nname = \"system_uptime\"\noid = \".1.3.6.1.2.1.25.1.1.0\"\n\n[[field]]\nname = \"memory_total_kib\"\noid = \".1.3.6.1.2.1.25.2.2.0\"\n\n[[field]]\nname = \"interface_count\"\noid = \".1.3.6.1.2.1.2.1.0\"\n\n[[table]]\nname = \"cpu\"\nindex_as_tag = true\nerror_policy = \"partial\"\n\n[[table.field]]\nname = \"load\"\noid = \".1.3.6.1.2.1.25.3.3.1.2\"\n\n[[table.field]]\nname = \"model\"\noid = \".1.3.6.1.2.1.25.3.2.1.3\"\nis_tag = true\n\n[[table]]\nname = \"storage\"\nindex_as_tag = true\nerror_policy = \"partial\"\n\n[[table.field]]\nname = \"type\"\noid = \".1.3.6.1.2.1.25.2.3.1.2\"\nis_tag = true\n\n[[table.field]]\nname = \"allocation_units\"\noid = \".1.3.6.1.2.1.25.2.3.1.4\"\n\n[[table.field]]\nname = \"size\"\noid = \".1.3.6.1.2.1.25.2.3.1.5\"\n\n[[table.field]]\nname = \"used\"\noid = \".1.3.6.1.2.1.25.2.3.1.6\"\n\n[[table]]\nname = \"interface\"\nindex_as_tag = true\nerror_policy = \"partial\"\n\n[[table.field]]\nname = \"type\"\noid = \".1.3.6.1.2.1.2.2.1.3\"\n\n[[table.field]]\nname = \"oper_status\"\noid = \".1.3.6.1.2.1.2.2.1.8\"\n\n[[table.field]]\nname = \"admin_status\"\noid = \".1.3.6.1.2.1.2.2.1.7\"\n\n[[table.field]]\nname = \"speed\"\noid = \".1.3.6.1.2.1.31.1.1.1.15\"\n\n[[table.field]]\nname = \"in_errors\"\noid = \".1.3.6.1.2.1.2.2.1.14\"\n\n[[table.field]]\nname = \"out_errors\"\noid = \".1.3.6.1.2.1.2.2.1.20\"\n\n[[table.field]]\nname = \"in_discards\"\noid = \".1.3.6.1.2.1.2.2.1.13\"\n\n[[table.field]]\nname = \"out_discards\"\noid = \".1.3.6.1.2.1.2.2.1.19\"\n\n[[table.field]]\nname = \"duplex\"\noid = \".1.3.6.1.2.1.10.7.2.1.19\"\n\n[[table]]\nname = \"interface_info\"\nindex_as_tag = true\nerror_policy = \"partial\"\n\n[[table.field]]\nname = \"type\"\noid = \".1.3.6.1.2.1.2.2.1.3\"\n\n[[table.field]]\nname = \"name\"\noid = \".1.3.6.1.2.1.31.1.1.1.1\"\nis_tag = true\n\n[[table.field]]\nname = \"alias\"\noid = \".1.3.6.1.2.1.31.1.1.1.18\"\nis_tag = true\n\n[[table.field]]\nname = \"mac\"\noid = \".1.3.6.1.2.1.2.2.1.6\"\nis_tag = true\nconversion = \"hwaddr\"\n\n[[table]]\nname = \"traffic32\"\nindex_as_tag = true\nerror_policy = \"partial\"\n\n[[table.field]]\nname = \"in_octets\"\noid = \".1.3.6.1.2.1.2.2.1.10\"\n\n[[table.field]]\nname = \"out_octets\"\noid = \".1.3.6.1.2.1.2.2.1.16\"\n\n[[table]]\nname = \"traffic64\"\nindex_as_tag = true\nerror_policy = \"partial\"\n\n[[table.field]]\nname = \"in_octets\"\noid = \".1.3.6.1.2.1.31.1.1.1.6\"\n\n[[table.field]]\nname = \"out_octets\"\noid = \".1.3.6.1.2.1.31.1.1.1.10\"\n\n[[table]]\nname = \"process\"\nindex_as_tag = true\nerror_policy = \"partial\"\n\n[[table.field]]\nname = \"name\"\noid = \".1.3.6.1.2.1.25.4.2.1.2\"\nis_tag = true\n\n[[table.field]]\nname = \"process_type\"\noid = \".1.3.6.1.2.1.25.4.2.1.6\"\n\n[[table.field]]\nname = \"cpu_time\"\noid = \".1.3.6.1.2.1.25.5.1.1.1\"\n\n[[table.field]]\nname = \"memory_kib\"\noid = \".1.3.6.1.2.1.25.5.1.1.2\"\n\n[[table]]\nname = \"tcp\"\nindex_as_tag = true\nerror_policy = \"partial\"\n\n[[table.field]]\nname = \"state\"\noid = \".1.3.6.1.2.1.6.13.1.1\"\n\n[[table.field]]\nname = \"local_address\"\noid = \".1.3.6.1.2.1.6.13.1.2\"\nis_tag = true\n\n[[table.field]]\nname = \"local_port\"\noid = \".1.3.6.1.2.1.6.13.1.3\"\nis_tag = true\n\n[[table.field]]\nname = \"remote_address\"\noid = \".1.3.6.1.2.1.6.13.1.4\"\nis_tag = true\n\n[[table.field]]\nname = \"remote_port\"\noid = \".1.3.6.1.2.1.6.13.1.5\"\nis_tag = true\n\n[[table]]\nname = \"tcp6\"\nindex_as_tag = true\nerror_policy = \"partial\"\n\n[[table.field]]\nname = \"state\"\noid = \".1.3.6.1.2.1.6.19.1.7\"\n\n[[table.field]]\nname = \"pid\"\noid = \".1.3.6.1.2.1.6.19.1.8\"\n\n[[table]]\nname = \"listener\"\nindex_as_tag = true\nerror_policy = \"partial\"\n\n[[table.field]]\nname = \"pid\"\noid = \".1.3.6.1.2.1.6.20.1.4\"\n\n[[table]]\nname = \"route\"\nindex_as_tag = true\nerror_policy = \"partial\"\n\n[[table.field]]\nname = \"route_type\"\noid = \".1.3.6.1.2.1.4.21.1.8\"\n\n[[table.field]]\nname = \"destination\"\noid = \".1.3.6.1.2.1.4.21.1.1\"\nis_tag = true\n\n[[table.field]]\nname = \"gateway\"\noid = \".1.3.6.1.2.1.4.21.1.7\"\nis_tag = true\n\n[[table.field]]\nname = \"mask\"\noid = \".1.3.6.1.2.1.4.21.1.11\"\nis_tag = true\n",
  "notification": "events_only",
  "compatibility": "Windows SNMP v2c; numeric HOST-RESOURCES, IF and TCP/IP OIDs. Optional OIDs depend on the agent. Validate on the target OS version.",
  "fields": [
    {
      "key": "protocol",
      "label": "SNMP version",
      "type": "select",
      "required": true,
      "default": "snmp_v2c",
      "options": [
        "snmp_v2c"
      ]
    },
    {
      "key": "port",
      "label": "SNMP port",
      "type": "number",
      "required": true,
      "default": 161,
      "min": 1,
      "max": 65535
    },
    {
      "key": "credential_id",
      "label": "SNMP community credential",
      "type": "credential",
      "required": true,
      "help": "Credentials are encrypted. One collector supports multiple devices."
    }
  ],
  "notify": {
    "mode": "events_only",
    "rule_ids": [],
    "message": "Device resource alert. Inspect the native event for device name, value and timestamp."
  }
}
```
