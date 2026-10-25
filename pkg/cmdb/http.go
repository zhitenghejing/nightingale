package cmdb

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ccfos/nightingale/v6/pkg/i18nx"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type User struct {
	ID       int64 `json:"id"`
	Admin    bool  `json:"admin"`
	Disabled int   `json:"disabled"`
}
type API struct {
	Reports    *ReportStore
	Store      *Store
	AuthOrigin string
	Client     *http.Client
}

func NewHandler(s *Store, origin string) http.Handler {
	return &API{Store: s, AuthOrigin: strings.TrimRight(origin, "/"), Client: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func respond(w http.ResponseWriter, r *http.Request, status int, data interface{}, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	msg := ""
	if err != nil {
		msg = i18nx.Translate(r.Header.Get("X-Language"), err.Error())
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"dat": data, "err": msg})
}
func body(w http.ResponseWriter, r *http.Request, v interface{}) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 6<<20))
	if err := dec.Decode(v); err != nil {
		return errors.New("请求内容格式错误或超过 6 MB")
	}
	if err := dec.Decode(new(interface{})); err != io.EOF {
		return errors.New("请求只能包含一个 JSON 对象")
	}
	return nil
}
func (a *API) identity(r *http.Request) (User, error) {
	var u User
	// Require bearer authentication; cookies alone cannot authorize a cross-site mutation.
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || len(r.Header.Get("Authorization")) < 10 {
		return u, errors.New("请先登录夜莺")
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, a.AuthOrigin+"/api/n9e/self/profile", nil)
	if err != nil {
		return u, err
	}
	req.Header.Set("Authorization", r.Header.Get("Authorization"))
	res, err := a.Client.Do(req)
	if err != nil {
		return u, errors.New("夜莺身份服务暂不可用")
	}
	defer res.Body.Close()
	var envelope struct {
		Dat User            `json:"dat"`
		Err json.RawMessage `json:"err"`
	}
	if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&envelope) != nil || envelope.Dat.ID < 1 || envelope.Dat.Disabled != 0 {
		return u, errors.New("登录已失效，请重新登录")
	}
	if len(envelope.Err) > 0 && string(envelope.Err) != `""` && string(envelope.Err) != "null" {
		return u, errors.New("登录已失效，请重新登录")
	}
	return envelope.Dat, nil
}
func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u, err := a.identity(r)
	if err != nil {
		respond(w, r, 401, nil, err)
		return
	}
	a.ServeUser(w, r, u)
}

