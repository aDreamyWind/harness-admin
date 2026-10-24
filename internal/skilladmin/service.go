package skilladmin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrInvalidInput = errors.New("Skill 参数不正确")
var ErrNotFound = errors.New("Skill 不存在")
var ErrForbidden = errors.New("无权读取此 Skill")
var ErrConflict = errors.New("Skill 状态冲突")
var ErrRepositorySource = errors.New("GitHub 仓库读取失败")

type Actor struct {
	OrganizationID string
	UserID         string
	RequestID      string
	SourceIP       string
}

type Asset struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Description        string    `json:"description"`
	Status             string    `json:"status"`
	PublishedVersionID *string   `json:"publishedVersionId"`
	LatestVersionNo    int       `json:"latestVersionNo"`
	DepartmentIDs      []string  `json:"departmentIds"`
	UserIDs            []string  `json:"userIds"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

type Version struct {
	ID             string          `json:"id"`
	SkillID        string          `json:"skillId"`
	VersionNo      int             `json:"versionNo"`
	Frontmatter    json.RawMessage `json:"frontmatter"`
	ManifestDigest string          `json:"manifestDigest"`
	SourceType     string          `json:"sourceType"`
	SourceRef      string          `json:"sourceRef"`
	SourceRevision string          `json:"sourceRevision"`
	SourcePath     string          `json:"sourcePath"`
	Files          []File          `json:"files"`
	CreatedAt      time.Time       `json:"createdAt"`
}

type Published struct {
	Asset   Asset   `json:"asset"`
	Version Version `json:"version"`
}

type Repository interface {
	ImportVersion(context.Context, Actor, Package) (Asset, error)
	List(context.Context, string) ([]Asset, error)
	Versions(context.Context, string, string) ([]Version, error)
	ReadVersionFile(context.Context, string, string, string, string) (File, error)
	Publish(context.Context, Actor, string, string, string) (Asset, error)
	ReplaceGrants(context.Context, Actor, string, []string, []string) (Asset, error)
	ListForUser(context.Context, string, string) ([]Published, error)
	GetForUser(context.Context, string, string, string) (Published, error)
	ReadFile(context.Context, string, string, string, string) (File, error)
}

type Service struct {
	repository Repository
	source     RepositorySource
}

func NewService(repository Repository, source RepositorySource) *Service {
	return &Service{repository: repository, source: source}
}

type GitHubImportInput struct {
	RepositoryURL string `json:"repositoryUrl"`
	Ref           string `json:"ref"`
	SkillPath     string `json:"skillPath"`
}

type GitHubImportResult struct {
	Asset    Asset  `json:"asset"`
	Revision string `json:"revision"`
}

func (s *Service) ImportGitHub(ctx context.Context, actor Actor, input GitHubImportInput) (GitHubImportResult, error) {
	if !validActor(actor) || !validGitHubRef(input.Ref) ||
		(input.SkillPath != "" && (input.SkillPath == "." || !validSkillPath(input.SkillPath))) {
		return GitHubImportResult{}, ErrInvalidInput
	}
	normalized, repository, err := parseGitHubRepository(input.RepositoryURL)
	if err != nil {
		return GitHubImportResult{}, err
	}
	if s.source == nil {
		return GitHubImportResult{}, ErrRepositorySource
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	revision, err := s.source.Resolve(ctx, "github", repository, input.Ref)
	if err != nil {
		return GitHubImportResult{}, fmt.Errorf("%w: %v", ErrRepositorySource, err)
	}
	if !githubCommit.MatchString(revision) {
		return GitHubImportResult{}, ErrRepositorySource
	}
	revision = strings.ToLower(revision)
	archive, err := s.source.Fetch(ctx, "github", repository, revision)
	if err != nil {
		return GitHubImportResult{}, fmt.Errorf("%w: %v", ErrRepositorySource, err)
	}
	pack, err := ParseRepositoryArchive(archive, input.SkillPath)
	if err != nil {
		return GitHubImportResult{}, err
	}
	pack.SourceType = "github"
	pack.SourceRef = normalized
	pack.SourceRevision = revision
	pack.SourcePath = input.SkillPath
	asset, err := s.repository.ImportVersion(ctx, actor, pack)
	if err != nil {
		return GitHubImportResult{}, err
	}
	return GitHubImportResult{Asset: asset, Revision: revision}, nil
}

func (s *Service) Import(ctx context.Context, actor Actor, archive []byte) (Asset, error) {
	if !validActor(actor) {
		return Asset{}, ErrInvalidInput
	}
	parsed, err := ParsePackage(archive)
	if err != nil {
		return Asset{}, err
	}
	return s.repository.ImportVersion(ctx, actor, parsed)
}

func (s *Service) List(ctx context.Context, organizationID string) ([]Asset, error) {
	if _, err := uuid.Parse(organizationID); err != nil {
		return nil, ErrInvalidInput
	}
	return s.repository.List(ctx, organizationID)
}

func (s *Service) Versions(ctx context.Context, organizationID, skillID string) ([]Version, error) {
	if _, err := uuid.Parse(organizationID); err != nil {
		return nil, ErrInvalidInput
	}
	if _, err := uuid.Parse(skillID); err != nil {
		return nil, ErrInvalidInput
	}
	return s.repository.Versions(ctx, organizationID, skillID)
}

func (s *Service) ReadVersionFile(ctx context.Context, organizationID, skillID, versionID, filePath string) (File, error) {
	if !validUUID(organizationID) || !validUUID(skillID) || !validUUID(versionID) || !validSkillPath(filePath) {
		return File{}, ErrInvalidInput
	}
	file, err := s.repository.ReadVersionFile(ctx, organizationID, skillID, versionID, filePath)
	if err != nil {
		return File{}, err
	}
	if !validStoredFile(file) {
		return File{}, errors.New("Skill 文件摘要校验失败")
	}
	return file, nil
}

func (s *Service) Publish(ctx context.Context, actor Actor, skillID, versionID, status string) (Asset, error) {
	if !validActor(actor) || !validUUID(skillID) || (status != "published" && status != "disabled") ||
		(status == "published" && !validUUID(versionID)) {
		return Asset{}, ErrInvalidInput
	}
	return s.repository.Publish(ctx, actor, skillID, versionID, status)
}

func (s *Service) ReplaceGrants(ctx context.Context, actor Actor, skillID string, departmentIDs, userIDs []string) (Asset, error) {
	if !validActor(actor) || !validUUID(skillID) || len(departmentIDs)+len(userIDs) > 500 {
		return Asset{}, ErrInvalidInput
	}
	for _, id := range append(append([]string{}, departmentIDs...), userIDs...) {
		if !validUUID(id) {
			return Asset{}, ErrInvalidInput
		}
	}
	return s.repository.ReplaceGrants(ctx, actor, skillID, departmentIDs, userIDs)
}

func (s *Service) ListForUser(ctx context.Context, organizationID, userID string) ([]Published, error) {
	if !validUUID(organizationID) || !validUUID(userID) {
		return nil, ErrInvalidInput
	}
	return s.repository.ListForUser(ctx, organizationID, userID)
}

func (s *Service) GetForUser(ctx context.Context, organizationID, userID, name string) (Published, error) {
	if !validUUID(organizationID) || !validUUID(userID) || !skillName.MatchString(name) {
		return Published{}, ErrInvalidInput
	}
	return s.repository.GetForUser(ctx, organizationID, userID, name)
}

func (s *Service) ReadFile(ctx context.Context, organizationID, userID, name, filePath string) (File, error) {
	if !validUUID(organizationID) || !validUUID(userID) || !skillName.MatchString(name) || !validSkillPath(filePath) {
		return File{}, ErrInvalidInput
	}
	file, err := s.repository.ReadFile(ctx, organizationID, userID, name, filePath)
	if err != nil {
		return File{}, err
	}
	if !validStoredFile(file) {
		return File{}, errors.New("Skill 文件摘要校验失败")
	}
	return file, nil
}

func validStoredFile(file File) bool {
	return file.Size == len(file.Content) && file.Digest == fmt.Sprintf("sha256:%x", sha256.Sum256(file.Content))
}

func validActor(actor Actor) bool { return validUUID(actor.OrganizationID) && validUUID(actor.UserID) }
func validUUID(value string) bool { _, err := uuid.Parse(value); return err == nil }
