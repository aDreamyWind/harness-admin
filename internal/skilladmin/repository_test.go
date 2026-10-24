package skilladmin

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type githubSourceStub struct {
	archive                            []byte
	provider, repository, ref, fetched string
}

func (s *githubSourceStub) Resolve(_ context.Context, provider, repository, ref string) (string, error) {
	s.provider, s.repository, s.ref = provider, repository, ref
	return strings.Repeat("a", 40), nil
}
func (s *githubSourceStub) Fetch(_ context.Context, _, _, revision string) ([]byte, error) {
	s.fetched = revision
	return s.archive, nil
}

type importedSkillStub struct {
	Repository
	pack Package
}

func (r *importedSkillStub) ImportVersion(_ context.Context, _ Actor, pack Package) (Asset, error) {
	r.pack = pack
	return Asset{ID: "asset-1", Name: pack.Name}, nil
}

func TestImportGitHubSelectsSubdirectoryAndPinsRevision(t *testing.T) {
	const skill = "---\nname: lunch-helper\ndescription: 推荐午餐\n---\n正文\n"
	source := &githubSourceStub{archive: skillArchive(t, map[string]string{
		"repo-aaa/README.md":                              "仓库说明",
		"repo-aaa/skills/lunch-helper/SKILL.md":           skill,
		"repo-aaa/skills/lunch-helper/references/menu.md": "菜单",
	}, false)}
	repo := &importedSkillStub{}
	service := NewService(repo, source)
	actor := Actor{OrganizationID: "11111111-1111-1111-1111-111111111111", UserID: "22222222-2222-2222-2222-222222222222"}
	result, err := service.ImportGitHub(context.Background(), actor, GitHubImportInput{
		RepositoryURL: "https://github.com/owner/repo.git", Ref: "main", SkillPath: "skills/lunch-helper",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Asset.Name != "lunch-helper" || result.Revision != strings.Repeat("a", 40) ||
		source.provider != "github" || source.repository != "owner/repo" || source.ref != "main" || source.fetched != result.Revision ||
		repo.pack.SourceType != "github" || repo.pack.SourceRef != "https://github.com/owner/repo" ||
		repo.pack.SourceRevision != result.Revision || repo.pack.SourcePath != "skills/lunch-helper" || len(repo.pack.Files) != 2 {
		t.Fatalf("仓库选择或来源记录错误: result=%#v pack=%#v", result, repo.pack)
	}
}

func TestImportGitHubRejectsUnsafeRepositoryAndArchive(t *testing.T) {
	actor := Actor{OrganizationID: "11111111-1111-1111-1111-111111111111", UserID: "22222222-2222-2222-2222-222222222222"}
	for _, input := range []GitHubImportInput{
		{RepositoryURL: "http://github.com/owner/repo"},
		{RepositoryURL: "https://github.com.evil.example/owner/repo"},
		{RepositoryURL: "https://github.com/owner/repo", Ref: "../main"},
		{RepositoryURL: "https://github.com/owner/repo", SkillPath: "../secret"},
	} {
		_, err := NewService(&importedSkillStub{}, &githubSourceStub{}).ImportGitHub(context.Background(), actor, input)
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("危险来源未拒绝: %#v, %v", input, err)
		}
	}
	source := &githubSourceStub{archive: skillArchive(t, map[string]string{
		"repo-aaa/skills/lunch-helper/SKILL.md": "---\nname: lunch-helper\ndescription: 推荐午餐\n---\n正文\n",
		"repo-aaa/skills/lunch-helper/script":   "",
	}, true)}
	_, err := NewService(&importedSkillStub{}, source).ImportGitHub(context.Background(), actor,
		GitHubImportInput{RepositoryURL: "https://github.com/owner/repo", SkillPath: "skills/lunch-helper"})
	if !errors.Is(err, ErrInvalidPackage) {
		t.Fatalf("符号链接未拒绝: %v", err)
	}
}