// ServeUser accepts an identity already verified by the center's auth/user middleware.
// Standalone handlers continue to verify bearer tokens through ServeHTTP.
func (a *API) ServeUser(w http.ResponseWriter, r *http.Request, u User) {
	if u.ID < 1 || u.Disabled != 0 {
		respond(w, r, 401, nil, errors.New("请先登录夜莺"))
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/cmdb/")
	if path == "health" && r.Method == http.MethodGet {
		db, err := a.Store.DB.DB()
		if err != nil || db.PingContext(r.Context()) != nil {
			respond(w, r, 503, nil, errors.New("数据库不可用"))
		} else {
			respond(w, r, 200, "ok", nil)
		}
		return
	}
	if r.URL.Path == "/api/cmdb/reports" || strings.HasPrefix(r.URL.Path, "/api/cmdb/reports/") {
		a.reportHTTP(w, r, u)
		return
	}
	access := Access{UserID: u.ID}
	if u.Admin {
		access = Access{UserID: u.ID, Read: true, Create: true, Edit: true, Delete: true, Import: true, Export: true}
	} else {
		if err := a.Store.DB.First(&access, "user_id = ?", u.ID).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			respond(w, r, 503, nil, errors.New("CMDB 权限暂不可用"))
			return
		}
	}
	if path == "access" {
		if r.Method == "GET" {
			respond(w, r, 200, map[string]interface{}{"permissions": access, "admin": u.Admin}, nil)
			return
		}
	}
	permission := access.Read
	if r.Method != "GET" {
		permission = access.Edit
		if path == "devices" {
			permission = access.Create
		}
		if path == "import" {
			permission = access.Import
		}
		if path == "delete" || path == "purge" || r.Method == http.MethodDelete {
			permission = access.Delete
		}
	}
	if path == "export" {
		permission = access.Export
	}
	if strings.HasPrefix(path, "acl") {
		permission = u.Admin
	}
	if !permission {
		respond(w, r, 403, nil, errors.New("没有该 CMDB 操作权限，请联系管理员授权"))
		return
	}
	result, err := a.route(w, r, u, path)
	if err != nil {
		respond(w, r, 400, nil, err)
		return
	}
	respond(w, r, 200, result, nil)
}
func (a *API) route(w http.ResponseWriter, r *http.Request, u User, path string) (interface{}, error) {
	db := a.Store.DB
	switch {
	case (path == "devices" || path == "export") && r.Method == "GET":
		return a.Store.Assets(r.URL.Query().Get("deleted") == "true")
	case path == "catalog" && r.Method == "GET":
		var rooms []Room
		var racks []Rack
		if err := db.Order("name").Find(&rooms).Error; err != nil {
			return nil, err
		}
		err := db.Order("name").Find(&racks).Error
		if rooms == nil {
			rooms = []Room{}
		}
		if racks == nil {
			racks = []Rack{}
		}
		return map[string]interface{}{"rooms": rooms, "racks": racks}, err
	case path == "changes" && r.Method == "GET":
		var changes []Change
		q := db.Order("id DESC").Limit(200)
		if id := r.URL.Query().Get("assetId"); id != "" {
			q = q.Where("asset_id = ?", id)
		}
		err := q.Find(&changes).Error
		if changes == nil {
			changes = []Change{}
		}
		return changes, err
	case path == "rooms" && (r.Method == "POST" || r.Method == "PUT"):
		var input struct {
			Room
			PreviousName string `json:"previousName"`
		}
		if err := body(w, r, &input); err != nil {
			return nil, err
		}
		room := input.Room
		room.Name = strings.TrimSpace(room.Name)
		if room.Name == "" || len(room.Name) > 160 {
			return nil, errors.New("机房名称必填且不超过 160 字节")
		}
		err := a.Store.Write(func(tx *gorm.DB) error {
			before, action := "", "room.create"
			if r.Method == "PUT" {
				var old Room
				if err := tx.First(&old, "id = ?", room.ID).Error; err != nil {
					return errors.New("机房不存在，请刷新")
				}
				if input.PreviousName != old.Name {
					return errors.New("机房已修改，请刷新后重试")
				}
				before, action = encode(old), "room.edit"
			} else {
				room.ID = uuid.NewString()
			}
			if err := tx.Save(&room).Error; err != nil {
				return errors.New("机房名称重复或保存失败")
			}
			return audit(tx, u.ID, room.ID, action, before, encode(room))
		})
		return room, err
	case path == "racks" && (r.Method == "POST" || r.Method == "PUT"):
		var rack Rack
		if err := body(w, r, &rack); err != nil {
			return nil, err
		}
		rack.Name = strings.TrimSpace(rack.Name)
		if rack.Name == "" || len(rack.Name) > 160 || rack.Units < 1 || rack.Units > 60 {
			return nil, errors.New("机柜名称必填，容量范围为 1–60U")
		}
		err := a.Store.Write(func(tx *gorm.DB) error {
			var room Room
			if tx.First(&room, "id = ?", rack.RoomID).Error != nil {
				return errors.New("请先选择有效机房")
			}
			before := ""
			if r.Method == "PUT" {
				var old Rack
				if tx.First(&old, "id = ?", rack.ID).Error != nil {
					return errors.New("机柜不存在")
				}
				if old.Version != rack.Version {
					return errors.New("机柜已修改，请刷新")
				}
				before = encode(old)
				var n int64
				if err := tx.Model(&Asset{}).Where("rack_id = ? AND deleted = ?", rack.ID, false).
					Where(clause.Gt{Column: "end", Value: rack.Units}).Count(&n).Error; err != nil {
					return err
				}
				if n > 0 {
					return errors.New("缩小容量将导致设备超出机柜")
				}
				rack.Version++
			} else {
				rack.ID = uuid.NewString()
				rack.Version = 1
			}
			if err := tx.Save(&rack).Error; err != nil {
				return errors.New("同一机房的机柜名称重复或保存失败")
			}
			return audit(tx, u.ID, rack.ID, "rack.save", before, encode(rack))
		})
		return rack, err
	case (strings.HasPrefix(path, "rooms/") || strings.HasPrefix(path, "racks/")) && r.Method == http.MethodDelete:
		room := strings.HasPrefix(path, "rooms/")
		id := strings.TrimPrefix(strings.TrimPrefix(path, "rooms/"), "racks/")
		var expected Rack
		if !room {
			if err := body(w, r, &expected); err != nil {
				return nil, err
			}
		}
		err := a.Store.Write(func(tx *gorm.DB) error {
			var n int64
			if room {
				var old Room
				if err := tx.First(&old, "id = ?", id).Error; err != nil {
					return errors.New("机房不存在，请刷新")
				}
				if err := tx.Model(&Rack{}).Where("room_id = ?", id).Count(&n).Error; err != nil {
					return err
				}
				if n > 0 {
					return errors.New("机房下还有机柜，请先迁移或删除机柜")
				}
				if err := tx.Delete(&old).Error; err != nil {
					return err
				}
				return audit(tx, u.ID, id, "room.delete", encode(old), "")
			}
			var old Rack
			if err := tx.First(&old, "id = ?", id).Error; err != nil {
				return errors.New("机柜不存在，请刷新")
			}
			if old.Version != expected.Version {
				return errors.New("机柜已修改，请刷新")
			}
			// Include unplaced and recycled devices so restoration never gains a dangling rack.
			if err := tx.Model(&Asset{}).Where("rack_id = ?", id).Count(&n).Error; err != nil {
				return err
			}
			if n > 0 {
				return errors.New("机柜内还有设备（含待上架及回收站），请先迁移；回收站设备需先恢复后迁移")
			}
			if err := tx.Delete(&old).Error; err != nil {
				return err
			}
			return audit(tx, u.ID, id, "rack.delete", encode(old), "")
		})
		return true, err
	case path == "devices" && r.Method == "POST" || strings.HasPrefix(path, "devices/") && r.Method == "PUT":
		var d Document
		if err := body(w, r, &d); err != nil {
			return nil, err
		}
		create := r.Method == "POST"
		if !create {
			d["id"] = strings.TrimPrefix(path, "devices/")
		}
		var saved Document
		err := a.Store.Write(func(tx *gorm.DB) error { var e error; saved, e = a.Store.Save(tx, d, u.ID, create); return e })
		return saved, err
	case path == "import" && r.Method == "POST":
		var docs []Document
		if err := body(w, r, &docs); err != nil {
			return nil, err
		}
		if len(docs) < 1 || len(docs) > 1000 {
			return nil, errors.New("每次导入 1–1000 台设备")
		}
		out := []Document{}
		err := a.Store.Write(func(tx *gorm.DB) error {
			for _, d := range docs {
				saved, err := a.Store.Save(tx, d, u.ID, true)
				if err != nil {
					return err
				}
				out = append(out, saved)
			}
			return nil
		})
		return out, err
	case path == "purge" && r.Method == http.MethodPost:
		var targets []purgeTarget
		if err := body(w, r, &targets); err != nil {
			return nil, err
		}
		err := a.Store.Write(func(tx *gorm.DB) error { return purgeAssets(tx, targets, u.ID) })
		return len(targets), err
	case (path == "delete" || path == "restore") && r.Method == "POST":
		var docs []Document
		if err := body(w, r, &docs); err != nil {
			return nil, err
		}
		if len(docs) < 1 || len(docs) > 1000 {
			return nil, errors.New("请选择 1–1000 台设备")
		}
		err := a.Store.Write(func(tx *gorm.DB) error {
			for _, d := range docs {
				var old Asset
				if tx.First(&old, "id = ?", str(d, "id")).Error != nil {
					return errors.New("设备不存在")
				}
				if old.Version != num(d, "version") {
					return errors.New("设备已修改，请刷新")
				}
				if path == "restore" {
					if !old.Deleted {
						return errors.New("设备不在回收站")
					}
					if err := tx.Model(&old).Update("deleted", false).Error; err != nil {
						return err
					}
					doc := decode(old)
					if _, err := a.Store.Save(tx, doc, u.ID, false); err != nil {
						return err
					}
					if err := audit(tx, u.ID, old.ID, "restore", encode(decode(old)), ""); err != nil {
						return err
					}
				} else {
					if old.Deleted {
						return errors.New("设备已删除")
					}
					if err := tx.Model(&old).Updates(map[string]interface{}{"deleted": true, "version": old.Version + 1}).Error; err != nil {
						return err
					}
					if err := audit(tx, u.ID, old.ID, "delete", string(old.Payload), ""); err != nil {
						return err
					}
				}
			}
			return nil
		})
		return true, err
	case path == "acl" && r.Method == "GET":
		var access []Access
		err := db.Find(&access).Error
		if access == nil {
			access = []Access{}
		}
		return access, err
	case path == "acl" && r.Method == "PUT":
		var access Access
		if err := body(w, r, &access); err != nil {
			return nil, err
		}
		if access.UserID < 1 {
			return nil, errors.New("用户 ID 不正确")
		}
		err := a.Store.Write(func(tx *gorm.DB) error {
			var old Access
			tx.First(&old, "user_id = ?", access.UserID)
			if err := tx.Save(&access).Error; err != nil {
				return err
			}
			return audit(tx, u.ID, "", "access.save", encode(old), encode(access))
		})
		return access, err
	}
	return nil, errors.New("接口或请求方法不存在")
}
