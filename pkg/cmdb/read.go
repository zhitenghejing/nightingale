package cmdb

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

// AssetSnapshot exposes only inventory facts. Credentials and arbitrary payload
// fields never cross this interface. Record time is not a physical inspection.
type AssetSnapshot struct {
	ID          string    `json:"id"`
	Serial      string    `json:"serial"`
	Version     int       `json:"version"`
	UpdatedAt   time.Time `json:"updated_at"`
	Fields      Document  `json:"fields"`
	Placement   string    `json:"placement_state"`
	Fingerprint string    `json:"-"`
}

func (s *Store) CheckRead(user User) error {
	if user.ID < 1 || user.Disabled != 0 {
		return errors.New("没有CMDB资产读取权限")
	}
	if user.Admin {
		return nil
	}
	var access Access
	if err := s.DB.First(&access, "user_id = ?", user.ID).Error; err != nil || !access.Read {
		return errors.New("没有CMDB资产读取权限")
	}
	return nil
}

// LookupAsset applies the same inventory ACL as the CMDB UI, then queries the
// unique serial/primary key before any limit. nil is a complete not-found result.
func (s *Store) LookupAsset(user User, serial, id string) (*AssetSnapshot, error) {
	if err := s.CheckRead(user); err != nil {
		return nil, err
	}
	serial = strings.TrimSpace(serial)
	id = strings.TrimSpace(id)
	if (serial == "" && id == "") || len(serial) > 240 || len(id) > 64 {
		return nil, errors.New("需要完整的字符串serial或asset_id，不能用空筛选查询资产")
	}
	query := s.DB.Where("deleted = ?", false)
	if serial != "" {
		query = query.Where("serial = ?", strings.ToLower(serial))
	}
	if id != "" {
		query = query.Where("id = ?", id)
	}
	var asset Asset
	if err := query.First(&asset).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	payload := decode(asset)
	fields := Document{}
	for _, key := range []string{"customer", "type", "vendor", "model", "hostname", "businessIp", "managementIp", "business", "note", "os", "level", "lifecycle", "cpuModel", "cpuCount", "memory", "memoryNote", "powerCount", "warrantyStart", "warrantyEnd", "expire", "warrantyType", "monitored", "alertEnabled", "monitorState"} {
		if v, ok := payload[key]; ok {
			fields[key] = v
		}
	}
	state := "unbound_ledger_location"
	for _, key := range []string{"room", "rack", "uStart", "uEnd"} {
		if v, ok := payload[key]; ok {
			fields[key] = v
		}
	}
	if asset.RackID != "" {
		state = "unresolved_rack_reference"
		// A stale payload location must not masquerade as the current rack binding.
		for _, key := range []string{"room", "rack", "uStart", "uEnd"} {
			delete(fields, key)
		}
		var rack Rack
		var room Room
		if err := s.DB.First(&rack, "id = ?", asset.RackID).Error; err != nil {
			return nil, err
		}
		if err := s.DB.First(&room, "id = ?", rack.RoomID).Error; err != nil {
			return nil, err
		}
		fields["room"] = room.Name
		fields["rack"] = rack.Name
		fields["rack_id"] = asset.RackID
		fields["uStart"] = asset.Start
		fields["uEnd"] = asset.End
		state = "registered_rack_binding"
	}
	out := &AssetSnapshot{ID: asset.ID, Serial: str(payload, "serial"), Version: asset.Version, UpdatedAt: asset.UpdatedAt, Fields: fields, Placement: state}
	if out.Serial == "" {
		out.Serial = asset.Serial
	}
	sum := sha256.Sum256([]byte(encode([]interface{}{asset.ID, asset.Version, asset.UpdatedAt, asset.Serial, asset.RackID, asset.Start, asset.End, string(asset.Payload), fields})))
	out.Fingerprint = hex.EncodeToString(sum[:])
	return out, nil
}
