package secret

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestCipherRoundTrip(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	cipher, err := NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := cipher.Seal("sensitive-api-key")
	if err != nil {
		t.Fatal(err)
	}
	opened, err := cipher.Open(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if opened != "sensitive-api-key" {
		t.Fatalf("解密结果不正确: %q", opened)
	}
}

func TestCipherRejectsInvalidKey(t *testing.T) {
	if _, err := NewCipher("short"); err == nil {
		t.Fatal("非法密钥应返回错误")
	}
}
