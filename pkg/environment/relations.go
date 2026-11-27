package environment

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/opsauth"
	"gorm.io/gorm"
)

func now() int64 { return time.Now().Unix() }

type RelationInput struct {
	FromRef     string `json:"from_ref"`
	ToRef       string `json:"to_ref"`
	Kind        string `json:"kind"`
	FromVersion string `json:"from_version"`
	ToVersion   string `json:"to_version"`
	Note        string `json:"note"`
	ValidUntil  int64  `json:"valid_until"`
}
type RelationView struct {
	models.EnvironmentRelation
	From      Entity `json:"from"`
	To        Entity `json:"to"`
	Effective string `json:"effective"`
	Reason    string `json:"reason,omitempty"`
}

func validateKind(k string, a, b Entity) error {
	if a.Kind == "alert" || a.Kind == "asset_change" || b.Kind == "alert" || b.Kind == "asset_change" {
		return bad("事件及审计记录不能作为人工依赖端点")
	}
	if a.Ref == b.Ref {
		return bad("对象不能与自身建立关系")
	}
	switch k {
	case "describes":
		if a.Kind != "document" {
			return bad("资料描述关系必须从资料指向对象")
		}
	case "depends_on":
		if a.Kind == "document" || b.Kind == "document" {
			return bad("文档不是运行依赖对象")
		}
	case "runs_on":
		if a.Kind != "service" || (b.Kind != "target" && b.Kind != "asset" && b.Kind != "device") {
			return bad("部署关系从业务/应用指向设备")
		}
	case "belongs_to":
		if b.Kind != "service" || a.Kind == "document" {
			return bad("所属关系须指向业务/应用")
		}
	case "maps_to":
		if (a.Kind != "target" && a.Kind != "device") || (b.Kind != "asset" && b.Kind != "target") {
			return bad("监控映射须从监控设备指向资产或Target")
		}
	default:
		return bad("不支持的关系类型")
	}
	return nil
}
func (s *Store) Add(user int64, in RelationInput) (models.EnvironmentRelation, error) {
	return s.add(user, in, "manual", "confirmed")
}
func (s *Store) add(user int64, in RelationInput, source, status string) (models.EnvironmentRelation, error) {
	var out models.EnvironmentRelation
	if utf8.RuneCountInString(in.Note) > 2000 || strings.TrimSpace(in.Note) == "" || in.ValidUntil <= now() || in.ValidUntil > now()+366*86400 {
		return out, bad("请填写来源说明及一年内的核实有效期")
	}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		t := &Store{DB: tx}
		a, e := t.resolve(user, in.FromRef, true)
		if e != nil {
			return e
		}
		b, e := t.resolve(user, in.ToRef, true)
		if e != nil {
			return e
		}
		if e = validateKind(in.Kind, a, b); e != nil {
			return e
		}
		if in.FromVersion != a.Fingerprint || in.ToVersion != b.Fingerprint {
			return conflict()
		}
		identity := hash([]string{a.Ref, b.Ref, in.Kind})
		var old models.EnvironmentRelation
		r := tx.Where("identity = ?", identity).Limit(1).Find(&old)
		if r.Error != nil {
			return r.Error
		}
		if old.ID > 0 {
			out = old
			if old.Status == status && old.FromVersion == a.Fingerprint && old.ToVersion == b.Fingerprint && old.ValidUntil > now() {
				return nil
			}
			return conflict()
		}
		out = models.EnvironmentRelation{Identity: identity, FromRef: a.Ref, ToRef: b.Ref, Kind: in.Kind, Status: status, FromVersion: a.Fingerprint, ToVersion: b.Fingerprint, Source: source, Note: in.Note, ValidUntil: in.ValidUntil, CreatedBy: user, UpdatedBy: user, UpdatedAt: now(), Revision: 1}
		if e = tx.Create(&out).Error; e != nil {
			return e
		}
		return audit(tx, user, out.ID, a.Ref, "create", out)
	})
	return out, err
}
func audit(tx *gorm.DB, user, id int64, ref, action string, detail interface{}) error {
	b, _ := json.Marshal(detail)
	return tx.Create(&models.EnvironmentChange{RelationID: id, ObjectRef: ref, ActorID: user, Action: action, Detail: string(b), CreatedAt: now()}).Error
}
func (s *Store) Change(user, id, revision int64, action string, in RelationInput) (models.EnvironmentRelation, error) {
	var out models.EnvironmentRelation
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if e := tx.First(&out, id).Error; e != nil {
			return fail()
		}
		t := &Store{DB: tx}
		a, e := t.resolve(user, out.FromRef, true)
		if e != nil {
			return e
		}
		b, e := t.resolve(user, out.ToRef, true)
		if e != nil {
			return e
		}
		if out.Revision != revision {
			return conflict()
		}
		status := ""
		switch action {
		case "confirm":
			if out.Status != "candidate" {
				return conflict()
			}
			if out.FromVersion != a.Fingerprint || out.ToVersion != b.Fingerprint || out.ValidUntil <= now() {
				return conflict()
			}
			status = "confirmed"
		case "reject":
			if out.Status != "candidate" {
				return conflict()
			}
			status = "rejected"
		case "withdraw":
			if out.Status != "confirmed" && out.Status != "candidate" {
				return conflict()
			}
			status = "withdrawn"
		case "renew":
			if out.Status == "candidate" || out.Status == "rejected" {
				return bad("候选请先确认或拒绝")
			}
			if in.FromVersion != a.Fingerprint || in.ToVersion != b.Fingerprint || in.ValidUntil <= now() || in.ValidUntil > now()+366*86400 || strings.TrimSpace(in.Note) == "" || utf8.RuneCountInString(in.Note) > 2000 {
				return conflict()
			}
			out.FromVersion = a.Fingerprint
			out.ToVersion = b.Fingerprint
			out.ValidUntil = in.ValidUntil
			out.Note = in.Note
			status = "confirmed"
		default:
			return bad("不支持的关系操作")
		}
		r := tx.Model(&models.EnvironmentRelation{}).Where("id = ? AND revision = ?", id, revision).Updates(map[string]interface{}{"status": status, "from_version": out.FromVersion, "to_version": out.ToVersion, "valid_until": out.ValidUntil, "note": out.Note, "updated_by": user, "updated_at": now(), "revision": revision + 1})
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected != 1 {
			return conflict()
		}
		out.Status = status
		out.Revision++
		out.UpdatedBy = user
		out.UpdatedAt = now()
		return audit(tx, user, out.ID, out.FromRef, action, out)
	})
	return out, err
}
func (s *Store) view(user int64, r models.EnvironmentRelation) (RelationView, error) {
	v := RelationView{EnvironmentRelation: r, Effective: r.Status}
	var e error
	if v.From, e = s.Get(user, r.FromRef); e != nil {
		return v, e
	}
	if v.To, e = s.Get(user, r.ToRef); e != nil {
		return v, e
	}
	if r.FromVersion != v.From.Fingerprint || r.ToVersion != v.To.Fingerprint {
		v.Effective = "stale"
		v.Reason = "对象信息已变化，须重新核实"
	} else if r.ValidUntil <= now() {
		v.Effective = "expired"
		v.Reason = "超过核实有效期"
	}
	return v, nil
}

