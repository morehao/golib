package gcrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
)

// AES密钥环境变量名
const (
	AESKeyEnv = "GOLIB_AES_KEY"
)

// 默认AES密钥（32字节 = AES-256）
const defaultAESKey = "golib-default-aes-key-32bytes!!"

// AESKeySize 支持的AES密钥长度
const (
	AES128KeySize = 16 // 128位 = 16字节
	AES192KeySize = 24 // 192位 = 24字节
	AES256KeySize = 32 // 256位 = 32字节
)

// AES  AES加密器
type AES struct {
	key []byte
}

// NewAES 创建AES加密器
// key: 密钥字符串，如果为空则从环境变量 GOLIB_AES_KEY 获取，如果环境变量也不存在则使用默认密钥
// 密钥会被转换为字节，长度必须是16、24或32字节（对应AES-128、AES-192、AES-256）
func NewAES(key string) (*AES, error) {
	// 如果key为空，尝试从环境变量获取，否则使用默认密钥
	if key == "" {
		key = getKeyFromEnvOrDefault(AESKeyEnv, defaultAESKey)
	}

	keyBytes := []byte(key)
	keyLen := len(keyBytes)

	// 如果密钥长度不符合要求，尝试调整
	if keyLen != AES128KeySize && keyLen != AES192KeySize && keyLen != AES256KeySize {
		// 如果密钥长度小于32，填充到32字节（使用0填充）
		if keyLen < AES256KeySize {
			padding := make([]byte, AES256KeySize-keyLen)
			keyBytes = append(keyBytes, padding...)
		} else {
			// 如果密钥长度大于32，截取前32字节（使用AES-256）
			keyBytes = keyBytes[:AES256KeySize]
		}
	}

	return &AES{key: keyBytes}, nil
}

// Encrypt 加密数据（使用GCM模式）
func (a *AES) Encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(a.key)
	if err != nil {
		return nil, err
	}

	// 使用GCM模式
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	// 生成随机nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	// 加密并附加nonce
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt 解密数据
func (a *AES) Decrypt(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(a.key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short: missing nonce")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}

// EncryptString 加密字符串，返回base64编码
func (a *AES) EncryptString(plaintext string) (string, error) {
	ciphertext, err := a.Encrypt([]byte(plaintext))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptString 解密base64编码的字符串
func (a *AES) DecryptString(ciphertext string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	plaintext, err := a.Decrypt(data)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// EncryptCBC 使用CBC模式加密（兼容性更好，但安全性略低于GCM）
func (a *AES) EncryptCBC(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(a.key)
	if err != nil {
		return nil, err
	}

	// PKCS7填充
	plaintext = pkcs7Padding(plaintext, aes.BlockSize)

	// 生成随机IV
	iv := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, err
	}

	// 加密
	mode := cipher.NewCBCEncrypter(block, iv)
	ciphertext := make([]byte, len(plaintext))
	mode.CryptBlocks(ciphertext, plaintext)

	// 将IV附加到密文前面
	result := append(iv, ciphertext...)
	return result, nil
}

// DecryptCBC 使用CBC模式解密
func (a *AES) DecryptCBC(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(a.key)
	if err != nil {
		return nil, err
	}

	if len(ciphertext) < aes.BlockSize {
		return nil, errors.New("ciphertext too short: missing IV")
	}

	// 提取IV
	iv := ciphertext[:aes.BlockSize]
	ciphertext = ciphertext[aes.BlockSize:]

	// 解密
	mode := cipher.NewCBCDecrypter(block, iv)
	plaintext := make([]byte, len(ciphertext))
	mode.CryptBlocks(plaintext, ciphertext)

	// 去除PKCS7填充
	plaintext, err = pkcs7UnPadding(plaintext)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}

// pkcs7Padding PKCS7填充
func pkcs7Padding(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padtext := make([]byte, padding)
	for i := range padtext {
		padtext[i] = byte(padding)
	}
	return append(data, padtext...)
}

// pkcs7UnPadding 去除PKCS7填充
func pkcs7UnPadding(data []byte) ([]byte, error) {
	length := len(data)
	if length == 0 {
		return nil, errors.New("data is empty")
	}
	unpadding := int(data[length-1])
	if unpadding > length {
		return nil, errors.New("invalid padding")
	}
	return data[:(length - unpadding)], nil
}
