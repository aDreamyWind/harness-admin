package skilladmin

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
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

func skillZIP(t *testing.T, files map[string]string, symlink bool) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for name, content := range files {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		if symlink && strings.HasSuffix(name, "/script") {
			header.SetMode(os.ModeSymlink | 0600)
		} else {
			header.SetMode(0600)
		}
		writer, err := archive.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
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

func TestParseZIPKeepsFilesAndIgnoresFinderMetadata(t *testing.T) {
	files := map[string]string{
		"lunch-helper/SKILL.md":           "---\nname: lunch-helper\ndescription: 推荐午餐\n---\n正文\n",
		"lunch-helper/references/menu.md": "菜单\n",
	}
	tarPack, err := ParsePackage(skillArchive(t, files, false))
	if err != nil {
		t.Fatal(err)
	}
	files["__MACOSX/lunch-helper/._SKILL.md"] = "Finder 元数据"
	files["lunch-helper/.DS_Store"] = "Finder 元数据"
	zipPack, err := ParsePackage(skillZIP(t, files, false))
	if err != nil || zipPack.Name != "lunch-helper" || len(zipPack.Files) != 2 ||
		zipPack.ManifestDigest != tarPack.ManifestDigest {
		t.Fatalf("ZIP 与 tar.gz 内容解析不一致: %#v, %v", zipPack, err)
	}
}

func TestParseZIPAcceptsGitHubBranchWrapper(t *testing.T) {
	frontmatter := "---\nname: amap-lbs-skill\ndescription: 高德地图综合服务\n---\n正文\n"
	pack, err := ParsePackage(skillZIP(t, map[string]string{
		"amap-lbs-skill-main/SKILL.md":          frontmatter,
		"amap-lbs-skill-main/README.md":         "项目说明",
		"amap-lbs-skill-main/scripts/search.js": "脚本",
	}, false))
	if err != nil || pack.Name != "amap-lbs-skill" || len(pack.Files) != 3 || pack.Files[0].Path == "amap-lbs-skill-main/SKILL.md" {
		t.Fatalf("GitHub 下载 ZIP 的顶层目录不应成为 Skill 名称或文件前缀: %#v, %v", pack, err)
	}
}

func TestParseZIPSelectsUniqueNestedSkill(t *testing.T) {
	frontmatter := "---\nname: lunch-helper\ndescription: 推荐午餐\n---\n正文\n"
	pack, err := ParsePackage(skillZIP(t, map[string]string{
		"repository-main/README.md":                              "仓库说明",
		"repository-main/skills/lunch-helper/SKILL.md":           frontmatter,
		"repository-main/skills/lunch-helper/references/menu.md": "菜单",
	}, false))
	if err != nil || len(pack.Files) != 2 || pack.Name != "lunch-helper" {
		t.Fatalf("未选中唯一的嵌套 Skill: %#v, %v", pack, err)
	}
	_, err = ParsePackage(skillZIP(t, map[string]string{
		"repository-main/skills/one/SKILL.md": frontmatter,
		"repository-main/skills/two/SKILL.md": frontmatter,
	}, false))
	if !errors.Is(err, ErrMultipleSkillManifests) {
		t.Fatalf("多个 Skill 应要求明确选择: %v", err)
	}
}

func TestParseZIPRejectsTraversalSymlinkAndOversize(t *testing.T) {
	frontmatter := "---\nname: lunch-helper\ndescription: 推荐午餐\n---\n正文\n"
	for _, data := range [][]byte{
		skillZIP(t, map[string]string{"lunch-helper/SKILL.md": frontmatter, "../outside": "x"}, false),
		skillZIP(t, map[string]string{"lunch-helper/SKILL.md": frontmatter, "lunch-helper/script": "../../etc/passwd"}, true),
		skillZIP(t, map[string]string{"SKILL.md": frontmatter, "large.txt": strings.Repeat("x", maxPackageBytes)}, false),
	} {
		if _, err := ParsePackage(data); err == nil {
			t.Fatal("非法 ZIP 被接受")
		}
	}
}
