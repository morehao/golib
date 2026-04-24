package configkv

import "errors"

var (
	errCryptoNotConfigured             = errors.New("crypto key not configured")
	errUnsupportedValueType            = errors.New("unsupported value type")
	errNoCodecRegistered               = errors.New("no codec registered for value type")
	errGroupAndKeyRequired             = errors.New("group and key are required")
	errSecretStringRequiresStringValue = errors.New("secret_string requires string value")
	errInvalidCiphertextFormat         = errors.New("invalid ciphertext format")
)