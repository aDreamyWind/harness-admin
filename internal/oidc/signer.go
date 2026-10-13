package oidc

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

type TokenSigner interface {
	Sign(map[string]any) (string, error)
	Verify(string) (map[string]any, error)
	JWKS() map[string]any
}

type RS256Signer struct {
	privateKey *rsa.PrivateKey
	kid        string
}

func NewRS256Signer(privateKeyPEM string) (*RS256Signer, error) {
	var privateKey *rsa.PrivateKey
	if strings.TrimSpace(privateKeyPEM) == "" {
		generated, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, fmt.Errorf("生成 OIDC RSA 签名密钥失败: %w", err)
		}
		privateKey = generated
	} else {
		block, _ := pem.Decode([]byte(privateKeyPEM))
		if block == nil {
			return nil, errors.New("OIDC_SIGNING_PRIVATE_KEY 不是有效的 PEM")
		}
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			if pkcs1, pkcs1Err := x509.ParsePKCS1PrivateKey(block.Bytes); pkcs1Err == nil {
				privateKey = pkcs1
			} else {
				return nil, fmt.Errorf("解析 OIDC RSA 签名密钥失败: %w", err)
			}
		} else {
			var ok bool
			privateKey, ok = parsed.(*rsa.PrivateKey)
			if !ok {
				return nil, errors.New("OIDC_SIGNING_PRIVATE_KEY 必须是 RSA 私钥")
			}
		}
	}
	if privateKey.N.BitLen() < 2048 {
		return nil, errors.New("OIDC RSA 签名密钥不得少于 2048 位")
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("编码 OIDC 公钥失败: %w", err)
	}
	digest := sha256.Sum256(publicDER)
	return &RS256Signer{privateKey: privateKey, kid: base64.RawURLEncoding.EncodeToString(digest[:12])}, nil
}

func (s *RS256Signer) Sign(claims map[string]any) (string, error) {
	header, err := json.Marshal(map[string]any{"alg": "RS256", "kid": s.kid, "typ": "JWT"})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	unsigned := encodeSegment(header) + "." + encodeSegment(payload)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, s.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (s *RS256Signer) Verify(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrInvalidToken
	}
	var header map[string]any
	if json.Unmarshal(headerBytes, &header) != nil || header["alg"] != "RS256" || header["kid"] != s.kid {
		return nil, ErrInvalidToken
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, ErrInvalidToken
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(&s.privateKey.PublicKey, crypto.SHA256, digest[:], signature) != nil {
		return nil, ErrInvalidToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidToken
	}
	claims := make(map[string]any)
	if json.Unmarshal(payload, &claims) != nil {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

func (s *RS256Signer) JWKS() map[string]any {
	exponent := big.NewInt(int64(s.privateKey.PublicKey.E)).Bytes()
	return map[string]any{"keys": []map[string]any{{
		"kty": "RSA", "use": "sig", "alg": "RS256", "kid": s.kid,
		"n": base64.RawURLEncoding.EncodeToString(s.privateKey.PublicKey.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(exponent),
	}}}
}

func encodeSegment(value []byte) string {
	return base64.RawURLEncoding.EncodeToString(value)
}
