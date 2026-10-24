package skilladmin

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"testing"
)

type exportRepositoryStub struct {
	Repository
	version Version
	files   map[string]File
}

func (r *exportRepositoryStub) Versions(context.Context, string, string) ([]Version, error) {
	return []Version{r.version}, nil
}

func (r *exportRepositoryStub) ReadVersionFile(_ context.Context, _, _, _, path string) (File, error) {
	file, ok := r.files[path]
	if !ok {
		return File{}, ErrNotFound
	}
	return file, nil
}

func testExportRepository() *exportRepositoryStub {
	files := map[string]File{}
	manifest := sha256.New()
	version := Version{ID: "22222222-2222-4222-8222-222222222222", SkillID: "11111111-1111-4111-8111-111111111111",
		VersionNo: 2, Frontmatter: json.RawMessage(`{"name":"lunch-helper"}`)}
	for _, entry := range []struct{ path, content string }{{"SKILL.md", "---\nname: lunch-helper\ndescription: 午餐\n---\n正文"}, {"references/menu.md", "菜单"}} {
		content := []byte(entry.content)
		file := File{Path: entry.path, Content: content, Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(content)), Size: len(content)}
		files[entry.path] = file
		version.Files = append(version.Files, File{Path: file.Path, Digest: file.Digest, Size: file.Size})
		_, _ = fmt.Fprintf(manifest, "%s\x00%s\x00%d\x00", file.Path, file.Digest, file.Size)
	}
	version.ManifestDigest = fmt.Sprintf("sha256:%x", manifest.Sum(nil))
	return &exportRepositoryStub{version: version, files: files}
}

func TestExportVersionZipsOnlySelectedImmutableFiles(t *testing.T) {
	repository := testExportRepository()
	service := NewService(repository, nil)
	archive, err := service.ExportVersion(context.Background(), "33333333-3333-4333-8333-333333333333",
		repository.version.SkillID, repository.version.ID)
	if err != nil {
		t.Fatal(err)
	}
	if archive.Name != "lunch-helper" || archive.VersionNo != 2 {
		t.Fatalf("下载版本信息错误: %#v", archive)
	}
	reader, err := zip.NewReader(bytes.NewReader(archive.Content), int64(len(archive.Content)))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) != len(repository.files) {
		t.Fatalf("ZIP 文件数 = %d", len(reader.File))
	}
	for _, item := range reader.File {
		path := item.Name[len("lunch-helper/"):]
		expected, ok := repository.files[path]
		if !ok || item.Name != "lunch-helper/"+path {
			t.Fatalf("意外的 ZIP 路径: %q", item.Name)
		}
		opened, err := item.Open()
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(opened)
		_ = opened.Close()
		if err != nil || !bytes.Equal(got, expected.Content) {
			t.Fatalf("%s 内容与固定版本不一致: %v", path, err)
		}
	}
}

func TestExportVersionRejectsMissingOrTamperedContent(t *testing.T) {
	repository := testExportRepository()
	service := NewService(repository, nil)
	orgID, skillID := "33333333-3333-4333-8333-333333333333", repository.version.SkillID
	if _, err := service.ExportVersion(context.Background(), orgID, skillID, "44444444-4444-4444-8444-444444444444"); err != ErrNotFound {
		t.Fatalf("不存在的版本应返回 ErrNotFound: %v", err)
	}
	file := repository.files["SKILL.md"]
	file.Content = []byte("被修改的内容")
	repository.files[file.Path] = file
	if _, err := service.ExportVersion(context.Background(), orgID, skillID, repository.version.ID); err == nil {
		t.Fatal("文件被修改后仍生成 ZIP")
	}
}
