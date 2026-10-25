package cmdb

import (
	"errors"

	"gorm.io/gorm"
)

type purgeTarget struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

// The caller holds Store.Write's shared lock, also used by restore and catalog writes.
// Explicit versions make clearing a snapshot safe against restoration and new arrivals.
func purgeAssets(tx *gorm.DB, targets []purgeTarget, user int64) error {
	if len(targets) == 0 {
		return errors.New("请选择需要彻底删除的回收站设备")
	}
	seen := make(map[string]bool, len(targets))
	for _, target := range targets {
		if target.ID == "" || len(target.ID) > 36 || target.Version < 1 || seen[target.ID] {
			return errors.New("设备 ID 或版本无效，或包含重复设备，请刷新回收站")
		}
		seen[target.ID] = true
		var old Asset
		if err := tx.First(&old, "id = ?", target.ID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("设备不存在，请刷新回收站")
			}
			return err
		}
		if !old.Deleted {
			return errors.New("只能彻底删除回收站设备，设备可能已恢复，请刷新回收站")
		}
		if old.Version != target.Version {
			return errors.New("设备已修改，请刷新回收站后重试")
		}
		if err := tx.Where("asset_id = ?", old.ID).Delete(&Disk{}).Error; err != nil {
			return err
		}
		result := tx.Where("id = ? AND version = ? AND deleted = ?", old.ID, target.Version, true).Delete(&Asset{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("设备已修改，请刷新回收站后重试")
		}
		if err := audit(tx, user, old.ID, "purge", encode(decode(old)), ""); err != nil {
			return err
		}
	}
	return nil
}
