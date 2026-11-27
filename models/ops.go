package models

// OpsJSON uses the existing dialect-aware, unbounded document representation.
type OpsJSON = ArchiveJSON

type OpsCase struct {
	ID                 int64   `json:"id" gorm:"primaryKey;autoIncrement"`
	GroupID            int64   `json:"group_id" gorm:"not null;index:idx_ops_case_group_state,priority:1"`
	Severity           int     `json:"severity"`
	Title              string  `json:"title" gorm:"type:varchar(255);not null"`
	State              string  `json:"state" gorm:"type:varchar(24);not null;index:idx_ops_case_group_state,priority:2"`
	WaitReason         string  `json:"wait_reason" gorm:"type:varchar(64);not null;default:''"`
	OwnerID            int64   `json:"owner_id" gorm:"not null;default:0;index"`
	TargetIdent        string  `json:"target_ident" gorm:"type:varchar(255);not null;default:''"`
	DatasourceID       int64   `json:"datasource_id" gorm:"not null;default:0"`
	RuleID             int64   `json:"rule_id" gorm:"not null;default:0"`
	Context            OpsJSON `json:"context"`
	KnowledgeVersionID int64   `json:"knowledge_version_id" gorm:"not null;default:0"`
	PlanID             int64   `json:"plan_id" gorm:"not null;default:0"`
	Revision           int64   `json:"revision" gorm:"not null;default:1"`
	Generation         int64   `json:"generation" gorm:"not null;default:1"`
	CloseType          string  `json:"close_type" gorm:"type:varchar(40);not null;default:''"`
	Summary            string  `json:"summary" gorm:"type:text"`
	Followup           string  `json:"followup" gorm:"type:text"`
	CreatedBy          int64   `json:"created_by"`
	CreatedAt          int64   `json:"created_at" gorm:"index"`
	UpdatedAt          int64   `json:"updated_at"`
	ClosedAt           int64   `json:"closed_at"`
}

func (OpsCase) TableName() string { return "ops_case" }

type OpsEpisode struct {
	ID         int64   `json:"id" gorm:"primaryKey;autoIncrement"`
	CaseID     int64   `json:"case_id" gorm:"not null;index"`
	GroupID    int64   `json:"group_id" gorm:"not null;index"`
	IncidentID string  `json:"incident_id" gorm:"type:varchar(64);not null;unique"`
	Sequence   int64   `json:"sequence"`
	Recovered  bool    `json:"recovered"`
	Snapshot   OpsJSON `json:"snapshot"`
	UpdatedAt  int64   `json:"updated_at"`
}

func (OpsEpisode) TableName() string { return "ops_episode" }

type OpsInbox struct {
	ID         int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	DispatchID string `json:"dispatch_id" gorm:"type:varchar(64);not null;unique"`
	IncidentID string `json:"incident_id" gorm:"type:varchar(64);not null;index"`
	CaseID     int64  `json:"case_id" gorm:"not null;index"`
	CreatedAt  int64  `json:"created_at"`
}

func (OpsInbox) TableName() string { return "ops_inbox" }

type OpsEvidence struct {
	ID          int64   `json:"id" gorm:"primaryKey;autoIncrement"`
	CaseID      int64   `json:"case_id" gorm:"not null;index"`
	Generation  int64   `json:"generation"`
	Kind        string  `json:"kind" gorm:"type:varchar(40);not null"`
	Source      string  `json:"source" gorm:"type:varchar(255);not null"`
	Status      string  `json:"status" gorm:"type:varchar(24);not null"`
	ObservedAt  int64   `json:"observed_at"`
	WindowStart int64   `json:"window_start"`
	WindowEnd   int64   `json:"window_end"`
	CollectedAt int64   `json:"collected_at"`
	Payload     OpsJSON `json:"payload"`
	SHA256      string  `json:"sha256" gorm:"type:varchar(64);not null"`
	CreatedBy   int64   `json:"created_by"`
}

func (OpsEvidence) TableName() string { return "ops_evidence" }

type OpsStep struct {
	PlanID      int64   `json:"plan_id" gorm:"not null;default:0"`
	ID          int64   `json:"id" gorm:"primaryKey;autoIncrement"`
	CaseID      int64   `json:"case_id" gorm:"not null;index"`
	Generation  int64   `json:"generation"`
	VersionID   int64   `json:"version_id"`
	StepKey     string  `json:"step_key" gorm:"type:varchar(64);not null"`
	State       string  `json:"state" gorm:"type:varchar(24);not null"`
	EvidenceIDs OpsJSON `json:"evidence_ids"`
	Note        string  `json:"note" gorm:"type:text"`
	CreatedBy   int64   `json:"created_by"`
	CreatedAt   int64   `json:"created_at"`
}

func (OpsStep) TableName() string { return "ops_step" }

type OpsAudit struct {
	ID        int64   `json:"id" gorm:"primaryKey;autoIncrement"`
	CaseID    int64   `json:"case_id" gorm:"not null;index"`
	ActorID   int64   `json:"actor_id"`
	Action    string  `json:"action" gorm:"type:varchar(64);not null"`
	Detail    OpsJSON `json:"detail"`
	CreatedAt int64   `json:"created_at"`
}

func (OpsAudit) TableName() string { return "ops_audit" }

