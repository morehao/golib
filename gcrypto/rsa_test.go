package gcrypto

import (
	"os"
	"testing"
)

func TestRSA_EncryptDecrypt(t *testing.T) {
	// 生成RSA密钥对
	privateKey, publicKey, err := GenerateRSAKeyPair(2048)
	if err != nil {
		t.Fatalf("GenerateRSAKeyPair failed: %v", err)
	}

	// 转换为PEM格式
	privateKeyPEM := string(PrivateKeyToPEM(privateKey))
	publicKeyPEMBytes, err := PublicKeyToPEM(publicKey)
	if err != nil {
		t.Fatalf("PublicKeyToPEM failed: %v", err)
	}
	publicKeyPEM := string(publicKeyPEMBytes)

	// 创建加密器（使用公钥加密）
	rsaEncryptor, err := NewRSA("", publicKeyPEM)
	if err != nil {
		t.Fatalf("NewRSA failed: %v", err)
	}
	plaintext := "Hello, World! This is a test message for RSA encryption."

	ciphertext, err := rsaEncryptor.Encrypt([]byte(plaintext))
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// 创建解密器（使用私钥解密）
	rsaDecryptor, err := NewRSA(privateKeyPEM, "")
	if err != nil {
		t.Fatalf("NewRSA failed: %v", err)
	}
	decrypted, err := rsaDecryptor.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if string(decrypted) != plaintext {
		t.Fatalf("Decrypted text doesn't match. Expected: %s, Got: %s", plaintext, string(decrypted))
	}
}

func TestRSA_EncryptDecryptString(t *testing.T) {
	privateKey, publicKey, err := GenerateRSAKeyPair(2048)
	if err != nil {
		t.Fatalf("GenerateRSAKeyPair failed: %v", err)
	}

	privateKeyPEM := string(PrivateKeyToPEM(privateKey))
	publicKeyPEMBytes, _ := PublicKeyToPEM(publicKey)
	publicKeyPEM := string(publicKeyPEMBytes)

	rsaEncryptor, err := NewRSA("", publicKeyPEM)
	if err != nil {
		t.Fatalf("NewRSA failed: %v", err)
	}
	plaintext := "测试中文RSA加密解密"

	encrypted, err := rsaEncryptor.EncryptString(plaintext)
	if err != nil {
		t.Fatalf("EncryptString failed: %v", err)
	}

	rsaDecryptor, err := NewRSA(privateKeyPEM, "")
	if err != nil {
		t.Fatalf("NewRSA failed: %v", err)
	}
	decrypted, err := rsaDecryptor.DecryptString(encrypted)
	if err != nil {
		t.Fatalf("DecryptString failed: %v", err)
	}

	if decrypted != plaintext {
		t.Fatalf("Decrypted text doesn't match. Expected: %s, Got: %s", plaintext, decrypted)
	}
}

func TestRSA_NewRSAFromPrivateKey(t *testing.T) {
	privateKey, _, err := GenerateRSAKeyPair(2048)
	if err != nil {
		t.Fatalf("GenerateRSAKeyPair failed: %v", err)
	}

	rsa := NewRSAFromPrivateKey(privateKey)
	if rsa == nil {
		t.Fatal("NewRSAFromPrivateKey returned nil")
	}

	// 应该可以同时加密和解密
	plaintext := "Test message"
	encrypted, err := rsa.EncryptString(plaintext)
	if err != nil {
		t.Fatalf("EncryptString failed: %v", err)
	}

	decrypted, err := rsa.DecryptString(encrypted)
	if err != nil {
		t.Fatalf("DecryptString failed: %v", err)
	}

	if decrypted != plaintext {
		t.Fatalf("Decrypted text doesn't match")
	}
}

func TestRSA_SignVerify(t *testing.T) {
	privateKey, publicKey, err := GenerateRSAKeyPair(2048)
	if err != nil {
		t.Fatalf("GenerateRSAKeyPair failed: %v", err)
	}

	privateKeyPEM := string(PrivateKeyToPEM(privateKey))
	publicKeyPEMBytes, _ := PublicKeyToPEM(publicKey)
	publicKeyPEM := string(publicKeyPEMBytes)

	rsaSigner, err := NewRSA(privateKeyPEM, "")
	if err != nil {
		t.Fatalf("NewRSA failed: %v", err)
	}
	rsaVerifier, err := NewRSA("", publicKeyPEM)
	if err != nil {
		t.Fatalf("NewRSA failed: %v", err)
	}

	data := []byte("This is data to be signed")
	signature, err := rsaSigner.Sign(data)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	err = rsaVerifier.Verify(data, signature)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
}

