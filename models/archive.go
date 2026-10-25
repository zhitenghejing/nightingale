package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// ArchiveJSON keeps document payloads unbounded on PostgreSQL/SQLite and uses
// LONGTEXT on MySQL; ordinary TEXT there cannot hold the allowed 5 MiB body.
type ArchiveJSON string

func (ArchiveJSON) GormDataType() string { return "text" }
func (ArchiveJSON) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db.Dialector.Name() == "mysql" {
		return "LONGTEXT"
	}
	return "TEXT"
}
func (v ArchiveJSON) Value() (driver.Value, error) { return string(v), nil }
func (v *ArchiveJSON) Scan(src interface{}) error {
	switch x := src.(type) {
	case nil:
		*v = ""
	case string:
		*v = ArchiveJSON(x)
	case []byte:
		*v = ArchiveJSON(x)
	default:
		return fmt.Errorf("archive JSON: unexpected database value %T", src)
	}
	return nil
}
func (v ArchiveJSON) MarshalJSON() ([]byte, error) {
	if v == "" {
		return []byte("null"), nil
	}
	if !json.Valid([]byte(v)) {
		return nil, fmt.Errorf("invalid archive JSON")
	}
	return []byte(v), nil
}
func (v *ArchiveJSON) UnmarshalJSON(b []byte) error {
	if !json.Valid(b) {
		return fmt.Errorf("invalid archive JSON")
	}
	*v = ArchiveJSON(string(b))
	return nil
}

type ArchiveRecord struct {
	ID                int64       `json:"id" gorm:"primaryKey;autoIncrement"`
	Category          string      `json:"category" gorm:"type:varchar(24);not null;index:idx_archive_category_updated,priority:1"`
	Title             string      `json:"title" gorm:"type:varchar(255);not null"`
	State             string      `json:"state" gorm:"type:varchar(16);not null;index"`
	Metadata          ArchiveJSON `json:"metadata"`
	Department        string      `json:"department" gorm:"type:varchar(255)"`
	Location          string      `json:"location" gorm:"type:varchar(255)"`
	Person            string      `json:"person" gorm:"type:varchar(255)"`
	Result            string      `json:"result" gorm:"type:varchar(24)"`
	Level             string      `json:"level" gorm:"type:varchar(8)"`
	StartAt           string      `json:"start_at" gorm:"type:varchar(40)"`
	PrimaryDocumentID int64       `json:"primary_document_id"`
	Revision          int64       `json:"revision" gorm:"not null;default:1"`
	Deleted           bool        `json:"deleted" gorm:"not null;default:false;index"`
	CreateAt          int64       `json:"create_at"`
	CreateBy          int64       `json:"create_by"`
	CreateName        string      `json:"create_name" gorm:"type:varchar(255)"`
	UpdateAt          int64       `json:"update_at" gorm:"index:idx_archive_category_updated,priority:2"`
	UpdateBy          int64       `json:"update_by"`
	UpdateName        string      `json:"update_name" gorm:"type:varchar(255)"`
}

func (ArchiveRecord) TableName() string { return "archive_record" }

type ArchiveDocument struct {
	ID               int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	ArchiveID        int64  `json:"archive_id" gorm:"not null;index"`
	Title            string `json:"title" gorm:"type:varchar(255);not null"`
	Purpose          string `json:"purpose" gorm:"type:varchar(24);not null"`
	Kind             string `json:"kind" gorm:"type:varchar(16);not null"`
	SourceDocumentID int64  `json:"source_document_id"`
	SourceVersionID  int64  `json:"source_version_id"`
	CurrentVersionID int64  `json:"current_version_id"`
	Deleted          bool   `json:"deleted" gorm:"not null;default:false"`
	CreateAt         int64  `json:"create_at"`
	CreateBy         int64  `json:"create_by"`
	UpdateAt         int64  `json:"update_at"`
}

func (ArchiveDocument) TableName() string { return "archive_document" }

type ArchiveDocumentVersion struct {
	ID          int64       `json:"id" gorm:"primaryKey;autoIncrement"`
	DocumentID  int64       `json:"document_id" gorm:"not null;uniqueIndex:idx_archive_document_seq,priority:1"`
	Sequence    int64       `json:"sequence" gorm:"not null;uniqueIndex:idx_archive_document_seq,priority:2"`
	BlobID      int64       `json:"blob_id"`
	Body        ArchiveJSON `json:"body"`
	AssetIDs    ArchiveJSON `json:"asset_ids"`
	HasContent  bool        `json:"has_content" gorm:"not null;default:false"`
	Size        int64       `json:"size"`
	Description string      `json:"description" gorm:"type:varchar(1000)"`
	CreateAt    int64       `json:"create_at"`
	CreateBy    int64       `json:"create_by"`
	CreateName  string      `json:"create_name" gorm:"type:varchar(255)"`
}

func (ArchiveDocumentVersion) TableName() string { return "archive_document_version" }

type ArchiveVersion struct {
	ID         int64       `json:"id" gorm:"primaryKey;autoIncrement"`
	ArchiveID  int64       `json:"archive_id" gorm:"not null;uniqueIndex:idx_archive_version_seq,priority:1"`
	Sequence   int64       `json:"sequence" gorm:"not null;uniqueIndex:idx_archive_version_seq,priority:2"`
	Snapshot   ArchiveJSON `json:"snapshot"`
	Action     string      `json:"action" gorm:"type:varchar(32);not null"`
	CreateAt   int64       `json:"create_at"`
	CreateBy   int64       `json:"create_by"`
	CreateName string      `json:"create_name" gorm:"type:varchar(255)"`
}