type Context struct {
	Entity          Entity                       `json:"entity"`
	Relations       []RelationView               `json:"relations"`
	NativeRelations []NativeRelation             `json:"native_relations"`
	Gaps            []string                     `json:"gaps"`
	Truncated       bool                         `json:"truncated"`
	Evidence        map[string]string            `json:"evidence"`
	Discovery       *models.EnvironmentDiscovery `json:"discovery,omitempty"`
}
type NativeRelation struct {
	From   Entity `json:"from"`
	To     Entity `json:"to"`
	Kind   string `json:"kind"`
	Source string `json:"source"`
}

func (s *Store) Context(user int64, ref string) (Context, error) {
	out := Context{Relations: []RelationView{}, NativeRelations: []NativeRelation{}, Gaps: []string{"关系覆盖不保证完整；没有关系不表示没有依赖。", "资产记录和文档不能证明实时健康，需进一步查询监控和日志。"}, Evidence: map[string]string{}}
	var e error
	out.Entity, e = s.Get(user, ref)
	if e != nil {
		return out, e
	}
	out.Evidence[ref] = out.Entity.Fingerprint
	if out.Entity.Kind == "document" {
		_, id, _ := parse(ref)
		var job models.EnvironmentDiscovery
		if e := s.DB.Where("version_id = ?", id).Limit(1).Find(&job).Error; e != nil {
			return out, e
		}
		if job.VersionID > 0 {
			job.Note = strings.ReplaceAll(job.Note, "environment_forbidden", "权限不足")
			out.Discovery = &job
		}
	}

	var rows []models.EnvironmentRelation
	if e = s.DB.Where("from_ref = ? OR to_ref = ?", ref, ref).Order("id desc").Limit(201).Find(&rows).Error; e != nil {
		return out, e
	}
	for _, r := range rows {
		v, e := s.view(user, r)
		if e != nil {
			continue
		}
		if len(out.Relations) >= 100 {
			out.Truncated = true
			break
		}
		out.Relations = append(out.Relations, v)
		out.Evidence["relation:"+fmt.Sprint(r.ID)] = relationReceipt(v)
		out.Evidence[v.From.Ref] = v.From.Fingerprint
		out.Evidence[v.To.Ref] = v.To.Fingerprint
	}
	if out.Entity.Kind == "device" {
		var d models.MonitorDevice
		if s.DB.First(&d, "id = ?", strings.TrimPrefix(ref, "device:")).Error == nil {
			if d.CMDBID != "" {
				if a, e := s.Get(user, "asset:"+d.CMDBID); e == nil {
					out.NativeRelations = append(out.NativeRelations, NativeRelation{out.Entity, a, "maps_to", "采集设备已保存的资产绑定"})
					out.Evidence[a.Ref] = a.Fingerprint
				}
			}
			var t models.Target
			if s.DB.Where("ident = ?", d.Ident).First(&t).Error == nil {
				if a, e := s.Get(user, fmt.Sprintf("target:%d", t.Id)); e == nil {
					out.NativeRelations = append(out.NativeRelations, NativeRelation{out.Entity, a, "maps_to", "采集设备与原生Target的精确ident"})
					out.Evidence[a.Ref] = a.Fingerprint
				}
			}
		}
	}
	if out.Entity.Kind == "target" || out.Entity.Kind == "asset" {
		query := s.DB.Where("deleted = ?", false)
		if out.Entity.Kind == "target" {
			query = query.Where("ident = ?", out.Entity.Attributes["ident"])
		} else {
			query = query.Where("cmdb_id = ?", strings.TrimPrefix(ref, "asset:"))
		}
		var devices []models.MonitorDevice
		// Read the stored binding; do not infer asset identity from an IP.
		if err := query.Limit(100).Find(&devices).Error; err != nil {
			return out, err
		}
		for _, d := range devices {
			device, e := s.Get(user, "device:"+d.ID)
			if e != nil {
				continue
			}
			out.NativeRelations = append(out.NativeRelations, NativeRelation{device, out.Entity, "maps_to", "已保存的采集设备/资产/Target配置"})
			out.Evidence[device.Ref] = device.Fingerprint
		}
	}

	return out, nil
}

// Validate is shared by tool publication and conversation replay. A relation
// update, source deletion or permission revocation invalidates its old receipt.
func (s *Store) Validate(user int64, receipts map[string]string) error {
	if _, e := opsauth.User(s.DB, user); e != nil {
		return e
	}
	if len(receipts) > 350 {
		return bad("证据范围过大")
	}
	for ref, expected := range receipts {
		if strings.HasPrefix(ref, "relation:") {
			_, id, e := parse(ref)
			if e != nil {
				return e
			}
			var r models.EnvironmentRelation
			if s.DB.First(&r, id).Error != nil {
				return fail()
			}
			v, e := s.view(user, r)
			if e != nil {
				return e
			}
			if expected != relationReceipt(v) {
				return conflict()
			}
		} else {
			v, e := s.Get(user, ref)
			if e != nil {
				return e
			}
			if expected != v.Fingerprint {
				return conflict()
			}
		}
	}
	return nil
}

func relationReceipt(v RelationView) string {
	return hash([]interface{}{v.EnvironmentRelation, v.Effective})
}
