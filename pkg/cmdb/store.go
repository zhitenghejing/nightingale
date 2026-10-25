// Package cmdb owns inventory and report persistence within the center deployment.
package cmdb

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

// largeText preserves large asset/audit documents on every supported dialect.
type largeText string

func (largeText) GormDataType() string { return "string" }
func (largeText) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db.Dialector.Name() == "mysql" {
		return "LONGTEXT"
	}
	return "TEXT"
}

// Document retains the complete ledger, including hardware and warranty fields.
type Document map[string]interface{}
type Room struct {
	ID   string `json:"id" gorm:"primaryKey;size:36"`
	Name string `json:"name" gorm:"size:160;uniqueIndex"`
}

func (Room) TableName() string { return "cmdb_room" }

type Rack struct {
	ID      string `json:"id" gorm:"primaryKey;size:36"`
	RoomID  string `json:"roomId" gorm:"size:36;uniqueIndex:rack_name"`
	Name    string `json:"name" gorm:"size:160;uniqueIndex:rack_name"`
	Units   int    `json:"units"`
	Version int    `json:"version"`
}

func (Rack) TableName() string { return "cmdb_rack" }

type Asset struct {
	ID        string `gorm:"primaryKey;size:36"`
	Serial    string `gorm:"size:240;uniqueIndex"`
	RackID    string `gorm:"size:36;index"`
	Start     int
	End       int
	Version   int
	Deleted   bool `gorm:"index"`
	Payload   largeText
	UpdatedAt time.Time
}

func (Asset) TableName() string { return "cmdb_device" }

type Disk struct {
	ID          uint64 `gorm:"primaryKey"`
	AssetID     string `gorm:"size:36;index"`
	Position    int
	Model       string `gorm:"size:240"`
	Description string `gorm:"type:text"`
	Count       int
}

func (Disk) TableName() string { return "cmdb_device_disk" }

type Change struct {
	ID        uint64    `json:"id" gorm:"primaryKey"`
	AssetID   string    `json:"assetId" gorm:"size:36;index"`
	UserID    int64     `json:"userId"`
	Action    string    `json:"action" gorm:"size:40"`
	Before    largeText `json:"before"`
	After     largeText `json:"after"`
	CreatedAt time.Time `json:"createdAt"`
}

func (Change) TableName() string { return "cmdb_change" }

type Access struct {
	UserID int64 `json:"userId" gorm:"primaryKey;autoIncrement:false"`
	Read   bool  `json:"read"`
	Create bool  `json:"create"`
	Edit   bool  `json:"edit"`
	Delete bool  `json:"delete"`
	Import bool  `json:"import"`
	Export bool  `json:"export"`
}

func (Access) TableName() string { return "cmdb_access" }

type writeLock struct {
	ID       int `gorm:"primaryKey;autoIncrement:false"`
	Revision int
}

func (writeLock) TableName() string { return "cmdb_write_lock" }

type Store struct{ DB *gorm.DB }

