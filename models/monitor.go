package models

// MonitorText shares the repository's dialect-aware unbounded document storage.
type MonitorText = ArchiveJSON

type MonitorCollector struct {
	ID                 string      `json:"id" gorm:"primaryKey;size:36"`
	Name               string      `json:"name" gorm:"size:128"`
	AgentIdent         string      `json:"agent_ident" gorm:"size:191"`
	DatasourceID       int64       `json:"datasource_id"`
	Networks           MonitorText `json:"-"`
	Ports              MonitorText `json:"-"`
	TokenHash          string      `json:"-" gorm:"size:64"`
	PreviousTokenHash  string      `json:"-" gorm:"size:64"`
	PreviousTokenUntil int64       `json:"-"`
	WriterUser         string      `json:"writer_user" gorm:"size:191;unique"`
	Enabled            bool        `json:"enabled"`
	RecordVersion      int64       `json:"record_version"`
	DesiredVersion     string      `json:"desired_version" gorm:"size:64"`
	LastServedVersion  string      `json:"last_served_version" gorm:"size:64"`
	LastClientVersion  string      `json:"last_client_version" gorm:"size:64"`
	LastFetchedAt      int64       `json:"last_fetched_at"`
	ClientSeenAt       int64       `json:"client_seen_at"`
}

func (MonitorCollector) TableName() string { return "monitor_collector" }

type MonitorCollectorGroup struct {
	CollectorID string `json:"collector_id" gorm:"primaryKey;size:36"`
	GroupID     int64  `json:"group_id" gorm:"primaryKey;autoIncrement:false"`
}

func (MonitorCollectorGroup) TableName() string { return "monitor_collector_group" }

type MonitorCredential struct {
	ID            string      `json:"id" gorm:"primaryKey;size:36"`
	Name          string      `json:"name" gorm:"size:128"`
	GroupID       int64       `json:"group_id" gorm:"index"`
	Kind          string      `json:"kind" gorm:"size:32"`
	Ciphertext    MonitorText `json:"-"`
	KeyID         string      `json:"-" gorm:"size:64"`
	RecordVersion int64       `json:"record_version"`
	UpdatedAt     int64       `json:"updated_at"`
}

func (MonitorCredential) TableName() string { return "monitor_credential" }

type MonitorDevice struct {
	ID                      string      `json:"id" gorm:"primaryKey;size:36"`
	Name                    string      `json:"name" gorm:"size:128"`
	Ident                   string      `json:"ident" gorm:"size:191;unique"`
	Address                 string      `json:"address" gorm:"size:45"`
	Port                    int         `json:"port"`
	Protocol                string      `json:"protocol" gorm:"size:32"`
	GroupID                 int64       `json:"group_id" gorm:"index"`
	CollectorID             string      `json:"collector_id" gorm:"size:36;index"`
	CredentialID            string      `json:"credential_id" gorm:"size:36;index"`
	ProfileState            MonitorText `json:"-"`
	Parameters              MonitorText `json:"-"`
	Notify                  MonitorText `json:"-"`
	TemplateVersionID       string      `json:"template_version_id" gorm:"size:64"`
	IntervalSeconds         int         `json:"interval_seconds"`
	TimeoutSeconds          int         `json:"timeout_seconds"`
	Retries                 int         `json:"retries"`
	Labels                  MonitorText `json:"-"`
	CMDBID                  string      `json:"cmdb_id" gorm:"size:36"`
	Enabled                 bool        `json:"enabled"`
	Deleted                 bool        `json:"deleted" gorm:"index"`
	RecordVersion           int64       `json:"record_version"`
	DesiredRevision         string      `json:"desired_revision" gorm:"size:64"`
	PublicationVersion      string      `json:"publication_version" gorm:"size:64"`
	WithdrawVersion         string      `json:"withdraw_version" gorm:"size:64"`
	WithdrawAt              int64       `json:"withdraw_at"`
	WithdrawIntervalSeconds int         `json:"withdraw_interval_seconds"`
	LastReceivedAt          int64       `json:"last_received_at"`
	LastRejectedAt          int64       `json:"last_rejected_at"`
	LastClockErrorAt        int64       `json:"last_clock_error_at"`
	CreatedAt               int64       `json:"created_at"`
	UpdatedAt               int64       `json:"updated_at"`
	GroupEndpoint           string      `json:"-" gorm:"size:64"`
	CollectorEndpoint       string      `json:"-" gorm:"size:64"`
	RequestKey              string      `json:"-" gorm:"size:64;unique"`
	RequestHash             string      `json:"-" gorm:"size:64"`
}

