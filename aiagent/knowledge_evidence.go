package aiagent

import "encoding/json"

// KnowledgeDataEvidence is a server-created scope, not model-supplied citations.
// Query time never substitutes for source/sample time in Result.
type KnowledgeDataEvidence struct {
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
