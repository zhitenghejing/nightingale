package models

// These center-only records contain no model-visible or exported secrets.
type OpsCredential struct {
	ID            string `json:"id" gorm:"primaryKey;size:36"`
	Name          string `json:"name" gorm:"size:128;not null"`
	GroupID       int64  `json:"group_id" gorm:"not null;index"`
	Disabled      bool   `json:"disabled" gorm:"not null;default:false"`
	DeviceOnly    bool   `json:"-" gorm:"not null;default:false"`
	Ciphertext    string `json:"-" gorm:"type:text;not null"`
	KeyID         string `json:"-" gorm:"size:64;not null"`
	RecordVersion int64  `json:"record_version" gorm:"not null"`
	UpdatedBy     int64  `json:"updated_by"`
	UpdatedAt     int64  `json:"updated_at"`
}

func (OpsCredential) TableName() string { return "ops_credential" }

type OpsSSHConnection struct {
	TargetIdent      string `json:"ident" gorm:"primaryKey;size:191"`
	TargetID         int64  `json:"-" gorm:"not null"`
	GroupID          int64  `json:"group_id" gorm:"not null;index"`
	Enabled          bool   `json:"enabled" gorm:"not null"`
	Protocol         string `json:"protocol" gorm:"size:8;not null;default:ssh"`
	Host             string `json:"host" gorm:"size:64;not null"`
	Port             int    `json:"port" gorm:"not null"`
	Fingerprint      string `json:"fingerprint" gorm:"size:128;not null"`
	CredentialID     string `json:"credential_id" gorm:"size:36;not null;index"`
	JumpHost         string `json:"jump_host" gorm:"size:64"`
	JumpPort         int    `json:"jump_port"`
	JumpFingerprint  string `json:"jump_fingerprint" gorm:"size:128"`
	JumpCredentialID string `json:"jump_credential_id" gorm:"size:36;index"`
	LogPathsJSON     string `json:"-" gorm:"type:text"`
	RecordVersion    int64  `json:"record_version" gorm:"not null"`
	UpdatedBy        int64  `json:"updated_by"`
	UpdatedAt        int64  `json:"updated_at"`
}

func (OpsSSHConnection) TableName() string { return "ops_ssh_connection" }

type OpsAccessAudit struct {
	ID        string `json:"id" gorm:"primaryKey;size:36"`
	ActorID   int64  `json:"actor_id"`
	GroupID   int64  `json:"group_id" gorm:"index"`
	ObjectID  string `json:"object_id" gorm:"size:191"`
	Action    string `json:"action" gorm:"size:48"`
	CreatedAt int64  `json:"created_at"`
}

func (OpsAccessAudit) TableName() string { return "ops_access_audit" }
func OpsAccessTables() []interface{} {
	return []interface{}{&OpsCredential{}, &OpsSSHConnection{}, &OpsAccessAudit{}}
}
