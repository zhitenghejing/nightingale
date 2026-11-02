package collection

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
)

// Keys are deployment-owned. Retain old entries when rotating the active key.
type Keyring struct {
	Active string            `json:"active"`
	Keys   map[string]string `json:"keys"`
}

func LoadKeyring(path string) (*Keyring, error) {
	stat, err := os.Stat(path)
	if err != nil || !stat.Mode().IsRegular() || stat.Mode().Perm()&0077 != 0 {
		return nil, errors.New("collection key file must be a private regular file")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("collection key file unavailable")
	}
	var k Keyring
	if json.Unmarshal(b, &k) != nil {
		return nil, errors.New("invalid collection key file")
	}
	if _, err = k.aead(k.Active); err != nil {
		return nil, err
	}
	return &k, nil
}
func (k *Keyring) aead(id string) (cipher.AEAD, error) {
	if k == nil || id == "" {
		return nil, errors.New("collection encryption key unavailable")
	}
	key, err := base64.StdEncoding.DecodeString(k.Keys[id])
	if err != nil || len(key) != 32 {
		return nil, errors.New("collection encryption key unavailable")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func (k *Keyring) Seal(plaintext []byte, context string) (string, string, error) {
	if k == nil {
		return "", "", errors.New("collection encryption key unavailable")
	}
	a, err := k.aead(k.Active)
	if err != nil {
		return "", "", err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(a.Seal(nonce, nonce, plaintext, []byte(context))), k.Active, nil
}
func (k *Keyring) Open(encoded, id, context string) ([]byte, error) {
	a, err := k.aead(id)
	if err != nil {
		return nil, err
	}
	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(b) < a.NonceSize() {
		return nil, errors.New("collection decryption failed")
	}
	b, err = a.Open(nil, b[:a.NonceSize()], b[a.NonceSize():], []byte(context))
	if err != nil {
		return nil, errors.New("collection decryption failed")
	}
	return b, nil
}
func digest(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func newSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
