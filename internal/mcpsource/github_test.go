package mcpsource

import (
	"net"
	"testing"
)

func TestPublicIPRejectsPrivateAndLinkLocal(t *testing.T) {
	for _, value := range []string{"127.0.0.1", "10.1.2.3", "169.254.169.254", "::1", "fc00::1"} {
		if publicIP(net.ParseIP(value)) {
			t.Fatalf("私有或保留地址被允许: %s", value)
		}
	}
	if !publicIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("公网地址被拒绝")
	}
}
