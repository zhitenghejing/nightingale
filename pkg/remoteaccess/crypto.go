// Package remoteaccess owns encrypted operations credentials and device SSH
// bindings. It never returns credentials to a UI or language model.
package remoteaccess

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type Keyring struct {
	Active string            `json:"active"`
	Keys   map[string]string `json:"keys"`
}

func LoadKeyring(path string) (*Keyring, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("SSH credential key file unavailable")
	}
	defer f.Close()
	stat, err := f.Stat()
	link, e := os.Lstat(path)
	if err != nil || e != nil || !stat.Mode().IsRegular() || !os.SameFile(stat, link) || stat.Mode().Perm()&0077 != 0 || stat.Size() > 65536 {
		return nil, fmt.Errorf("SSH credential key file must be a private regular file")
	}
	var k Keyring
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&k) != nil || decoder.Decode(new(interface{})) != io.EOF {
		return nil, fmt.Errorf("invalid SSH credential key file")
	}
	for id := range k.Keys {
		if len(id) > 64 {
			return nil, fmt.Errorf("invalid SSH encryption key ID")
		}
		if _, err = k.aead(id); err != nil {
			return nil, err
		}
	}
	if _, err = k.aead(k.Active); err != nil {
		return nil, err
	}
	return &k, nil
}
func (k *Keyring) aead(id string) (cipher.AEAD, error) {
	if k == nil || id == "" {
		return nil, fmt.Errorf("SSH credential encryption key unavailable")
	}
	b, err := base64.StdEncoding.DecodeString(k.Keys[id])
	if err != nil || len(b) != 32 {
		return nil, fmt.Errorf("SSH credential encryption key unavailable")
	}
	c, err := aes.NewCipher(b)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(c)
}
func (k *Keyring) seal(b []byte, aad string) (string, string, error) {
	if k == nil {
		return "", "", fmt.Errorf("SSH credential encryption key unavailable")
	}
	a, err := k.aead(k.Active)
	if err != nil {
		return "", "", err
	}
	n := make([]byte, a.NonceSize())
	if _, err = rand.Read(n); err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(a.Seal(n, n, b, []byte(aad))), k.Active, nil
}
func (k *Keyring) open(b, id, aad string) ([]byte, error) {
	a, err := k.aead(id)
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(b)
	if err != nil || len(raw) < a.NonceSize() {
		return nil, fmt.Errorf("SSH credential decryption failed")
	}
	out, err := a.Open(nil, raw[:a.NonceSize()], raw[a.NonceSize():], []byte(aad))
	if err != nil {
		return nil, fmt.Errorf("SSH credential decryption failed")
	}
	return out, nil
}

// RequestDigest authenticates secret-bearing idempotency inputs without storing
// plaintext or an unkeyed password verifier in the device record.
func (s *Store) RequestDigest(b []byte) (string, error) {
	if s.Keys == nil {
		return "", fmt.Errorf("SSH credential encryption key unavailable")
	}
	key, e := base64.StdEncoding.DecodeString(s.Keys.Keys[s.Keys.Active])
	if e != nil || len(key) != 32 {
		return "", fmt.Errorf("SSH credential encryption key unavailable")
	}
	h := hmac.New(sha256.New, key)
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil)), nil
}
