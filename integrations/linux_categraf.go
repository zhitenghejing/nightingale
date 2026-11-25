package integrations

import "embed"

// LinuxCategraf provides the native Linux assets used by the ARM and X86 Agent templates.
//
//go:embed Linux/markdown/README.md Linux/metrics/categraf-base.json Linux/dashboards/categraf-detail.json Linux/alerts/linux_by_categraf.json
var LinuxCategraf embed.FS
