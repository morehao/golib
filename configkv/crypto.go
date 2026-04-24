package configkv

import (
	"strings"

	"github.com/morehao/golib/gcrypto"
)

const (
	encryptedPrefix = "enc:"
)

type aesCrypto struct {
	aes *gcrypto.AES
}

func newAESCrypto() (*aesCrypto, error) {
	aes, err := gcrypto.NewAES("")
	if err != nil {
		return nil, err
	}
	return &aesCrypto{aes: aes}, nil
}

func (c *aesCrypto) Encrypt(plaintext string) (string, error) {
	ciphertext, err := c.aes.EncryptString(plaintext)
	if err != nil {
		return "", err
	}
	return encryptedPrefix + ciphertext, nil
}

func (c *aesCrypto) Decrypt(ciphertext string) (string, error) {
	if !strings.HasPrefix(ciphertext, encryptedPrefix) {
		return "", errInvalidCiphertextFormat
	}
	ct := strings.TrimPrefix(ciphertext, encryptedPrefix)
	return c.aes.DecryptString(ct)
}