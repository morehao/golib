package gcrypto

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"os"
)

// RSA密钥环境变量名
const (
	RSAPrivateKeyEnv = "GOLIB_RSA_PRIVATE_KEY"
	RSAPublicKeyEnv  = "GOLIB_RSA_PUBLIC_KEY"
)

// RSA RSA加密器
type RSA struct {
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
}

// NewRSA 从私钥和公钥创建RSA加密器
// 如果只需要加密，可以只提供公钥；如果只需要解密，可以只提供私钥
// privateKeyPEM: PEM格式的私钥字符串，如果为空则从环境变量 GOLIB_RSA_PRIVATE_KEY 获取
// publicKeyPEM: PEM格式的公钥字符串，如果为空则从环境变量 GOLIB_RSA_PUBLIC_KEY 获取
func NewRSA(privateKeyPEM, publicKeyPEM string) (*RSA, error) {
	var privateKey *rsa.PrivateKey
	var publicKey *rsa.PublicKey
	var err error

	// 处理私钥
	if privateKeyPEM != "" {
		privateKey, err = parsePrivateKeyPEM([]byte(privateKeyPEM))
		if err != nil {
			return nil, err
		}
		publicKey = &privateKey.PublicKey
	} else if envKey := os.Getenv(RSAPrivateKeyEnv); envKey != "" {
		privateKey, err = parsePrivateKeyPEM([]byte(envKey))
		if err != nil {
			return nil, err
		}
		publicKey = &privateKey.PublicKey
	}

	// 处理公钥（如果私钥已经提供了公钥，且没有明确指定公钥参数，则不需要从环境变量获取）
	if publicKeyPEM != "" {
		pubKey, err := parsePublicKeyPEM([]byte(publicKeyPEM))
		if err != nil {
			return nil, err
		}
		publicKey = pubKey
	} else if publicKey == nil {
		// 只有在没有从私钥获取到公钥的情况下，才尝试从环境变量获取
		if envKey := os.Getenv(RSAPublicKeyEnv); envKey != "" {
			pubKey, err := parsePublicKeyPEM([]byte(envKey))
			if err != nil {
				return nil, err
			}
			publicKey = pubKey
		}
	}

	if privateKey == nil && publicKey == nil {
		return nil, errors.New("at least one key must be provided (via parameters or environment variables)")
	}

	return &RSA{
		privateKey: privateKey,
		publicKey:  publicKey,
	}, nil
}

// NewRSAFromPrivateKey 从私钥创建RSA加密器（私钥包含公钥信息）
func NewRSAFromPrivateKey(privateKey *rsa.PrivateKey) *RSA {
	return &RSA{
		privateKey: privateKey,
		publicKey:  &privateKey.PublicKey,
	}
}

// GenerateRSAKeyPair 生成RSA密钥对
// keySize: 密钥长度，建议使用2048或4096位
func GenerateRSAKeyPair(keySize int) (*rsa.PrivateKey, *rsa.PublicKey, error) {
	if keySize < 512 {
		return nil, nil, errors.New("key size must be at least 512 bits")
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, keySize)
	if err != nil {
		return nil, nil, err
	}
	return privateKey, &privateKey.PublicKey, nil
}

// PrivateKeyToPEM 将私钥转换为PEM格式
func PrivateKeyToPEM(privateKey *rsa.PrivateKey) []byte {
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	})
	return privateKeyPEM
}

// PublicKeyToPEM 将公钥转换为PEM格式
func PublicKeyToPEM(publicKey *rsa.PublicKey) ([]byte, error) {
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return nil, err
	}
	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyBytes,
	})
	return publicKeyPEM, nil
}

// Encrypt 使用公钥加密数据（使用OAEP填充）
func (r *RSA) Encrypt(plaintext []byte) ([]byte, error) {
	if r.publicKey == nil {
		return nil, errors.New("public key is required for encryption")
	}
	if len(plaintext) == 0 {
		return nil, errors.New("plaintext is empty")
	}

	// RSA加密有长度限制，需要分块加密
	// 对于OAEP，最大加密块大小 = keySize/8 - 2*hashSize - 2
	// 对于SHA256，hashSize = 32
	maxBlockSize := r.publicKey.Size() - 2*sha256.Size - 2

	var ciphertext []byte
	for len(plaintext) > 0 {
		chunkSize := maxBlockSize
		if len(plaintext) < chunkSize {
			chunkSize = len(plaintext)
		}

		chunk := plaintext[:chunkSize]
		plaintext = plaintext[chunkSize:]

		encryptedChunk, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, r.publicKey, chunk, nil)
		if err != nil {
			return nil, err
		}

		ciphertext = append(ciphertext, encryptedChunk...)
	}

	return ciphertext, nil
}

// Decrypt 使用私钥解密数据
func (r *RSA) Decrypt(ciphertext []byte) ([]byte, error) {
	if r.privateKey == nil {
		return nil, errors.New("private key is required for decryption")
	}

	blockSize := r.privateKey.Size()
	if len(ciphertext) == 0 {
		return nil, errors.New("ciphertext is empty")
	}
	if len(ciphertext)%blockSize != 0 {
		return nil, errors.New("ciphertext length must be a multiple of key size")
	}

	var plaintext []byte
	for len(ciphertext) > 0 {
		chunk := ciphertext[:blockSize]
		ciphertext = ciphertext[blockSize:]

		decryptedChunk, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, r.privateKey, chunk, nil)
		if err != nil {
			return nil, err
		}

		plaintext = append(plaintext, decryptedChunk...)
	}

	return plaintext, nil
}

// EncryptString 加密字符串，返回base64编码
func (r *RSA) EncryptString(plaintext string) (string, error) {
	ciphertext, err := r.Encrypt([]byte(plaintext))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptString 解密base64编码的字符串
func (r *RSA) DecryptString(ciphertext string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	plaintext, err := r.Decrypt(data)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// Sign 使用私钥签名数据
func (r *RSA) Sign(data []byte) ([]byte, error) {
	if r.privateKey == nil {
		return nil, errors.New("private key is required for signing")
	}

	hashed := sha256.Sum256(data)
	signature, err := rsa.SignPKCS1v15(rand.Reader, r.privateKey, crypto.SHA256, hashed[:])
	if err != nil {
		return nil, err
	}

	return signature, nil
}

// Verify 使用公钥验证签名
func (r *RSA) Verify(data []byte, signature []byte) error {
	if r.publicKey == nil {
		return errors.New("public key is required for verification")
	}

	hashed := sha256.Sum256(data)
	return rsa.VerifyPKCS1v15(r.publicKey, crypto.SHA256, hashed[:], signature)
}

// parsePrivateKeyPEM 解析PEM格式的私钥
func parsePrivateKeyPEM(privateKeyPEM []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(privateKeyPEM)
	if block == nil {
		return nil, errors.New("failed to parse PEM block")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// 尝试PKCS8格式
		key, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			return nil, err
		}
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("not an RSA private key")
		}
		return rsaKey, nil
	}

	return privateKey, nil
}

// parsePublicKeyPEM 解析PEM格式的公钥
func parsePublicKeyPEM(publicKeyPEM []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(publicKeyPEM)
	if block == nil {
		return nil, errors.New("failed to parse PEM block")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("not an RSA public key")
	}

	return rsaPub, nil
}
