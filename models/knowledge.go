package models

type KnowledgeItem struct {
	ID               int64   `json:"id" gorm:"primaryKey;autoIncrement"`
	GroupID          int64   `json:"group_id" gorm:"not null;index"`
	Title            string  `json:"title" gorm:"type:varchar(255);not null"`
	Draft            OpsJSON `json:"draft"`
	Revision         int64   `json:"revision" gorm:"not null;default:1"`
	CurrentVersionID int64   `json:"current_version_id" gorm:"not null;default:0"`
	CreatedBy        int64   `json:"created_by"`
	UpdatedBy        int64   `json:"updated_by"`
	UpdatedAt        int64   `json:"updated_at"`
}

func (KnowledgeItem) TableName() string { return "ops_knowledge" }

type KnowledgeVersion struct {
	ID             int64   `json:"id" gorm:"primaryKey;autoIncrement"`
	ItemID         int64   `json:"item_id" gorm:"not null;uniqueIndex:idx_ops_knowledge_sequence,priority:1"`
	GroupID        int64   `json:"group_id" gorm:"not null;index"`
	Sequence       int64   `json:"sequence" gorm:"not null;uniqueIndex:idx_ops_knowledge_sequence,priority:2"`
	Title          string  `json:"title" gorm:"type:varchar(255);not null"`
	Payload        OpsJSON `json:"payload"`
	SHA256         string  `json:"sha256" gorm:"type:varchar(64);not null"`
	PublishedBy    int64   `json:"published_by"`
	PublishedAt    int64   `json:"published_at"`
	WithdrawnAt    int64   `json:"withdrawn_at"`
	WithdrawReason string  `json:"withdraw_reason" gorm:"type:text"`
}

func (KnowledgeVersion) TableName() string { return "ops_knowledge_version" }

type KnowledgeSource struct {
	ID          int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	GroupID     int64  `json:"group_id" gorm:"not null;index"`
	Name        string `json:"name" gorm:"type:varchar(255);not null"`
	MediaType   string `json:"media_type" gorm:"type:varchar(120);not null"`
	StorageKey  string `json:"-" gorm:"type:varchar(64);not null;unique"`
	SHA256      string `json:"sha256" gorm:"type:varchar(64);not null"`
	Size        int64  `json:"size"`
	Description string `json:"description" gorm:"type:text"`
	CreatedBy   int64  `json:"created_by"`
	CreatedAt   int64  `json:"created_at"`
}

func (KnowledgeSource) TableName() string { return "ops_knowledge_source" }

func KnowledgeTables() []interface{} {
	return []interface{}{&KnowledgeItem{}, &KnowledgeVersion{}, &KnowledgeSource{}}
}
