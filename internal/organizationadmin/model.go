package organizationadmin

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidInput         = errors.New("企业参数不正确")
	ErrOrganizationNotFound = errors.New("企业不存在")
)

type Organization struct {
	ID        string
	Code      string
	Name      string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type UpdateInput struct {
	OrganizationID string
	ActorID        string
	Name           string
	RequestID      string
	SourceIP       string
}

type NameUpdate struct {
	OrganizationID string
	ActorID        string
	Name           string
	RequestID      string
	SourceIP       string
}

type Repository interface {
	FindOrganization(ctx context.Context, organizationID string) (Organization, error)
	UpdateOrganization(ctx context.Context, input NameUpdate) (Organization, error)
}
