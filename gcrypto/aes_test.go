package gcrypto

import (
	"os"
	"testing"
)

func TestAES_EncryptDecrypt(t *testing.T) {
	// 使用默认密钥
	aes, err := NewAES("")
	if err != nil {
		t.Fatalf("NewAES failed: %v", err)
	}

	plaintext := "Hello, World! This is a test message."
	ciphertext, err := aes.Encrypt([]byte(plaintext))
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := aes.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if string(decrypted) != plaintext {
		t.Fatalf("Decrypted text doesn't match. Expected: %s, Got: %s", plaintext, string(decrypted))
	}
}

func TestAES_EncryptDecryptString(t *testing.T) {
	// 使用自定义密钥
	keyStr := "my-secret-key-1234567890123456" // 32 bytes
	aes, err := NewAES(keyStr)
	if err != nil {
		t.Fatalf("NewAES failed: %v", err)
	}

	plaintext := "测试中文加密解密"
	encrypted, err := aes.EncryptString(plaintext)
	if err != nil {
		t.Fatalf("EncryptString failed: %v", err)
	}

	decrypted, err := aes.DecryptString(encrypted)
	if err != nil {
		t.Fatalf("DecryptString failed: %v", err)
	}

	if decrypted != plaintext {
		t.Fatalf("Decrypted text doesn't match. Expected: %s, Got: %s", plaintext, decrypted)
	}
}

func TestAES_EncryptDecryptCBC(t *testing.T) {
	keyStr := "my-secret-key-1234567890123456" // 32 bytes
	aes, err := NewAES(keyStr)
	if err != nil {
		t.Fatalf("NewAES failed: %v", err)
	}

	plaintext := []byte("This is a test message for CBC mode encryption.")
	ciphertext, err := aes.EncryptCBC(plaintext)
	if err != nil {
		t.Fatalf("EncryptCBC failed: %v", err)
	}

	decrypted, err := aes.DecryptCBC(ciphertext)
	if err != nil {
		t.Fatalf("DecryptCBC failed: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Fatalf("Decrypted text doesn't match. Expected: %s, Got: %s", string(plaintext), string(decrypted))
	}
}

func TestAES_WithEnvKey(t *testing.T) {
	// 设置环境变量
	testKey := "test-env-key-1234567890123456"
	os.Setenv(AESKeyEnv, testKey)
	defer os.Unsetenv(AESKeyEnv)

	// 使用空字符串，应该从环境变量获取
	aes, err := NewAES("")
	if err != nil {
		t.Fatalf("NewAES failed: %v", err)
	}

	plaintext := "Test message with env key"
	encrypted, err := aes.EncryptString(plaintext)
	if err != nil {
		t.Fatalf("EncryptString failed: %v", err)
	}

	// 使用相同的环境变量密钥解密
	aes2, err := NewAES("")
	if err != nil {
		t.Fatalf("NewAES failed: %v", err)
	}

	decrypted, err := aes2.DecryptString(encrypted)
	if err != nil {
		t.Fatalf("DecryptString failed: %v", err)
	}

	if decrypted != plaintext {
		t.Fatalf("Decrypted text doesn't match")
	}
}

func TestAES_ShortKey(t *testing.T) {
	// 测试短密钥（会自动填充）
	shortKey := "short-key"
	aes, err := NewAES(shortKey)
	if err != nil {
		t.Fatalf("NewAES failed: %v", err)
	}

	plaintext := "Test message"
	encrypted, err := aes.EncryptString(plaintext)
	if err != nil {
		t.Fatalf("EncryptString failed: %v", err)
	}

	decrypted, err := aes.DecryptString(encrypted)
	if err != nil {
		t.Fatalf("DecryptString failed: %v", err)
	}

	if decrypted != plaintext {
		t.Fatalf("Decrypted text doesn't match")
	}
}
