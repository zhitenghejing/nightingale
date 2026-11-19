package models

import (
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

type LibraryText string

func (LibraryText) GormDataType() string { return "text" }
func (LibraryText) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db.Dialector.Name() == "mysql" {
		return "LONGTEXT"
	}
	return "TEXT"
}

// LibraryVersion indexes an immutable, formally saved archive version. Private
// editor drafts never enter this table. Markdown is available before AI summary.
type LibraryVersion struct {
	ID              int64       `json:"id" gorm:"primaryKey;autoIncrement"`
	SourceVersionID int64       `json:"source_version_id" gorm:"not null;unique"`
	DocumentID      int64       `json:"document_id" gorm:"not null;index"`
	ArchiveID       int64       `json:"archive_id" gorm:"not null;index"`
	Title           string      `json:"title" gorm:"type:varchar(255)"`
	Sequence        int64       `json:"sequence"`
	State           string      `json:"state" gorm:"type:varchar(32);index"`
	Markdown        LibraryText `json:"markdown"`
	Summary         LibraryText `json:"summary"`
	SummaryState    string      `json:"summary_state" gorm:"type:varchar(32)"`
	Digest          string      `json:"digest" gorm:"type:varchar(64)"`
	Error           string      `json:"error" gorm:"type:varchar(500)"`
	Attempts        int         `json:"attempts"`
	Lease           string      `json:"-" gorm:"type:varchar(64)"`
	LeaseUntil      int64       `json:"-"`
	NextRun         int64       `json:"-"`
	UpdatedAt       int64       `json:"updated_at"`
}

func (LibraryVersion) TableName() string { return "knowledge_document_version" }

type LibrarySection struct {
	ID        int64       `json:"id" gorm:"primaryKey;autoIncrement"`
	VersionID int64       `json:"version_id" gorm:"not null;index"`
	Position  int         `json:"position"`
	Heading   string      `json:"heading" gorm:"type:varchar(255)"`
	Body      LibraryText `json:"body"`
}

func (LibrarySection) TableName() string { return "knowledge_document_section" }

type LibraryWorkbook struct {
	VersionID int64       `json:"version_id" gorm:"primaryKey;autoIncrement:false"`
	Data      LibraryText `json:"-"`
}

func (LibraryWorkbook) TableName() string { return "knowledge_document_workbook" }
func LibraryTables() []interface{} {
	return []interface{}{&LibraryVersion{}, &LibrarySection{}, &LibraryWorkbook{}}
}
