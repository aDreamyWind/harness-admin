package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

type Cipher struct {
	aead cipher.AEAD
}

func NewCipher(encodedKey string) (*Cipher, error) {
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("MODEL_CREDENTIAL_MASTER_KEY 必须是 Base64 编码的 32 字节密钥")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("创建凭据加密器失败: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("创建凭据加密模式失败: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

func (c *Cipher) Seal(plaintext string) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("生成凭据随机数失败: %w", err)
	}
	return c.aead.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

func (c *Cipher) Open(ciphertext []byte) (string, error) {
	if len(ciphertext) < c.aead.NonceSize() {
		return "", fmt.Errorf("加密凭据格式不正确")
	}
	nonce := ciphertext[:c.aead.NonceSize()]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext[c.aead.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("解密模型凭据失败: %w", err)
	}
	return string(plaintext), nil
}