func TestRSA_PEMConversion(t *testing.T) {
	privateKey, publicKey, err := GenerateRSAKeyPair(2048)
	if err != nil {
		t.Fatalf("GenerateRSAKeyPair failed: %v", err)
	}

	// 转换为PEM格式
	privateKeyPEM := string(PrivateKeyToPEM(privateKey))
	publicKeyPEMBytes, err := PublicKeyToPEM(publicKey)
	if err != nil {
		t.Fatalf("PublicKeyToPEM failed: %v", err)
	}
	publicKeyPEM := string(publicKeyPEMBytes)

	// 从PEM格式恢复
	rsaFromPEM, err := NewRSA(privateKeyPEM, publicKeyPEM)
	if err != nil {
		t.Fatalf("NewRSA failed: %v", err)
	}

	// 测试加密解密
	plaintext := "Test PEM conversion"
	encrypted, err := rsaFromPEM.EncryptString(plaintext)
	if err != nil {
		t.Fatalf("EncryptString failed: %v", err)
	}

	decrypted, err := rsaFromPEM.DecryptString(encrypted)
	if err != nil {
		t.Fatalf("DecryptString failed: %v", err)
	}

	if decrypted != plaintext {
		t.Fatalf("Decrypted text doesn't match")
	}
}

func TestRSA_WithEnvKeys(t *testing.T) {
	privateKey, publicKey, err := GenerateRSAKeyPair(2048)
	if err != nil {
		t.Fatalf("GenerateRSAKeyPair failed: %v", err)
	}

	privateKeyPEM := string(PrivateKeyToPEM(privateKey))
	publicKeyPEMBytes, _ := PublicKeyToPEM(publicKey)
	publicKeyPEM := string(publicKeyPEMBytes)

	// 设置环境变量
	os.Setenv(RSAPrivateKeyEnv, privateKeyPEM)
	os.Setenv(RSAPublicKeyEnv, publicKeyPEM)
	defer func() {
		os.Unsetenv(RSAPrivateKeyEnv)
		os.Unsetenv(RSAPublicKeyEnv)
	}()

	// 使用空字符串，应该从环境变量获取
	rsaEncryptor, err := NewRSA("", "")
	if err != nil {
		t.Fatalf("NewRSA failed: %v", err)
	}

	plaintext := "Test message with env keys"
	encrypted, err := rsaEncryptor.EncryptString(plaintext)
	if err != nil {
		t.Fatalf("EncryptString failed: %v", err)
	}

	rsaDecryptor, err := NewRSA("", "")
	if err != nil {
		t.Fatalf("NewRSA failed: %v", err)
	}

	decrypted, err := rsaDecryptor.DecryptString(encrypted)
	if err != nil {
		t.Fatalf("DecryptString failed: %v", err)
	}

	if decrypted != plaintext {
		t.Fatalf("Decrypted text doesn't match")
	}
}

func TestRSA_LargeData(t *testing.T) {
	privateKey, publicKey, err := GenerateRSAKeyPair(2048)
	if err != nil {
		t.Fatalf("GenerateRSAKeyPair failed: %v", err)
	}

	privateKeyPEM := string(PrivateKeyToPEM(privateKey))
	publicKeyPEMBytes, _ := PublicKeyToPEM(publicKey)
	publicKeyPEM := string(publicKeyPEMBytes)

	rsaEncryptor, err := NewRSA("", publicKeyPEM)
	if err != nil {
		t.Fatalf("NewRSA failed: %v", err)
	}
	rsaDecryptor, err := NewRSA(privateKeyPEM, "")
	if err != nil {
		t.Fatalf("NewRSA failed: %v", err)
	}

	// 测试较大的数据（超过单个RSA块的大小）
	plaintext := make([]byte, 1000)
	for i := range plaintext {
		plaintext[i] = byte(i % 256)
	}

	ciphertext, err := rsaEncryptor.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := rsaDecryptor.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if len(decrypted) != len(plaintext) {
		t.Fatalf("Decrypted length doesn't match. Expected: %d, Got: %d", len(plaintext), len(decrypted))
	}

	for i := range plaintext {
		if decrypted[i] != plaintext[i] {
			t.Fatalf("Decrypted data doesn't match at index %d", i)
		}
	}
}

func TestRSA_InvalidKeySize(t *testing.T) {
	_, _, err := GenerateRSAKeyPair(256)
	if err == nil {
		t.Fatal("Expected error for key size less than 512")
	}
}

func TestRSA_EncryptWithoutPublicKey(t *testing.T) {
	privateKey, _, err := GenerateRSAKeyPair(2048)
	if err != nil {
		t.Fatalf("GenerateRSAKeyPair failed: %v", err)
	}

	privateKeyPEM := string(PrivateKeyToPEM(privateKey))
	rsa, err := NewRSA(privateKeyPEM, "")
	if err != nil {
		t.Fatalf("NewRSA failed: %v", err)
	}
	_, err = rsa.Encrypt([]byte("test"))
	if err == nil {
		t.Fatal("Expected error when encrypting without public key")
	}
}

func TestRSA_DecryptWithoutPrivateKey(t *testing.T) {
	_, publicKey, err := GenerateRSAKeyPair(2048)
	if err != nil {
		t.Fatalf("GenerateRSAKeyPair failed: %v", err)
	}

	publicKeyPEMBytes, _ := PublicKeyToPEM(publicKey)
	publicKeyPEM := string(publicKeyPEMBytes)
	rsa, err := NewRSA("", publicKeyPEM)
	if err != nil {
		t.Fatalf("NewRSA failed: %v", err)
	}
	_, err = rsa.Decrypt([]byte("test"))
	if err == nil {
		t.Fatal("Expected error when decrypting without private key")
	}
}
