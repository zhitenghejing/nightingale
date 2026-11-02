package collection

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ccfos/nightingale/v6/models"
	_ "golang.org/x/image/webp"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const MaxIconBytes = 2 << 20

type IconInput struct {
	Name    string `json:"name"`
	DataURL string `json:"data_url"`
}

type Icon struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	CreatedAt int64  `json:"created_at"`
}

func iconView(row models.MonitorIcon) Icon {
	return Icon{ID: row.ID, Name: row.Name, URL: "/api/n9e/collection/icons/" + row.ID + "/content", CreatedAt: row.CreatedAt}
}

func (s *Store) Icons() ([]Icon, error) {
	var rows []models.MonitorIcon
	err := s.DB.Omit("data").Order("created_at, id").Find(&rows).Error
	out := make([]Icon, 0, len(rows))
	for _, row := range rows {
		out = append(out, iconView(row))
	}
	return out, err
}

func decodeIcon(value string) ([]byte, string, error) {
	if len(value) > 3<<20 {
		return nil, "", bad("invalid icon image")
	}
	header, encoded, ok := strings.Cut(value, ",")
	mime := strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64")
	if !ok || header != "data:"+mime+";base64" || (mime != "image/png" && mime != "image/jpeg" && mime != "image/webp" && mime != "image/svg+xml") {
		return nil, "", bad("invalid icon image")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) == 0 || len(raw) > MaxIconBytes {
		return nil, "", bad("invalid icon image")
	}
	if mime == "image/svg+xml" {
		decoder := xml.NewDecoder(bytes.NewReader(raw))
		root, depth := false, 0
		for {
			token, err := decoder.Token()
			if err == io.EOF {
				if root && depth == 0 {
					return raw, mime, nil
				}
				break
			}
			if err != nil {
				break
			}
			switch token := token.(type) {
			case xml.Directive:
				return nil, "", bad("invalid icon image")
			case xml.StartElement:
				if depth == 0 {
					if root || token.Name.Local != "svg" || (token.Name.Space != "" && token.Name.Space != "http://www.w3.org/2000/svg") {
						return nil, "", bad("invalid icon image")
					}
					root = true
				}
				depth++
			case xml.EndElement:
				depth--
			case xml.CharData:
				if depth == 0 && strings.TrimSpace(string(token)) != "" {
					return nil, "", bad("invalid icon image")
				}
			}
		}
	} else {
		config, format, err := image.DecodeConfig(bytes.NewReader(raw))
		if err == nil && mime == "image/"+format && config.Width > 0 && config.Height > 0 && config.Width <= 4096 && config.Height <= 4096 {
			if _, _, err := image.Decode(bytes.NewReader(raw)); err == nil {
				return raw, mime, nil
			}
		}
	}
	return nil, "", bad("invalid icon image")
}

func (s *Store) AddIcon(scope Scope, input IconInput) (Icon, error) {
	if !scope.Admin {
		return Icon{}, denied()
	}
	name := strings.TrimSpace(input.Name)
	if strings.ContainsRune(name, 0) || !utf8.ValidString(name) || len([]rune(name)) < 1 || len([]rune(name)) > 128 {
		return Icon{}, bad("invalid icon name")
	}
	raw, mime, err := decodeIcon(input.DataURL)
	if err != nil {
		return Icon{}, err
	}
	row := models.MonitorIcon{ID: digest(append([]byte(mime+":"), raw...)), Name: name, MIME: mime, Data: models.MonitorText(base64.StdEncoding.EncodeToString(raw)), Size: len(raw), CreatedAt: time.Now().Unix()}
	// The primary key deduplicates even simultaneous imports on different Center replicas.
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
		return tx.First(&row, "id = ?", row.ID).Error
	})
	return iconView(row), err
}

func (s *Store) IconContent(id string) ([]byte, string, error) {
	decoded, err := hex.DecodeString(id)
	if err != nil || len(decoded) != 32 || strings.ToLower(id) != id {
		return nil, "", missing()
	}
	var row models.MonitorIcon
	if err = s.DB.First(&row, "id = ?", id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", missing()
	} else if err != nil {
		return nil, "", err
	}
	raw, err := base64.StdEncoding.DecodeString(string(row.Data))
	return raw, row.MIME, err
}
