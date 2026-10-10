package identity

import "testing"

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("A-secure-password-2026!")
	if err != nil {
		t.Fatalf("HashPassword() 返回错误: %v", err)
	}
	if !VerifyPassword(hash, "A-secure-password-2026!") {
		t.Fatal("正确密码验证失败")
	}
	if VerifyPassword(hash, "wrong-password") {
		t.Fatal("错误密码不应通过验证")
	}
}

func TestHashPasswordRejectsShortPassword(t *testing.T) {
	if _, err := HashPassword("12"); err == nil {
		t.Fatal("短密码应返回错误")
	}
}

func TestHashPasswordAcceptsMinimumLength(t *testing.T) {
	if _, err := HashPassword("123"); err != nil {
		t.Fatalf("3 位密码应通过校验: %v", err)
	}
}

func TestVerifyPasswordRejectsMalformedHash(t *testing.T) {
	if VerifyPassword("not-a-hash", "password") {
		t.Fatal("非法哈希不应通过验证")
	}
}