func (s *Store) Migrate() error {
	// Initial provisioning creates missing CMDB tables only. Never let AutoMigrate
	// infer ALTER/DROP operations on existing tables; future upgrades need reviewed SQL.
	for _, model := range []interface{}{&Room{}, &Rack{}, &Asset{}, &Disk{}, &Change{}, &Access{}, &writeLock{}} {
		migrator := s.DB.Migrator()
		if !migrator.HasTable(model) {
			if err := migrator.CreateTable(model); err != nil {
				return err
			}
		}
		statement := &gorm.Statement{DB: s.DB}
		if err := statement.Parse(model); err != nil {
			return err
		}
		for _, column := range statement.Schema.DBNames {
			if !migrator.HasColumn(model, column) {
				return fmt.Errorf("CMDB table %s requires an explicit schema upgrade", statement.Schema.Table)
			}
		}
	}
	for _, index := range []struct {
		model interface{}
		name  string
	}{{&Room{}, "idx_cmdb_room_name"}, {&Rack{}, "rack_name"}, {&Asset{}, "idx_cmdb_device_serial"}} {
		if !s.DB.Migrator().HasIndex(index.model, index.name) {
			return fmt.Errorf("required CMDB index %s is missing", index.name)
		}
	}

	return s.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&writeLock{ID: 1}).Error
}
func (s *Store) Write(fn func(*gorm.DB) error) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		// Updating an existing singleton serializes placement checks even when the rack is empty.
		lock := tx.Model(&writeLock{}).Where("id = 1").UpdateColumn("revision", gorm.Expr("revision + 1"))
		if lock.Error != nil {
			return lock.Error
		}
		if lock.RowsAffected != 1 {
			return errors.New("CMDB write lock is missing; run the schema migration")
		}
		return fn(tx)
	})
}
func str(d Document, k string) string { v, _ := d[k].(string); return strings.TrimSpace(v) }
func num(d Document, k string) int {
	switch v := d[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}
func encode(v interface{}) string { b, _ := json.Marshal(v); return string(b) }
func decode(a Asset) Document {
	d := Document{}
	_ = json.Unmarshal([]byte(a.Payload), &d)
	d["id"] = a.ID
	d["version"] = a.Version
	d["deleted"] = a.Deleted
	return d
}
func audit(tx *gorm.DB, user int64, id, action, before, after string) error {
	return tx.Create(&Change{AssetID: id, UserID: user, Action: action, Before: largeText(before), After: largeText(after)}).Error
}
func validate(d Document) error {
	for _, k := range []string{"customer", "serial"} {
		if str(d, k) == "" {
			return fmt.Errorf("客户信息和序列号必填")
		}
	}
	for k, v := range d {
		if text, ok := v.(string); ok && len(text) > 10000 {
			return fmt.Errorf("字段 %s 过长", k)
		}
	}
	if len(str(d, "serial")) > 240 {
		return errors.New("序列号过长")
	}
	for _, k := range []string{"uStart", "uEnd", "cpuCount", "powerCount"} {
		if d[k] != nil {
			v, ok := d[k].(float64)
			if !ok || v != float64(int(v)) || v < 0 {
				return fmt.Errorf("%s 必须为非负整数", k)
			}
		}
	}
	for _, k := range []string{"warrantyStart", "warrantyEnd", "expire"} {
		if v := str(d, k); v != "" {
			if _, err := time.Parse("2006-01-02", v); err != nil {
				return errors.New("日期格式必须为 YYYY-MM-DD")
			}
		}
	}
	if str(d, "warrantyStart") != "" && str(d, "warrantyEnd") != "" && str(d, "warrantyStart") > str(d, "warrantyEnd") {
		return errors.New("保修开始不能晚于结束")
	}
	monitored, _ := d["monitored"].(bool)
	alarm, _ := d["alertEnabled"].(bool)
	if alarm && !monitored {
		return errors.New("开启报警前请先启用监控标记")
	}
	if monitored {
		d["monitorState"] = "未关联监控"
	} else {
		d["monitorState"] = "未接入"
	}
	if disks, ok := d["disks"].([]interface{}); ok {
		if len(disks) > 100 {
			return errors.New("硬盘组数量不能超过 100")
		}
		for _, raw := range disks {
			x, ok := raw.(map[string]interface{})
			if !ok {
				return errors.New("硬盘格式错误")
			}
			c, ok := x["count"].(float64)
			if !ok || c < 1 || c > 10000 || c != float64(int(c)) {
				return errors.New("硬盘数量必须为正整数")
			}
		}
	} else if d["disks"] != nil {
		return errors.New("硬盘格式错误")
	}
	return nil
}
func (s *Store) Save(tx *gorm.DB, d Document, user int64, create bool) (Document, error) {
	if err := validate(d); err != nil {
		return nil, err
	}
	id := str(d, "id")
	var old Asset
	before := ""
	version := 1
	if create {
		id = uuid.NewString()
	} else {
		if err := tx.First(&old, "id = ? AND deleted = ?", id, false).Error; err != nil {
			return nil, errors.New("设备不存在或已删除")
		}
		if num(d, "version") != old.Version {
			return nil, errors.New("数据已被其他人修改，请刷新后重试")
		}
		before = encode(decode(old))
		version = old.Version + 1
	}
	serial := strings.ToLower(str(d, "serial"))
	var duplicates int64
	if err := tx.Model(&Asset{}).Where("serial = ? AND id <> ?", serial, id).Count(&duplicates).Error; err != nil {
		return nil, err
	}
	if duplicates > 0 {
		return nil, errors.New("序列号已存在（包括回收站设备）")
	}
	start, end := num(d, "uStart"), num(d, "uEnd")
	rackID := str(d, "rackId")
	if str(d, "rack") != "" || rackID != "" {
		var r Rack
		var err error
		if rackID != "" {
			err = tx.First(&r, "id = ?", rackID).Error
		} else {
			var room Room
			err = tx.First(&room, "name = ?", str(d, "room")).Error
			if err == nil {
				err = tx.First(&r, "room_id = ? AND name = ?", room.ID, str(d, "rack")).Error
			}
		}
		if err != nil {
			return nil, errors.New("机柜不存在，请先在机柜图中创建机房和机柜")
		}
		var room Room
		if err = tx.First(&room, "id = ?", r.RoomID).Error; err != nil {
			return nil, err
		}
		rackID = r.ID
		d["room"] = room.Name
		d["rack"] = r.Name
		if start != 0 || end != 0 {
			if start < 1 || end < start || end > r.Units {
				return nil, errors.New("U 位超出机柜容量或起止顺序错误")
			}
			var n int64
			if err = tx.Model(&Asset{}).Where("rack_id = ? AND deleted = ? AND id <> ?", rackID, false, id).
				Where(clause.Lte{Column: "start", Value: end}).Where(clause.Gte{Column: "end", Value: start}).Count(&n).Error; err != nil {
				return nil, err
			}
			if n > 0 {
				return nil, errors.New("机柜 U 位已被其他设备占用")
			}
		}
	} else if start != 0 || end != 0 {
		return nil, errors.New("配置 U 位时必须指定机柜")
	}
	if (start == 0) != (end == 0) {
		return nil, errors.New("起止 U 位须同时填写")
	}
	d["id"] = id
	d["rackId"] = rackID
	d["version"] = version
	d["serial"] = str(d, "serial")
	d["deleted"] = false
	a := Asset{ID: id, Serial: serial, RackID: rackID, Start: start, End: end, Version: version, Payload: largeText(encode(d))}
	if err := tx.Save(&a).Error; err != nil {
		return nil, err
	}
	if err := tx.Where("asset_id = ?", id).Delete(&Disk{}).Error; err != nil {
		return nil, err
	}
	if disks, ok := d["disks"].([]interface{}); ok {
		for i, raw := range disks {
			x := Document(raw.(map[string]interface{}))
			if err := tx.Create(&Disk{AssetID: id, Position: i, Model: str(x, "model"), Description: str(x, "description"), Count: num(x, "count")}).Error; err != nil {
				return nil, err
			}
		}
	}
	action := "edit"
	if create {
		action = "create"
	}
	return d, audit(tx, user, id, action, before, encode(d))
}
func (s *Store) Assets(deleted bool) ([]Document, error) {
	var rows []Asset
	err := s.DB.Where("deleted = ?", deleted).Order("updated_at DESC").Find(&rows).Error
	out := make([]Document, 0, len(rows))
	for _, a := range rows {
		d := decode(a)
		if a.RackID != "" {
			var r Rack
			var room Room
			if s.DB.First(&r, "id = ?", a.RackID).Error == nil && s.DB.First(&room, "id = ?", r.RoomID).Error == nil {
				d["room"] = room.Name
				d["rack"] = r.Name
			}
		}
		out = append(out, d)
	}
	return out, err
}
