package skilladmin

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
)

func skillArchive(t *testing.T, files map[string]string, symlink bool) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(gz)
	for name, content := range files {
		header := &tar.Header{Name: name, Mode: 0600, Size: int64(len(content))}
		if symlink && (name == "script" || strings.HasSuffix(name, "/script")) {
			header.Typeflag, header.Linkname, header.Size = tar.TypeSymlink, "/etc/passwd", 0
		}
		if err := archive.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Typeflag != tar.TypeSymlink {
			if _, err := archive.Write([]byte(content)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestParsePackageKeepsFilesAndFrontmatter(t *testing.T) {
	data := skillArchive(t, map[string]string{
		"lunch-helper/SKILL.md":           "---\nname: lunch-helper\ndescription: 推荐午餐。需要讨论饮食方案时使用。\nmetadata:\n  owner: kitchen\n---\n给出建议。\n",
		"lunch-helper/references/menu.md": "# 今日菜单\n",
	}, false)
	result, err := ParsePackage(data)
	if err != nil || result.Name != "lunch-helper" || len(result.Files) != 2 || result.ManifestDigest == "" {
		t.Fatalf("合法 Skill 包解析失败: %#v, %v", result, err)
	}
	if string(result.Frontmatter) == "" || result.Files[0].Digest == "" {
		t.Fatal("未保留元数据或摘要")
	}
	for _, file := range result.Files {
		if !validStoredFile(file) {
			t.Fatalf("文件摘要与内容不一致: %s", file.Path)
		}
	}
}

func TestParsePackageRejectsTraversalAndSymlink(t *testing.T) {
	frontmatter := "---\nname: lunch-helper\ndescription: 推荐午餐\n---\n正文\n"
	for _, data := range [][]byte{
		skillArchive(t, map[string]string{"lunch-helper/SKILL.md": frontmatter, "../outside": "x"}, false),
		skillArchive(t, map[string]string{"lunch-helper/SKILL.md": frontmatter, "script": ""}, true),
	} {
		if _, err := ParsePackage(data); err == nil {
			t.Fatal("非法归档被接受")
		}
	}
}
