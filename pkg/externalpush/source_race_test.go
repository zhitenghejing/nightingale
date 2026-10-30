package externalpush

import (
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/cmdb"
	"gorm.io/gorm"
)

func TestConcurrentFirstRegistrationReusesWinningSourceSnapshot(t *testing.T) {
	db := testDB(t)
	store := &cmdb.Store{DB: db}
	if err := store.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.AlertRule{}); err != nil {
		t.Fatal(err)
	}
	var asset cmdb.Document
	if err := store.Write(func(tx *gorm.DB) error {
		var err error
		asset, err = store.Save(tx, cmdb.Document{"customer": "TEST", "serial": "TEST", "hostname": "TEST_NODE", "type": "TEST_OLD"}, 1, true)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	rule := models.AlertRule{AppendTags: "phase=TEST_OLD"}
	if err := db.Create(&rule).Error; err != nil {
		t.Fatal(err)
	}
	s := simulation("http://127.0.0.1:19999/unused")
	s.Event.RuleId, s.Event.TagsJSON = rule.Id, []string{"ident=TEST_NODE"}
	s.Params = map[string]string{"company_id": "TEST", "device_source": "cmdb", "cmdb_match_field": "hostname", "cmdb_match_label": "ident", "device_tags_source": "rule_append_tags", "level_map": `{"1":"严重"}`, "repeat_policy": "all_dispatches"}
	var winner int64
	injected := false
	// At the database read boundary, interleave a second registration after the
	// first has read old asset rows, but before its task INSERT. No send occurs.
	name := "test:interleave_first_source_registration"
	if err := db.Callback().Query().After("gorm:query").Register(name, func(query *gorm.DB) {
		if injected || query.Statement.Schema == nil || query.Statement.Schema.Table != "cmdb_device" {
			return
		}
		injected = true
		asset["type"] = "TEST_NEW"
		if err := store.Write(func(tx *gorm.DB) error { _, err := store.Save(tx, asset, 1, false); return err }); err != nil {
			t.Error(err)
			return
		}
		if err := db.Model(&rule).Update("append_tags", "phase=TEST_NEW").Error; err != nil {
			t.Error(err)
			return
		}
		var err error
		winner, err = Enqueue(db, s, 0, 1)
		if err != nil {
			t.Error(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove(name)
	id, err := Enqueue(db, s, 0, 1)
	if err != nil || !injected || winner == 0 || id != winner {
		t.Fatalf("source-only race became a configuration conflict: id=%d winner=%d error=%v", id, winner, err)
	}
	d := state(t, db, id)
	if d.Task.State != Pending || d.Task.Attempts != 0 || d.PayloadPreview == nil || d.PayloadPreview.DeviceType != "TEST_NEW" || d.PayloadPreview.DeviceTags != "phase=TEST_NEW" {
		t.Fatalf("winning snapshot was lost: %+v", d)
	}
	var count int64
	if err := db.Model(&models.ExternalPushTask{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("duplicate task created", count, err)
	}
}