func (ArchiveVersion) TableName() string { return "archive_version" }

type ArchiveBlob struct {
	ID          int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	ArchiveID   int64  `json:"archive_id" gorm:"not null;index"`
	DocumentID  int64  `json:"document_id" gorm:"index"`
	StorageKey  string `json:"-" gorm:"type:varchar(64);unique;not null"`
	Name        string `json:"name" gorm:"type:varchar(255);not null"`
	ContentType string `json:"content_type" gorm:"type:varchar(100);not null"`
	Kind        string `json:"kind" gorm:"type:varchar(16);not null"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256" gorm:"type:varchar(64);not null"`
	CreateAt    int64  `json:"create_at"`
	CreateBy    int64  `json:"create_by"`
	Asset       bool   `json:"asset" gorm:"not null;default:false"`
	Staged      bool   `json:"-" gorm:"not null;default:false;index"`
}

func (ArchiveBlob) TableName() string { return "archive_blob" }

type ArchiveEditLease struct {
	DocumentID    int64  `json:"document_id" gorm:"primaryKey;autoIncrement:false"`
	SessionID     string `json:"session_id" gorm:"type:varchar(64);not null"`
	Epoch         int64  `json:"epoch" gorm:"not null;default:0"`
	OwnerID       int64  `json:"owner_id"`
	OwnerName     string `json:"owner_name" gorm:"type:varchar(255)"`
	ExpiresAt     int64  `json:"expires_at"`
	BaseVersionID int64  `json:"base_version_id"`
}

func (ArchiveEditLease) TableName() string { return "archive_edit_lease" }

type ArchiveDraft struct {
	ID            int64       `json:"id" gorm:"primaryKey;autoIncrement"`
	DocumentID    int64       `json:"document_id" gorm:"not null;uniqueIndex:idx_archive_draft_owner,priority:1"`
	OwnerID       int64       `json:"owner_id" gorm:"not null;uniqueIndex:idx_archive_draft_owner,priority:2"`
	BaseVersionID int64       `json:"base_version_id"`
	Body          ArchiveJSON `json:"body"`
	Mode          string      `json:"mode" gorm:"type:varchar(16)"`
	Markdown      ArchiveJSON `json:"markdown"`
	UpdateAt      int64       `json:"update_at"`
}

func (ArchiveDraft) TableName() string { return "archive_draft" }

type ArchiveLog struct {
	ID            int64       `json:"id" gorm:"primaryKey;autoIncrement"`
	ArchiveID     int64       `json:"archive_id" gorm:"not null;index:idx_archive_log_created,priority:1"`
	DocumentID    int64       `json:"document_id"`
	Action        string      `json:"action" gorm:"type:varchar(32);not null"`
	Detail        ArchiveJSON `json:"detail"`
	ActorID       int64       `json:"actor_id"`
	ActorUsername string      `json:"actor_username" gorm:"type:varchar(64)"`
	ActorName     string      `json:"actor_name" gorm:"type:varchar(255)"`
	CreateAt      int64       `json:"create_at" gorm:"index:idx_archive_log_created,priority:2"`
}

func (ArchiveLog) TableName() string { return "archive_log" }

type ArchiveBookmark struct {
	ID         int64       `json:"id" gorm:"primaryKey;autoIncrement"`
	DocumentID int64       `json:"document_id" gorm:"not null;uniqueIndex:idx_archive_bookmark_owner,priority:1"`
	OwnerID    int64       `json:"owner_id" gorm:"not null;uniqueIndex:idx_archive_bookmark_owner,priority:2"`
	VersionID  int64       `json:"version_id"`
	Position   ArchiveJSON `json:"position"`
	UpdateAt   int64       `json:"update_at"`
}

func (ArchiveBookmark) TableName() string { return "archive_bookmark" }

type ArchiveExport struct {
	ID                string `json:"id" gorm:"type:varchar(64);primaryKey"`
	ArchiveID         int64  `json:"archive_id" gorm:"not null;index"`
	ArchiveVersionID  int64  `json:"archive_version_id"`
	DocumentID        int64  `json:"document_id"`
	DocumentVersionID int64  `json:"document_version_id"`
	Format            string `json:"format" gorm:"type:varchar(16)"`
	State             string `json:"state" gorm:"type:varchar(16);not null;index"`
	Error             string `json:"error" gorm:"type:text"`
	StorageKey        string `json:"-" gorm:"type:varchar(100)"`
	FileName          string `json:"file_name" gorm:"type:varchar(255)"`
	CreateAt          int64  `json:"create_at"`
	CreateBy          int64  `json:"create_by"`
	UpdateAt          int64  `json:"update_at"`
}

func (ArchiveExport) TableName() string { return "archive_export" }

func ArchiveTables() []interface{} {
	return []interface{}{&ArchiveRecord{}, &ArchiveDocument{}, &ArchiveDocumentVersion{}, &ArchiveVersion{}, &ArchiveBlob{}, &ArchiveEditLease{}, &ArchiveDraft{}, &ArchiveLog{}, &ArchiveBookmark{}, &ArchiveExport{}}
}
