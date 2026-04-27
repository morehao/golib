package configkv

import "errors"

var (
	errCryptoNotConfigured             = errors.New("crypto key not configured")
	errUnsupportedValueType            = errors.New("unsupported value type")
	errNoCodecRegistered               = errors.New("no codec registered for value type")
	errGroupAndKeyRequired             = errors.New("group and key are required")
	errInvalidCiphertextFormat         = errors.New("invalid ciphertext format")
	errValueEmpty                      = errors.New("value is empty")
	errValueTypeInvalid                = errors.New("invalid value type")
	errValueRequiredForEncryption      = errors.New("value is required when encryption is enabled")
)