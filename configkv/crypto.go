package configkv

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const (
	encryptedPrefix = "enc:"
)

var (
	errInvalidKeySize    = errors.New("invalid key size: must be 16, 24, or 32 bytes")
	errCiphertextTooShort = errors.New("ciphertext too short")
	errInvalidPrefix     = errors.New("invalid ciphertext format")
)

type Crypto interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(ciphertext string) (string, error)
}

type aesCrypto struct {
	key []byte
}

func NewAESCrypto(key []byte) (Crypto, error) {
	keyLen := len(key)
	if keyLen != 16 && keyLen != 24 && keyLen != 32 {
		return nil, errInvalidKeySize
	}
	return &aesCrypto{key: key}, nil
}

func (c *aesCrypto) Encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return encryptedPrefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (c *aesCrypto) Decrypt(ciphertext string) (string, error) {
	if !strings.HasPrefix(ciphertext, encryptedPrefix) {
		return "", errInvalidPrefix
	}

	ct := strings.TrimPrefix(ciphertext, encryptedPrefix)
	data, err := base64.StdEncoding.DecodeString(ct)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errCiphertextTooShort
	}

	nonce, data := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}