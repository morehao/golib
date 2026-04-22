package configkv

import (
	"testing"
	"time"
)

func TestValueString(t *testing.T) {
	v := stringValue("hello")
	if v.String() != "hello" {
		t.Errorf("expected 'hello', got '%s'", v.String())
	}
}

func TestValueInt(t *testing.T) {
	v := stringValue("123")
	if v.Int() != 123 {
		t.Errorf("expected 123, got %d", v.Int())
	}
}

func TestValueInt64(t *testing.T) {
	v := stringValue("9223372036854775807")
	if v.Int64() != 9223372036854775807 {
		t.Errorf("expected 9223372036854775807, got %d", v.Int64())
	}
}

func TestValueFloat64(t *testing.T) {
	v := stringValue("3.14")
	if v.Float64() != 3.14 {
		t.Errorf("expected 3.14, got %f", v.Float64())
	}
}

func TestValueBool(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"true", true},
		{"false", false},
		{"1", true},
		{"0", false},
	}
	for _, tt := range tests {
		v := stringValue(tt.input)
		if v.Bool() != tt.expected {
			t.Errorf("input '%s': expected %v, got %v", tt.input, tt.expected, v.Bool())
		}
	}
}

func TestValueDuration(t *testing.T) {
	v := stringValue("30s")
	if v.Duration() != 30*time.Second {
		t.Errorf("expected 30s, got %v", v.Duration())
	}

	v2 := stringValue("1h30m")
	if v2.Duration() != 1*time.Hour+30*time.Minute {
		t.Errorf("expected 1h30m, got %v", v2.Duration())
	}
}

func TestValueIsZero(t *testing.T) {
	v := stringValue("")
	if !v.IsZero() {
		t.Error("expected empty string to be zero")
	}

	v2 := stringValue("hello")
	if v2.IsZero() {
		t.Error("expected 'hello' to not be zero")
	}
}

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
	crypto, err := NewAESCrypto(key)
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

func TestCache(t *testing.T) {
	cache := NewCache()

	cache.Set("key1", []byte("value1"), time.Second)

	val, ok := cache.Get("key1")
	if !ok {
		t.Error("expected to get value from cache")
	}
	if string(val) != "value1" {
		t.Errorf("expected 'value1', got '%s'", string(val))
	}

	cache.Delete("key1")
	_, ok = cache.Get("key1")
	if ok {
		t.Error("expected key to be deleted")
	}

	cache.Set("key2", []byte("value2"), time.Millisecond)
	time.Sleep(10 * time.Millisecond)
	_, ok = cache.Get("key2")
	if ok {
		t.Error("expected expired key to be deleted")
	}
}

