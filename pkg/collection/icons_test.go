package collection

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/stretchr/testify/require"
)

func iconFixture(mime string, raw []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw)
}

func verifyIconLibrary(t *testing.T, s *Store, scope Scope) {
	t.Helper()
	input := IconInput{Name: "Device artwork.svg", DataURL: iconFixture("image/svg+xml", []byte(`<!--`+strings.Repeat("artwork", 16000)+`--><svg xmlns="http://www.w3.org/2000/svg" width="32" height="32"><rect width="32" height="32" fill="blue"/></svg>`))}
	_, err := s.AddIcon(Scope{}, input)
	require.Error(t, err)
	require.Equal(t, 403, err.(*Error).Code)
	first, err := s.AddIcon(scope, input)
	require.NoError(t, err)
	input.Name = "Another filename.svg"
	second, err := s.AddIcon(scope, input)
	require.NoError(t, err)
	require.Equal(t, first, second)
	icons, err := s.Icons()
	require.NoError(t, err)
	require.Contains(t, icons, first)
	raw, mime, err := s.IconContent(first.ID)
	require.NoError(t, err)
	require.Equal(t, "image/svg+xml", mime)
	require.Contains(t, string(raw), "<svg")
	reopened := &Store{DB: s.DB}
	require.NoError(t, reopened.Migrate())
	content, _, err := reopened.IconContent(first.ID)
	require.NoError(t, err)
	require.Equal(t, raw, content)
	for _, id := range []string{"../file", strings.Repeat("a", 64), strings.ToUpper(first.ID)} {
		_, _, err = s.IconContent(id)
		require.Error(t, err)
	}
	var count int64
	require.NoError(t, s.DB.Model(&models.MonitorIcon{}).Where("id = ?", first.ID).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestReusableIconLibrary(t *testing.T) {
	s, scope, _, _, _ := testStore(t)
	verifyIconLibrary(t, s, scope)
}

func TestIconContentValidation(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	var pngData, jpgData bytes.Buffer
	require.NoError(t, png.Encode(&pngData, img))
	require.NoError(t, jpeg.Encode(&jpgData, img, nil))
	for mime, raw := range map[string][]byte{"image/png": pngData.Bytes(), "image/jpeg": jpgData.Bytes(), "image/svg+xml": []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"/>`)} {
		t.Run(mime, func(t *testing.T) {
			data, got, err := decodeIcon(iconFixture(mime, raw))
			require.NoError(t, err)
			require.Equal(t, mime, got)
			require.Equal(t, raw, data)
		})
	}
	for _, value := range []string{"", "https://example.org/icon.png", "data:image/png;base64,broken", iconFixture("image/png", []byte("not a PNG")), iconFixture("text/html", []byte("html")), iconFixture("image/png", jpgData.Bytes()), iconFixture("image/svg+xml", []byte(`<html/>`)), iconFixture("image/svg+xml", []byte(`<svg>`)), iconFixture("image/svg+xml", []byte(`<svg/><svg/>`)), iconFixture("image/svg+xml", []byte(`<!DOCTYPE svg><svg/>`)), iconFixture("image/svg+xml", bytes.Repeat([]byte("x"), MaxIconBytes+1))} {
		_, _, err := decodeIcon(value)
		require.Error(t, err, value[:min(len(value), 60)])
	}
	s, scope, _, _, _ := testStore(t)
	for _, name := range []string{" ", "bad\x00name", strings.Repeat("a", 129)} {
		_, err := s.AddIcon(scope, IconInput{Name: name, DataURL: iconFixture("image/png", pngData.Bytes())})
		require.Error(t, err)
	}
}
