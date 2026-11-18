// Package environment joins authorized, versioned references without copying
// source credentials or treating inferred relations as established facts.
package environment

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/cmdb"
	nctx "github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/ccfos/nightingale/v6/pkg/knowledgebase"
	"github.com/ccfos/nightingale/v6/pkg/opsauth"
	"gorm.io/gorm"
)

type Store struct{ DB *gorm.DB }
type Entity struct {
	Ref         string                 `json:"ref"`
	Kind        string                 `json:"kind"`
	Name        string                 `json:"name"`
	URL         string                 `json:"url"`
	Fingerprint string                 `json:"fingerprint"`
	Attributes  map[string]interface{} `json:"attributes"`
	Writable    bool                   `json:"writable"`
}

func hash(v interface{}) string { b, _ := json.Marshal(v); return fmt.Sprintf("%x", sha256.Sum256(b)) }
func fail() error {
	return opsauth.Fail(403, "environment_forbidden", "没有权限查阅，或对象已失效")
}
func bad(s string) error { return opsauth.Fail(422, "environment_input", s) }
func conflict() error {
	return opsauth.Fail(409, "environment_changed", "对象或关系已变化，请刷新并重新确认")
}
func parse(ref string) (string, int64, error) {
	p := strings.Split(ref, ":")
	if len(p) != 2 {
		return "", 0, bad("对象引用格式错误")
	}
	id, e := strconv.ParseInt(p[1], 10, 64)
	if e != nil || id <= 0 {
		return "", 0, bad("对象引用格式错误")
	}
	return p[0], id, nil
}
func (s *Store) assetPermission(user int64, write bool) error {
	u, e := opsauth.User(s.DB, user)
	if e != nil {
		return fail()
	}
	if u.IsAdmin() {
		return nil
	}
	var a cmdb.Access
	if s.DB.First(&a, "user_id = ?", user).Error != nil || !a.Read || (write && !a.Edit) {
		return fail()
	}
	return nil
}
func (s *Store) entity(user int64, ref string, write bool) (Entity, error) {
	out := Entity{Ref: ref, Attributes: map[string]interface{}{}}
	u, e := opsauth.User(s.DB, user)
	if e != nil {
		return out, fail()
	}
	kind, id, e := parse(ref)
	if e != nil {
		return out, e
	}
	out.Kind = kind
	c := nctx.NewContext(context.Background(), s.DB, true)
	switch kind {
	case "target":
		var t models.Target
		if s.DB.First(&t, id).Error != nil {
			return out, fail()
		}
		gids, e := models.TargetGroupIdsGetByIdent(c, t.Ident)
		if e != nil {
			return out, e
		}
		if len(gids) == 0 {
			gids = []int64{t.GroupId}
		}
		allowed := u.IsAdmin()
		for _, g := range gids {
			if g == 0 && !write {
				allowed = true
				continue
			}
			if _, e := opsauth.Check(s.DB, user, g, write); e == nil {
				allowed = true
			}
		}
		if !allowed {
			return out, fail()
		}
		sort.Slice(gids, func(i, j int) bool { return gids[i] < gids[j] })
		out.Name = t.Ident
		out.URL = "/targets"
		out.Attributes = map[string]interface{}{"ident": t.Ident, "host_ip": t.HostIp, "note": t.Note, "tags": strings.Fields(t.Tags), "os": t.OS}
		out.Fingerprint = hash([]interface{}{out.Attributes, gids, t.Id})
	case "service":
		var o models.EnvironmentObject
		if s.DB.First(&o, "id = ? AND deleted = ?", id, false).Error != nil {
			return out, fail()
		}
		if _, e = opsauth.Check(s.DB, user, o.GroupID, write); e != nil {
			return out, fail()
		}
		out.Name = o.Name
		out.URL = "/targets"
		out.Attributes = map[string]interface{}{"group_id": o.GroupID, "type": o.Kind, "description": o.Description, "revision": o.Revision}
		out.Fingerprint = hash(o)
	case "alert":
		if write {
			return out, fail()
		}
		var a models.AlertHisEvent
		if s.DB.First(&a, id).Error != nil {
			return out, fail()
		}
		if _, e = opsauth.Check(s.DB, user, a.GroupId, false); e != nil {
			return out, fail()
		}
		out.Name = a.RuleName
		out.URL = fmt.Sprintf("/alert-his-events/%d", a.Id)
		out.Attributes = map[string]interface{}{"target_ident": a.TargetIdent, "severity": a.Severity, "trigger_time": a.TriggerTime, "is_recovered": a.IsRecovered}
		out.Fingerprint = hash([]interface{}{a.Id, a.GroupId, out.Attributes})
	case "asset_change":
		if write {
			return out, fail()
		}
		var a cmdb.Change
		if s.DB.First(&a, id).Error != nil {
			return out, fail()
		}
		if _, e = s.resolve(user, "asset:"+a.AssetID, false); e != nil {
			return out, e
		}
		out.Name = "资产台账变更：" + a.Action
		out.URL = "/targets/cmdb"
		out.Attributes = map[string]interface{}{"asset_ref": "asset:" + a.AssetID, "action": a.Action, "created_at": a.CreatedAt.Unix()}
		out.Fingerprint = hash(a)
	case "document":
		k, e := (&knowledgebase.Store{DB: s.DB}).Version(user, id)
		if e != nil {
			return out, fail()
		}
		if k.State != "ready" {
			return out, opsauth.Fail(409, "document_not_ready", "资料原文尚未解析完成，暂不能建立或提取关联")
		}
		if write && !u.IsAdmin() {
			var version models.ArchiveDocumentVersion
			if s.DB.First(&version, k.SourceVersionID).Error != nil || version.CreateBy != user {
				return out, fail()
			}
		}
		out.Name = k.Title
		out.URL = fmt.Sprintf("/archives/%d/documents/%d?version=%d", k.ArchiveID, k.DocumentID, k.SourceVersionID)
		out.Attributes = map[string]interface{}{"sequence": k.Sequence, "source_version_id": k.SourceVersionID}
		out.Fingerprint = hash([]interface{}{k.ID, k.Digest, k.SourceVersionID})
	default:
		return out, bad("不支持的对象类型")
	}
	if write {
		out.Writable = true
	} else {
		_, e := s.entityWrite(user, ref)
		out.Writable = e == nil
	}
	return out, nil
}

