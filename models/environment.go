package models

// Environment records reference native sources; native names, credentials and
// metrics remain owned by their original modules.
type EnvironmentObject struct {
	ID          int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	GroupID     int64  `json:"group_id" gorm:"index"`
	Name        string `json:"name" gorm:"size:160"`
	Kind        string `json:"kind" gorm:"size:32"`
	Description string `json:"description" gorm:"type:text"`
	Revision    int64  `json:"revision"`
	Deleted     bool   `json:"deleted" gorm:"index"`
	UpdatedAt   int64  `json:"updated_at"`
}

func (EnvironmentObject) TableName() string { return "environment_object" }

type EnvironmentRelation struct {
	ID          int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	Identity    string `json:"-" gorm:"size:64;unique"`
	FromRef     string `json:"from_ref" gorm:"size:100;index"`
	ToRef       string `json:"to_ref" gorm:"size:100;index"`
	Kind        string `json:"kind" gorm:"size:32"`
	Status      string `json:"status" gorm:"size:24;index"`
	FromVersion string `json:"-" gorm:"size:64"`
	ToVersion   string `json:"-" gorm:"size:64"`
	Source      string `json:"source" gorm:"size:32"`
	Note        string `json:"note" gorm:"type:text"`
	ValidUntil  int64  `json:"valid_until"`
	CreatedBy   int64  `json:"created_by"`
	UpdatedBy   int64  `json:"updated_by"`
	UpdatedAt   int64  `json:"updated_at"`
	Revision    int64  `json:"revision"`
}

func (EnvironmentRelation) TableName() string { return "environment_relation" }

type EnvironmentChange struct {
	ID         int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	RelationID int64  `json:"relation_id" gorm:"index"`
	ObjectRef  string `json:"object_ref" gorm:"size:100;index"`
	ActorID    int64  `json:"actor_id"`
	Action     string `json:"action" gorm:"size:32"`
	Detail     string `json:"detail" gorm:"type:text"`
	CreatedAt  int64  `json:"created_at" gorm:"index"`
}

func (EnvironmentChange) TableName() string { return "environment_change" }
func EnvironmentTables() []interface{} {
	return []interface{}{&EnvironmentObject{}, &EnvironmentRelation{}, &EnvironmentChange{}, &EnvironmentDiscovery{}}
}

// EnvironmentDiscovery is a durable retry schedule, not a second knowledge index.
type EnvironmentDiscovery struct {
	VersionID  int64  `json:"version_id" gorm:"primaryKey;autoIncrement:false"`
	ActorID    int64  `json:"actor_id"`
	State      string `json:"state" gorm:"size:24"`
	Note       string `json:"note" gorm:"type:text"`
	NextRun    int64  `json:"next_run" gorm:"index"`
	LeaseUntil int64  `json:"-"`
	UpdatedAt  int64  `json:"updated_at"`
}

func (EnvironmentDiscovery) TableName() string { return "environment_discovery" }
