package models

// MonitorIcon stores reusable, immutable template artwork independently of releases.
type MonitorIcon struct {
	ID        string      `json:"id" gorm:"primaryKey;size:64"`
	Name      string      `json:"name" gorm:"size:128"`
	MIME      string      `json:"mime" gorm:"size:32"`
	Data      MonitorText `json:"-"`
	Size      int         `json:"size"`
	CreatedAt int64       `json:"created_at"`
}

func (MonitorIcon) TableName() string { return "monitor_icon" }
