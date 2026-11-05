package modelprovider

import (
	"net"
	"testing"
)

func TestUnsafeIP(t *testing.T) {
	tests := []struct {
		raw    string
		unsafe bool
	}{
		{"127.0.0.1", true}, {"10.0.0.1", true}, {"169.254.169.254", true}, {"8.8.8.8", false},
		{"100.64.0.0", true}, {"100.100.100.200", true}, {"100.127.255.255", true}, {"100.128.0.0", false},
	}
	for _, test := range tests {
		if actual := unsafeIP(net.ParseIP(test.raw)); actual != test.unsafe {
			t.Fatalf("unsafeIP(%s) = %v，期望 %v", test.raw, actual, test.unsafe)
		}
	}
}