// UUID sources have their own parser; no hostname/IP guessing is performed.
func (s *Store) entityWrite(user int64, ref string) (Entity, error) {
	return s.resolve(user, ref, true)
}
func (s *Store) resolve(user int64, ref string, write bool) (Entity, error) {
	p := strings.SplitN(ref, ":", 2)
	if len(p) != 2 {
		return Entity{}, bad("对象引用格式错误")
	}
	if p[0] != "asset" && p[0] != "device" {
		return s.entity(user, ref, write)
	}
	out := Entity{Ref: ref, Kind: p[0], Attributes: map[string]interface{}{}}
	if len(p[1]) > 64 || p[1] == "" {
		return out, bad("对象引用格式错误")
	}
	u, e := opsauth.User(s.DB, user)
	if e != nil {
		return out, fail()
	}
	if p[0] == "asset" {
		if e = s.assetPermission(user, write); e != nil {
			return out, e
		}
		var a cmdb.Asset
		if s.DB.First(&a, "id = ? AND deleted = ?", p[1], false).Error != nil {
			return out, fail()
		}
		var payload map[string]interface{}
		if json.Unmarshal([]byte(a.Payload), &payload) != nil {
			return out, fail()
		}
		for _, k := range []string{"hostname", "businessIp", "managementIp", "business", "customer", "type", "model", "lifecycle", "note"} {
			out.Attributes[k] = payload[k]
		}
		out.Attributes["rack_id"] = a.RackID
		out.Attributes["serial"] = a.Serial
		out.Attributes["u_start"] = a.Start
		out.Attributes["u_end"] = a.End
		out.Name, _ = payload["hostname"].(string)
		if out.Name == "" {
			out.Name = a.Serial
		}
		out.URL = "/targets/cmdb"
		out.Fingerprint = hash([]interface{}{a.ID, a.Serial, out.Attributes})
	} else {
		var d models.MonitorDevice
		if s.DB.First(&d, "id = ? AND deleted = ?", p[1], false).Error != nil {
			return out, fail()
		}
		if _, e = opsauth.Check(s.DB, user, d.GroupID, write); e != nil {
			return out, fail()
		}
		perm, e := u.CheckPerm(nctx.NewContext(context.Background(), s.DB, true), "/collection/read")
		if e != nil || !perm {
			return out, fail()
		}
		out.Name = d.Name
		out.URL = "/targets"
		out.Attributes = map[string]interface{}{"ident": d.Ident, "address": d.Address, "port": d.Port, "protocol": d.Protocol, "enabled": d.Enabled}
		out.Fingerprint = hash([]interface{}{d.ID, d.Name, d.GroupID, d.CMDBID, out.Attributes})
	}
	if write {
		out.Writable = true
	} else {
		_, e = s.resolve(user, ref, true)
		out.Writable = e == nil
	}
	return out, nil
}
func (s *Store) Get(user int64, ref string) (Entity, error) { return s.resolve(user, ref, false) }

