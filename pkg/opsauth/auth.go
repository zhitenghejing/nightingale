// Package opsauth centralizes identity and business-group authorization for
// operations cases and their knowledge. Every entry point rechecks the DB.
package opsauth

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ccfos/nightingale/v6/models"
	nctx "github.com/ccfos/nightingale/v6/pkg/ctx"
	"gorm.io/gorm"
)

type Problem struct {
	Status  int
	Code    string
	Message string
}

func (e *Problem) Error() string                  { return e.Message }
func Fail(status int, code, message string) error { return &Problem{status, code, message} }

func User(db *gorm.DB, id int64) (*models.User, error) {
	u, err := models.UserGetById(nctx.NewContext(context.Background(), db, true), id)
	if err != nil {
		return nil, err
	}
	if u == nil || u.IsDisabled() {
		return nil, Fail(403, "identity_forbidden", "Account is missing or disabled")
	}
	return u, nil
}
func Check(db *gorm.DB, id, group int64, write bool) (*models.User, error) {
	u, err := User(db, id)
	if err != nil {
		return nil, err
	}
	c := nctx.NewContext(context.Background(), db, true)
	bg, err := models.BusiGroupGetById(c, group)
	if err != nil {
		return nil, err
	}
	if bg == nil {
		return nil, Fail(404, "group_missing", "Business group not found")
	}
	var flags []string
	if write {
		flags = []string{"rw"}
	}
	allowed, err := u.CanDoBusiGroup(c, bg, flags...)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, Fail(403, "group_forbidden", "No access to this business group")
	}
	return u, nil
}
func Groups(db *gorm.DB, id int64) ([]int64, bool, error) {
	u, err := User(db, id)
	if err != nil {
		return nil, false, err
	}
	if u.IsAdmin() {
		return nil, true, nil
	}
	ids, err := models.MyBusiGroupIds(nctx.NewContext(context.Background(), db, true), id)
	return ids, false, err
}
func JSON(v interface{}) models.OpsJSON {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("internal JSON encoding: %v", err))
	}
	return models.OpsJSON(b)
}
