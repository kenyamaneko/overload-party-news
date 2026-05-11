// Package internalauth は内部サービス間認証 JWT (HS256) の検証を提供する adapter。
package internalauth

import (
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v4"

	"github.com/kenyamaneko/overload-party-news/internal/port"
)

// 本 adapter が port.InternalAuthVerifier を満たすことを compile-time に保証する。
var _ port.InternalAuthVerifier = (*Verifier)(nil)

// ExpectedIssuer は受理する iss クレーム値。
const ExpectedIssuer = "overload-party-gateway"

// DefaultKeyID は HMAC 鍵を識別する初期 key ID。
const DefaultKeyID KeyID = "v1"

// KeyID は HMAC 鍵を識別する文字列。
type KeyID string

// KeyResolver は kid に対応する HMAC 鍵を返す。
type KeyResolver func(kid KeyID) ([]byte, error)

// StaticHS256Resolver は単一鍵だけを返す KeyResolver を構築する。
func StaticHS256Resolver(secret []byte, keyID KeyID) KeyResolver {
	return func(kid KeyID) ([]byte, error) {
		if kid != keyID {
			return nil, fmt.Errorf("internalauth: unknown key id %q", kid)
		}
		return secret, nil
	}
}

// Verifier は HS256 で内部認証 JWT を検証し sub クレームを返す。
type Verifier struct {
	resolver KeyResolver
	issuer   string
}

// VerifierOption は Verifier の任意フィールドを上書きする。
type VerifierOption func(*Verifier)

// WithExpectedIssuer は受理する iss を上書きする。
func WithExpectedIssuer(issuer string) VerifierOption {
	return func(v *Verifier) { v.issuer = issuer }
}

// NewVerifier は KeyResolver から Verifier を生成する。
func NewVerifier(resolver KeyResolver, opts ...VerifierOption) *Verifier {
	v := &Verifier{
		resolver: resolver,
		issuer:   ExpectedIssuer,
	}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

// Verify は signed token を検証し sub (= playerID) を返す。
// 署名 / 有効期限 / iss / alg=HS256 / kid 解決のいずれかが失敗すれば error を返す。
func (v *Verifier) Verify(token string) (string, error) {
	if token == "" {
		return "", errors.New("internalauth: token is empty")
	}
	claims := &jwt.RegisteredClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("internalauth: unexpected signing method %q", t.Header["alg"])
		}
		rawKID, ok := t.Header["kid"].(string)
		if !ok || rawKID == "" {
			return nil, errors.New("internalauth: kid header missing")
		}
		secret, err := v.resolver(KeyID(rawKID))
		if err != nil {
			return nil, err
		}
		return secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		return "", fmt.Errorf("internalauth: parse: %w", err)
	}
	if !parsed.Valid {
		return "", errors.New("internalauth: token invalid")
	}
	if claims.Issuer != v.issuer {
		return "", fmt.Errorf("internalauth: unexpected issuer %q", claims.Issuer)
	}
	if claims.Subject == "" {
		return "", errors.New("internalauth: sub is empty")
	}
	return claims.Subject, nil
}
