package configkv

import (
	"testing"
)

func TestJSONCodec(t *testing.T) {
	codec := JSONCodec{}

	type TestStruct struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	obj := TestStruct{Name: "test", Age: 123}
	data, err := codec.Marshal(obj)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var result TestStruct
	if err := codec.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if result.Name != "test" || result.Age != 123 {
		t.Errorf("expected {test 123}, got %+v", result)
	}

	if codec.Name() != "json" {
		t.Errorf("expected name 'json', got '%s'", codec.Name())
	}
}

func TestAESCrypto(t *testing.T) {
	key := []byte("1234567890123456")
	crypto, err := newAESCrypto(key)
	if err != nil {
		t.Fatalf("failed to create crypto: %v", err)
	}

	plaintext := "hello world"
	ciphertext, err := crypto.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	if ciphertext == plaintext {
		t.Error("ciphertext should differ from plaintext")
	}

	decrypted, err := crypto.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("expected '%s', got '%s'", plaintext, decrypted)
	}
}