type SearchResult struct {
	Items     []Entity `json:"items"`
	Truncated bool     `json:"truncated"`
	Note      string   `json:"note"`
}

func (s *Store) Search(user int64, q string) (SearchResult, error) {
	out := SearchResult{Items: []Entity{}, Note: "名称/IP仅用于发现候选；多匹配须确认，历史归属未自动证明。每类最多扫描500条。"}
	if _, e := opsauth.User(s.DB, user); e != nil {
		return out, fail()
	}
	q = strings.TrimSpace(q)
	if len(q) > 200 {
		return out, bad("搜索内容过长")
	}
	refs := []string{}
	var targets []models.Target
	if e := s.DB.Order("id").Limit(500).Find(&targets).Error; e != nil {
		return out, e
	}
	for _, v := range targets {
		refs = append(refs, fmt.Sprintf("target:%d", v.Id))
	}
	var assets []cmdb.Asset
	if e := s.DB.Where("deleted = ?", false).Order("id").Limit(500).Find(&assets).Error; e != nil {
		return out, e
	}
	for _, v := range assets {
		refs = append(refs, "asset:"+v.ID)
	}
	var devices []models.MonitorDevice
	if e := s.DB.Where("deleted = ?", false).Order("id").Limit(500).Find(&devices).Error; e != nil {
		return out, e
	}
	for _, v := range devices {
		refs = append(refs, "device:"+v.ID)
	}
	var objs []models.EnvironmentObject
	if e := s.DB.Where("deleted = ?", false).Order("id").Limit(500).Find(&objs).Error; e != nil {
		return out, e
	}
	for _, v := range objs {
		refs = append(refs, fmt.Sprintf("service:%d", v.ID))
	}
	docs, e := (&knowledgebase.Store{DB: s.DB}).List(user)
	if e != nil {
		return out, e
	}
	for i, v := range docs {
		if i >= 500 {
			break
		}
		if v.State == "ready" {
			refs = append(refs, fmt.Sprintf("document:%d", v.ID))
		}
	}
	for _, r := range refs {
		v, e := s.Get(user, r)
		if e != nil {
			continue
		}
		b, _ := json.Marshal(v.Attributes)
		if q != "" && !strings.Contains(strings.ToLower(v.Name+" "+v.Ref+" "+string(b)), strings.ToLower(q)) {
			continue
		}
		if len(out.Items) >= 100 {
			out.Truncated = true
			break
		}
		out.Items = append(out.Items, v)
	}
	return out, nil
}
func (s *Store) SaveObject(user int64, o models.EnvironmentObject) (models.EnvironmentObject, error) {
	if _, e := opsauth.Check(s.DB, user, o.GroupID, true); e != nil {
		return o, e
	}
	o.Name = strings.TrimSpace(o.Name)
	if o.Name == "" || utf8.RuneCountInString(o.Name) > 160 || utf8.RuneCountInString(o.Description) > 4000 {
		return o, bad("业务对象名称或描述不合法")
	}
	if o.Kind != "service" && o.Kind != "application" && o.Kind != "database" && o.Kind != "vip" {
		return o, bad("不支持的业务对象类型")
	}
	e := s.DB.Transaction(func(tx *gorm.DB) error {
		if o.ID == 0 {
			o.Revision = 1
			o.UpdatedAt = now()
			if e := tx.Create(&o).Error; e != nil {
				return e
			}
			return audit(tx, user, 0, fmt.Sprintf("service:%d", o.ID), "create_object", o)
		}
		old, e := (&Store{DB: tx}).resolve(user, fmt.Sprintf("service:%d", o.ID), true)
		if e != nil {
			return e
		}
		_ = old
		r := tx.Model(&models.EnvironmentObject{}).Where("id = ? AND revision = ?", o.ID, o.Revision).Updates(map[string]interface{}{"name": o.Name, "kind": o.Kind, "description": o.Description, "group_id": o.GroupID, "deleted": o.Deleted, "revision": o.Revision + 1, "updated_at": now()})
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected != 1 {
			return conflict()
		}
		o.Revision++
		return audit(tx, user, 0, fmt.Sprintf("service:%d", o.ID), "update_object", o)
	})
	return o, e
}
