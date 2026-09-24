package configkv

import "errors"

var (
	errCryptoNotConfigured        = errors.New("crypto key not configured")
	errUnsupportedValueType       = errors.New("unsupported value type")
	errNoCodecRegistered          = errors.New("no codec registered for value type")
	errGroupAndKeyRequired        = errors.New("group and key are required")
	errInvalidCiphertextFormat    = errors.New("invalid ciphertext format")
	errValueEmpty                 = errors.New("value is empty")
	errValueTypeInvalid           = errors.New("invalid value type")
	errValueRequiredForEncryption = errors.New("value is required when encryption is enabled")

	// errDBRequired db 为 nil。
	errDBRequired = errors.New("db is required")
	// errNotInitialized 未调用 Init 就使用包级便捷函数。
	errNotInitialized = errors.New("not initialized, call Init first")
)