type OpsAction struct {
	PlanID       int64   `json:"plan_id" gorm:"not null;default:0"`
	ID           int64   `json:"id" gorm:"primaryKey;autoIncrement"`
	CaseID       int64   `json:"case_id" gorm:"not null;index"`
	Generation   int64   `json:"generation"`
	VersionID    int64   `json:"version_id"`
	Kind         string  `json:"kind" gorm:"type:varchar(32);not null"`
	State        string  `json:"state" gorm:"type:varchar(24);not null;index"`
	OperationKey string  `json:"operation_key" gorm:"type:varchar(64);not null;unique"`
	Baseline     string  `json:"baseline" gorm:"type:varchar(64);not null"`
	Payload      OpsJSON `json:"payload"`
	Result       OpsJSON `json:"result"`
	ProposedBy   int64   `json:"proposed_by"`
	ApprovedBy   int64   `json:"approved_by"`
	ExpiresAt    int64   `json:"expires_at"`
	CreatedAt    int64   `json:"created_at"`
	UpdatedAt    int64   `json:"updated_at"`
}

func (OpsAction) TableName() string { return "ops_action" }

type OpsVerification struct {
	PlanID      int64   `json:"plan_id" gorm:"not null;default:0"`
	ID          int64   `json:"id" gorm:"primaryKey;autoIncrement"`
	CaseID      int64   `json:"case_id" gorm:"not null;index"`
	Generation  int64   `json:"generation"`
	VersionID   int64   `json:"version_id"`
	CheckKey    string  `json:"check_key" gorm:"type:varchar(64);not null"`
	State       string  `json:"state" gorm:"type:varchar(24);not null"`
	Reason      string  `json:"reason" gorm:"type:text"`
	EvidenceIDs OpsJSON `json:"evidence_ids"`
	ConfirmedBy int64   `json:"confirmed_by"`
	CreatedAt   int64   `json:"created_at"`
}

func (OpsVerification) TableName() string { return "ops_verification" }

type OpsRun struct {
	ID           int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	CaseID       int64  `json:"case_id" gorm:"not null;index"`
	ActiveCaseID *int64 `json:"-" gorm:"unique"`
	UserID       int64  `json:"user_id"`
	State        string `json:"state" gorm:"type:varchar(24);not null;index"`
	Fence        int64  `json:"fence" gorm:"not null;default:0"`
	LeaseUntil   int64  `json:"lease_until"`
	NextRunAt    int64  `json:"next_run_at"`
	Attempts     int    `json:"attempts"`
	Mode         string `json:"mode" gorm:"type:varchar(24);not null"`
	Result       string `json:"result" gorm:"type:text"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}

func (OpsRun) TableName() string { return "ops_run" }

type OpsPolicy struct {
	ID              int64   `json:"id" gorm:"primaryKey;autoIncrement"`
	GroupID         int64   `json:"group_id" gorm:"not null;unique"`
	Enabled         bool    `json:"enabled"`
	ServiceUserID   int64   `json:"service_user_id"`
	AutoInvestigate bool    `json:"auto_investigate"`
	RuleIDs         OpsJSON `json:"rule_ids"`
	Revision        int64   `json:"revision" gorm:"not null;default:1"`
	ScanAt          int64   `json:"scan_at"`
	ScanID          int64   `json:"scan_id"`
	ReconcileAt     int64   `json:"reconcile_at"`
	Health          string  `json:"health" gorm:"type:varchar(255)"`
	GapSince        int64   `json:"gap_since"`
}

func (OpsPolicy) TableName() string { return "ops_policy" }

// OpsPlan is an immutable, case-local proposal. Confirmation changes only its
// decision metadata; revisions append a new row and never rewrite past content.
type OpsPlan struct {
	ID                  int64   `json:"id" gorm:"primaryKey;autoIncrement"`
	CaseID              int64   `json:"case_id" gorm:"not null;uniqueIndex:idx_ops_plan_case_sequence,priority:1"`
	Sequence            int64   `json:"sequence" gorm:"not null;uniqueIndex:idx_ops_plan_case_sequence,priority:2"`
	Title               string  `json:"title" gorm:"type:varchar(255);not null"`
	Scope               string  `json:"scope" gorm:"type:text"`
	Rationale           string  `json:"rationale" gorm:"type:text"`
	Payload             OpsJSON `json:"payload"`
	EvidenceIDs         OpsJSON `json:"evidence_ids"`
	SHA256              string  `json:"sha256" gorm:"type:varchar(64);not null"`
	Generation          int64   `json:"generation"`
	EvidenceAnchor      int64   `json:"evidence_anchor"`
	ConfirmedGeneration int64   `json:"confirmed_generation"`
	State               string  `json:"state" gorm:"type:varchar(24);not null"`
	CreatedBy           int64   `json:"created_by"`
	CreatedAt           int64   `json:"created_at"`
	ConfirmedBy         int64   `json:"confirmed_by"`
	ConfirmedAt         int64   `json:"confirmed_at"`
	DecisionNote        string  `json:"decision_note" gorm:"type:text"`
}

func (OpsPlan) TableName() string { return "ops_plan" }

func OpsTables() []interface{} {
	return []interface{}{&OpsCase{}, &OpsEpisode{}, &OpsInbox{}, &OpsEvidence{}, &OpsStep{}, &OpsAudit{}, &OpsAction{}, &OpsVerification{}, &OpsRun{}, &OpsPolicy{}, &OpsPlan{}}
}