func (MonitorDevice) TableName() string { return "monitor_device" }

type MonitorTemplateVersion struct {
	ID          string      `json:"id" gorm:"primaryKey;size:64"`
	TemplateKey string      `json:"template_key" gorm:"size:64"`
	Version     int         `json:"version"`
	Name        string      `json:"name" gorm:"size:128"`
	Definition  MonitorText `json:"definition"`
	Digest      string      `json:"digest" gorm:"size:64"`
}

func (MonitorTemplateVersion) TableName() string { return "monitor_template_version" }

type MonitorPublication struct {
	ID          string      `json:"id" gorm:"primaryKey;size:36"`
	CollectorID string      `json:"collector_id" gorm:"size:36;index"`
	Version     string      `json:"version" gorm:"size:64;unique"`
	Ciphertext  MonitorText `json:"-"`
	KeyID       string      `json:"-" gorm:"size:64"`
	Digest      string      `json:"digest" gorm:"size:64"`
	Manifest    MonitorText `json:"-"`
	Reason      string      `json:"reason" gorm:"size:40"`
	CreatedBy   int64       `json:"created_by"`
	CreatedAt   int64       `json:"created_at"`
}

func (MonitorPublication) TableName() string { return "monitor_publication" }

type MonitorProbe struct {
	ID             string      `json:"id" gorm:"primaryKey;size:36"`
	GroupID        int64       `json:"group_id" gorm:"index"`
	CollectorID    string      `json:"collector_id" gorm:"size:36;index"`
	CreatedBy      int64       `json:"created_by" gorm:"index"`
	Ciphertext     MonitorText `json:"-"`
	KeyID          string      `json:"-" gorm:"size:64"`
	Revision       string      `json:"revision" gorm:"size:64"`
	Deadline       int64       `json:"deadline"`
	CreatedAt      int64       `json:"created_at"`
	CleanupVersion string      `json:"cleanup_version" gorm:"size:64"`
	CleanupAt      int64       `json:"cleanup_at"`
	LastReceivedAt int64       `json:"last_received_at"`
	State          string      `json:"state" gorm:"size:32"`
	Result         string      `json:"result" gorm:"size:32"`
}

func (MonitorProbe) TableName() string { return "monitor_probe" }

type MonitorAudit struct {
	ID          string      `json:"id" gorm:"primaryKey;size:36"`
	ActorID     int64       `json:"actor_id"`
	GroupID     int64       `json:"group_id" gorm:"index"`
	ObjectID    string      `json:"object_id" gorm:"size:36;index"`
	Action      string      `json:"action" gorm:"size:40"`
	Detail      MonitorText `json:"detail"`
	Publication string      `json:"publication" gorm:"size:64"`
	CreatedAt   int64       `json:"created_at"`
}

func (MonitorAudit) TableName() string { return "monitor_audit" }
func MonitorTables() []interface{} {
	return []interface{}{&MonitorCategory{}, &MonitorCatalog{}, &MonitorCollector{}, &MonitorCollectorGroup{}, &MonitorCredential{}, &MonitorDevice{}, &MonitorTemplateVersion{}, &MonitorPublication{}, &MonitorProbe{}, &MonitorAudit{}}
}
