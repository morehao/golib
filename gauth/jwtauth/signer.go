package jwtauth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

// Verifier 描述验签能力，供仅需校验 token 的下游服务使用。
type Verifier interface {
	// alg 返回签名算法名，如 "HS256"、"RS256"、"ES256"，
	// 用于 Parse 时设置合法的算法白名单。
	alg() string
	// keyFunc 是传给 jwt 库的验签密钥回调。
	// 通过算法类型断言防止算法混淆攻击（algorithm confusion attack）。
	keyFunc(token *jwt.Token) (any, error)
}

// Signer 在 Verifier 基础上额外具备签发能力，供持有私钥的一方使用。
type Signer interface {
	Verifier
	// signingKey 返回签发 token 使用的密钥。
	signingKey() any
}

// HS256Signer 使用对称密钥进行 HMAC-SHA256 签名与验签。
type HS256Signer struct {
	key []byte
}

// NewHS256Signer 使用给定的对称密钥构造 HS256 签发与验签实例。
// key 在内部转为 []byte 并做防御性复制，防止调用方后续修改影响内部状态。
func NewHS256Signer(key string) (*HS256Signer, error) {
	if key == "" {
		return nil, ErrInvalidVerifyKey
	}

	k := []byte(key)
	keyCopy := make([]byte, len(k))
	copy(keyCopy, k)

	return &HS256Signer{key: keyCopy}, nil
}

func (s *HS256Signer) alg() string {
	return jwt.SigningMethodHS256.Alg()
}

func (s *HS256Signer) keyFunc(token *jwt.Token) (any, error) {
	if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
		return nil, fmt.Errorf("unexpected signing method: %v", token.Method.Alg())
	}
	return s.key, nil
}

func (s *HS256Signer) signingKey() any {
	return s.key
}

// rsaKeyFunc 承载 RS256 的验签逻辑，被 RS256Signer 与 RS256Verifier 复用。
type rsaKeyFunc struct {
	pub *rsa.PublicKey
}

func (v rsaKeyFunc) alg() string {
	return jwt.SigningMethodRS256.Alg()
}

func (v rsaKeyFunc) keyFunc(token *jwt.Token) (any, error) {
	if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
		return nil, fmt.Errorf("unexpected signing method: %v", token.Method.Alg())
	}
	return v.pub, nil
}

// RS256Verifier 仅使用公钥验签，适合只校验 token 的下游服务。
type RS256Verifier struct {
	rsaKeyFunc
}

// NewRS256Verifier 使用给定的 RSA 公钥构造只验签实例。
func NewRS256Verifier(pub *rsa.PublicKey) (*RS256Verifier, error) {
	if pub == nil {
		return nil, ErrNilRSAPublicKey
	}
	return &RS256Verifier{rsaKeyFunc: rsaKeyFunc{pub: pub}}, nil
}

// RS256Signer 使用私钥签发、公钥验签。
type RS256Signer struct {
	rsaKeyFunc
	priv *rsa.PrivateKey
}

// NewRS256Signer 使用给定的 RSA 私钥与公钥构造签发与验签实例。
// priv 用于签发，pub 用于验签，二者须为同一密钥对。
func NewRS256Signer(priv *rsa.PrivateKey, pub *rsa.PublicKey) (*RS256Signer, error) {
	if priv == nil {
		return nil, ErrNilRSAPrivateKey
	}
	if pub == nil {
		return nil, ErrNilRSAPublicKey
	}
	return &RS256Signer{
		rsaKeyFunc: rsaKeyFunc{pub: pub},
		priv:       priv,
	}, nil
}

func (s *RS256Signer) signingKey() any {
	return s.priv
}

// ecdsaKeyFunc 承载 ES256 的验签逻辑，被 ES256Signer 与 ES256Verifier 复用。
type ecdsaKeyFunc struct {
	pub *ecdsa.PublicKey
}

func (v ecdsaKeyFunc) alg() string {
	return jwt.SigningMethodES256.Alg()
}

func (v ecdsaKeyFunc) keyFunc(token *jwt.Token) (any, error) {
	if _, ok := token.Method.(*jwt.SigningMethodECDSA); !ok {
		return nil, fmt.Errorf("unexpected signing method: %v", token.Method.Alg())
	}
	return v.pub, nil
}

// ES256Verifier 仅使用公钥验签，适合只校验 token 的下游服务。
type ES256Verifier struct {
	ecdsaKeyFunc
}

// NewES256Verifier 使用给定的 ECDSA P-256 公钥构造只验签实例。
func NewES256Verifier(pub *ecdsa.PublicKey) (*ES256Verifier, error) {
	if pub == nil {
		return nil, ErrNilECDSAPublicKey
	}
	if pub.Curve != elliptic.P256() {
		return nil, ErrUnsupportedCurve
	}
	return &ES256Verifier{ecdsaKeyFunc: ecdsaKeyFunc{pub: pub}}, nil
}

// ES256Signer 使用私钥签发、公钥验签。公钥由私钥推导，无需单独传入。
type ES256Signer struct {
	ecdsaKeyFunc
	priv *ecdsa.PrivateKey
}

// NewES256Signer 使用给定的 ECDSA 私钥构造签发与验签实例。
// 仅支持 P-256 曲线；公钥取自私钥，自动用于验签。
func NewES256Signer(priv *ecdsa.PrivateKey) (*ES256Signer, error) {
	if priv == nil {
		return nil, ErrNilECDSAPrivateKey
	}
	if priv.Curve != elliptic.P256() {
		return nil, ErrUnsupportedCurve
	}
	return &ES256Signer{
		ecdsaKeyFunc: ecdsaKeyFunc{pub: &priv.PublicKey},
		priv:         priv,
	}, nil
}

func (s *ES256Signer) signingKey() any {
	return s.priv
}
