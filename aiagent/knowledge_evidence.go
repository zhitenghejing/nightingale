package aiagent

import "encoding/json"

func IsKnowledgeDataTool(name string) bool {
	return name == "query_knowledge_data" || name == "target_statistics"
}

// Legacy workbook entry remains callable for saved/general workflows.
func IsKnowledgeWorkbookTool(name string) bool {
	switch name {
	case "query_knowledge_workbook", "list_workbook_sheets", "read_workbook_range", "count_workbook_rows", "sum_workbook_column":
		return true
	}
	return false
}

// KnowledgeDataEvidence is a server-created scope, not model-supplied citations.
// Query time never substitutes for source/sample time in Result.
type KnowledgeDataEvidence struct {
	AssetRefs          map[string]string `json:"asset_refs,omitempty"`
	AssetLookupSerial  string            `json:"asset_lookup_serial,omitempty"`
	AssetLookupID      string            `json:"asset_lookup_id,omitempty"`
	InventoryScope     string            `json:"inventory_scope,omitempty"`
	WorkbookVersion    int64             `json:"workbook_version,omitempty"`
	WorkbookDigest     string            `json:"workbook_digest,omitempty"`
	SourceLabel        string            `json:"source_label,omitempty"`
	SourceURL          string            `json:"source_url,omitempty"`
	EnvironmentRefs    map[string]string `json:"environment_refs,omitempty"`
	DatasourceVersions map[int64]string  `json:"datasource_versions,omitempty"`
	TargetVersions     map[string]string `json:"target_versions,omitempty"`
	Operation          string            `json:"operation"`
	DatasourceIDs      []int64           `json:"datasource_ids,omitempty"`
	TargetIdents       []string          `json:"target_idents,omitempty"`
	QueriedAt          int64             `json:"queried_at"`
	Query              string            `json:"query,omitempty"`
	TimeRange          string            `json:"time_range,omitempty"`
	Result             json.RawMessage   `json:"result"`
	Truncated          bool              `json:"truncated"`
}
