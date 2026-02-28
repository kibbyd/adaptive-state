package cipher

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// #region config
var (
	WorkspaceDir = `C:\adaptive_state\orac_workspace`
	KeyFile      = filepath.Join(WorkspaceDir, ".cipher_key")
	InboxDir     = filepath.Join(WorkspaceDir, "inbox")
)

// #endregion config

// #region key
func ensureKey() ([]byte, error) {
	os.MkdirAll(WorkspaceDir, 0755)
	data, err := os.ReadFile(KeyFile)
	if err == nil && len(data) >= 32 {
		return data[:32], nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("keygen: %w", err)
	}
	if err := os.WriteFile(KeyFile, key, 0600); err != nil {
		return nil, fmt.Errorf("write key: %w", err)
	}
	return key, nil
}

// #endregion key

// #region keystream
func keystream(key []byte, length int) []byte {
	stream := make([]byte, 0, length+32)
	counter := uint64(0)
	for len(stream) < length {
		buf := make([]byte, len(key)+8)
		copy(buf, key)
		binary.BigEndian.PutUint64(buf[len(key):], counter)
		h := sha256.Sum256(buf)
		stream = append(stream, h[:]...)
		counter++
	}
	return stream[:length]
}

// #endregion keystream

// #region encrypt-decrypt
func Encrypt(plaintext string) (string, error) {
	key, err := ensureKey()
	if err != nil {
		return "", err
	}
	data := []byte(plaintext)
	ks := keystream(key, len(data))
	cipher := make([]byte, len(data))
	for i := range data {
		cipher[i] = data[i] ^ ks[i]
	}
	return base64.StdEncoding.EncodeToString(cipher), nil
}

func Decrypt(b64Ciphertext string) (string, error) {
	key, err := ensureKey()
	if err != nil {
		return "", err
	}
	cipher, err := base64.StdEncoding.DecodeString(b64Ciphertext)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	ks := keystream(key, len(cipher))
	plain := make([]byte, len(cipher))
	for i := range cipher {
		plain[i] = cipher[i] ^ ks[i]
	}
	return string(plain), nil
}

// #endregion encrypt-decrypt

// #region inbox
// ReadInbox reads and decrypts from_commander.enc. Returns "" if no message.
func ReadInbox() (string, error) {
	path := filepath.Join(InboxDir, "from_commander.enc")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return "", nil
	}
	return Decrypt(text)
}

// WriteOutbox encrypts and writes to to_commander.enc.
func WriteOutbox(plaintext string) error {
	os.MkdirAll(InboxDir, 0755)
	encrypted, err := Encrypt(plaintext)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(InboxDir, "to_commander.enc"), []byte(encrypted), 0644)
}

// WriteOutboxRaw writes pre-encrypted content to to_commander.enc.
func WriteOutboxRaw(encrypted string) error {
	os.MkdirAll(InboxDir, 0755)
	return os.WriteFile(filepath.Join(InboxDir, "to_commander.enc"), []byte(encrypted), 0644)
}

// ClearInbox removes from_commander.enc after reading so the same message isn't re-read.
func ClearInbox() {
	os.Remove(filepath.Join(InboxDir, "from_commander.enc"))
}

// #endregion inbox
