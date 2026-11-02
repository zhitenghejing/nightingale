package externalpush

import (
	"crypto/md5" // Required by the existing receiver protocol.
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/ccfos/nightingale/v6/models"
)

const DefaultSigningKeyFile = "/etc/n9e/secrets/platform10.key"

// FileKeys belongs to a center process. Only references enter channel/task
// configuration. Paths and key bytes never enter dispatches or API responses.
type FileKeys struct{ files map[string]string }

type SigningKeyStatus struct {
	Reference string `json:"reference"`
	Ready     bool   `json:"ready"`
	Reason    string `json:"reason"`
}

func NewFileKeys(overrides map[string]string) *FileKeys {
	files := map[string]string{models.ExternalPushDefaultKeyRef: DefaultSigningKeyFile}
	for ref, path := range overrides {
		files[ref] = path
	}
	return &FileKeys{files: files}
}

func (k *FileKeys) read(ref string) ([]byte, error) {
	if k == nil {
		k = NewFileKeys(nil)
	}
	path, ok := k.files[ref]
	if !ok {
		return nil, errors.New("signing key reference is not configured on center")
	}
	if !filepath.IsAbs(path) {
		return nil, errors.New("signing key file path must be absolute on center")
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil, errors.New("signing key file is missing on center")
	}
	if err != nil {
		return nil, errors.New("signing key file is unreadable on center")
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("signing key must be a regular file")
	}
	// Owner read/write or a controlled read-only group is supported. Never accept
	// world access, execution, or group write. Docker mounts should use 0400/0440.
	if info.Mode().Perm()&0137 != 0 {
		return nil, errors.New("signing key file permissions must restrict access to its owner or read-only group")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("signing key file is unreadable on center")
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil || !actual.Mode().IsRegular() || actual.Mode().Perm()&0137 != 0 {
		return nil, errors.New("signing key file changed or has unsafe permissions")
	}
	value, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil {
		return nil, errors.New("signing key file could not be read completely")
	}
	if len(value) == 0 || len(value) > 65536 || !utf8.Valid(value) || strings.TrimSpace(string(value)) == "" || strings.ContainsAny(string(value), "\r\n\x00") || strings.HasPrefix(string(value), "\ufeff") {
		return nil, errors.New("signing key must be nonempty UTF-8 without BOM, line breaks or NUL, at most 65536 bytes")
	}
	return value, nil
}

func (k *FileKeys) Status(ref string) SigningKeyStatus {
	result := SigningKeyStatus{Reference: ref}
	if _, err := k.read(ref); err != nil {
		result.Reason = err.Error()
		return result
	}
	result.Ready = true
	result.Reason = "signing key file is configured and readable"
	return result
}

func (k *FileKeys) Sign(problemTime, ref string) (string, error) {
	key, err := k.read(ref)
	if err != nil {
		return "", err
	}
	// Delete ordinary ASCII spaces in the timestamp only. Never trim the key.
	// UTF-8 is explicit and confirmed by the integration owner.
	sum := md5.Sum(append([]byte(strings.ReplaceAll(problemTime, " ", "")), key...))
	return strings.ToUpper(hex.EncodeToString(sum[:])), nil
}
