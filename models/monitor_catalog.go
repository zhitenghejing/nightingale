package models

// Catalog metadata is separate from builtin_components: changing a display name
// or logo must not change the component identity or its localized source files.
type MonitorCategory struct {
	ID            string `json:"id" gorm:"primaryKey;size:36"`
	ParentID      string `json:"parent_id" gorm:"size:36;index"`
	Name          string `json:"name" gorm:"size:128"`
	Sort          int    `json:"sort"`
	Disabled      bool   `json:"disabled"`
	RecordVersion int64  `json:"record_version"`
}

func (MonitorCategory) TableName() string { return "monitor_category" }

type MonitorCatalog struct {
	ComponentID        uint64      `json:"component_id" gorm:"primaryKey;autoIncrement:false"`
	Name               string      `json:"name" gorm:"size:128"`
	Logo               MonitorText `json:"-"`
	CategoryID         string      `json:"category_id" gorm:"size:36;index"`
	Deleted            bool        `json:"deleted"`
	Disabled           bool        `json:"disabled"`
	Draft              MonitorText `json:"-"`
	PublishedVersionID string      `json:"published_version_id" gorm:"size:64"`
	RecordVersion      int64       `json:"record_version"`
}

func (MonitorCatalog) TableName() string { return "monitor_catalog" }
