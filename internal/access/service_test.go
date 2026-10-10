package access

import (
	"context"
	"errors"
	"testing"
)

type repositoryStub struct {
	allowed bool
	err     error
}

func (r repositoryStub) HasPermission(context.Context, string, string) (bool, error) {
	return r.allowed, r.err
}

func TestAllowed(t *testing.T) {
	service := NewService(repositoryStub{allowed: true})
	allowed, err := service.Allowed(context.Background(), "user-1", "iam.user.read")
	if err != nil || !allowed {
		t.Fatalf("Allowed() = %v, %v，期望 true, nil", allowed, err)
	}
}

func TestAllowedRejectsEmptyInput(t *testing.T) {
	service := NewService(repositoryStub{allowed: true})
	allowed, err := service.Allowed(context.Background(), "", "iam.user.read")
	if err != nil || allowed {
		t.Fatalf("Allowed() = %v, %v，期望 false, nil", allowed, err)
	}
}

func TestAllowedWrapsRepositoryError(t *testing.T) {
	service := NewService(repositoryStub{err: errors.New("database unavailable")})
	if _, err := service.Allowed(context.Background(), "user-1", "iam.user.read"); err == nil {
		t.Fatal("仓储错误应向上返回")
	}
}
