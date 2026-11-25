package cmdb

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	defaultRackUnits = 42
	maxRackUnits     = 60
)

type importLocation struct{ room, rack string }

func (s *Store) importAssets(docs []Document, user int64, placementsOnly bool) ([]Document, error) {
	// Size new cabinets from the highest U in the entire file, independent of row order.
	locations := make([]importLocation, len(docs))
	units := make(map[importLocation]int)
	for i, d := range docs {
		if str(d, "rackId") != "" || str(d, "rack") == "" {
			continue
		}
		location := importLocation{room: str(d, "room"), rack: str(d, "rack")}
		if location.room == "" || len(location.room) > 160 || len(location.rack) > 160 {
			return nil, fmt.Errorf("第 %d 行：机房和机柜名称必填且各不超过 160 字节", i+2)
		}
		end := num(d, "uEnd")
		if end > maxRackUnits {
			return nil, fmt.Errorf("第 %d 行：机柜 U 位不能超过 %dU", i+2, maxRackUnits)
		}
		locations[i] = location
		units[location] = max(defaultRackUnits, units[location], end)
	}

	out := make([]Document, 0, len(docs))
	err := s.Write(func(tx *gorm.DB) error {
		racks := make(map[importLocation]string)
		seenSerials := make(map[string]bool)
		for i, d := range docs {
			var current Asset
			if placementsOnly {
				if str(d, "rackId") == "" && locations[i].rack == "" {
					if num(d, "uStart") != 0 || num(d, "uEnd") != 0 {
						return fmt.Errorf("第 %d 行：配置 U 位时必须指定机柜", i+2)
					}
					continue
				}
				serial := strings.ToLower(str(d, "serial"))
				if seenSerials[serial] {
					return fmt.Errorf("第 %d 行：文件中存在重复序列号", i+2)
				}
				seenSerials[serial] = true
				err := tx.Where("serial = ? AND deleted = ?", serial, false).Take(&current).Error
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return fmt.Errorf("第 %d 行：序列号对应的在用设备不存在", i+2)
				}
				if err != nil {
					return fmt.Errorf("第 %d 行：%w", i+2, err)
				}
				if current.Start > 0 || current.End > 0 {
					continue
				}
				existing := decode(current)
				for _, key := range []string{"room", "rack", "rackId", "uStart", "uEnd"} {
					existing[key] = d[key]
				}
				d = existing
			}
			if location := locations[i]; location.rack != "" {
				id := racks[location]
				if id == "" {
					var err error
					id, err = ensureImportRack(tx, location, units[location], user)
					if err != nil {
						return fmt.Errorf("第 %d 行：%w", i+2, err)
					}
					racks[location] = id
				}
				d["rackId"] = id
			}
			if placementsOnly && current.RackID == str(d, "rackId") && num(d, "uStart") == 0 && num(d, "uEnd") == 0 {
				continue
			}
			saved, err := s.Save(tx, d, user, !placementsOnly)
			if err != nil {
				return fmt.Errorf("第 %d 行：%w", i+2, err)
			}
			out = append(out, saved)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func ensureImportRack(tx *gorm.DB, location importLocation, units int, user int64) (string, error) {
	var room Room
	err := tx.Where("name = ?", location.room).Take(&room).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		room = Room{ID: uuid.NewString(), Name: location.room}
		if err := tx.Create(&room).Error; err != nil {
			return "", err
		}
		if err := audit(tx, user, room.ID, "room.create", "", encode(room)); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}

	var rack Rack
	err = tx.Where("room_id = ? AND name = ?", room.ID, location.rack).Take(&rack).Error
	if err == nil {
		return rack.ID, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}
	rack = Rack{ID: uuid.NewString(), RoomID: room.ID, Name: location.rack, Units: units, Version: 1}
	if err := tx.Create(&rack).Error; err != nil {
		return "", err
	}
	return rack.ID, audit(tx, user, rack.ID, "rack.save", "", encode(rack))
}
