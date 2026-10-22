package mcpdeploy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

type NodePlan struct {
	EntryFile    string `json:"entryFile"`
	BuildCommand string `json:"buildCommand"`
	HasLockfile  bool   `json:"hasLockfile"`
}

// nodeEntryFromArchive 仅读取仓库根目录的 package.json，不运行仓库提供的命令。
// 源码包允许一个 GitHub/Gitee 自动生成的外层目录。
func nodeEntryFromArchive(archive []byte) (string, error) {
	plan, err := nodePlanFromArchive(archive)
	return plan.EntryFile, err
}

func nodePlanFromArchive(archive []byte) (NodePlan, error) {
	zipped, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return NodePlan{}, errors.New("代码包不是有效的 .tar.gz 文件")
	}
	defer zipped.Close()
	reader := tar.NewReader(zipped)
	var direct, wrapped []byte
	var directLock, wrappedLock bool
	topLevels := map[string]bool{}
	for count := 0; count < 2000; count++ {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return NodePlan{}, errors.New("代码包 tar 内容损坏")
		}
		if header.Typeflag == tar.TypeXGlobalHeader || header.Typeflag == tar.TypeXHeader {
			continue
		}
		name := strings.TrimPrefix(header.Name, "./")
		parts := strings.Split(strings.Trim(name, "/"), "/")
		if len(parts) == 0 || parts[0] == "" {
			continue
		}
		topLevels[parts[0]] = true
		if (header.Typeflag == tar.TypeReg || header.Typeflag == tar.TypeRegA) && parts[len(parts)-1] == "package-lock.json" {
			if len(parts) == 1 {
				directLock = true
			} else if len(parts) == 2 {
				wrappedLock = true
			}
		}
		if (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA) ||
			parts[len(parts)-1] != "package.json" || len(parts) > 2 {
			continue
		}
		if header.Size < 0 || header.Size > 64*1024 {
			return NodePlan{}, errors.New("package.json 超过 64 KiB")
		}
		content, err := io.ReadAll(io.LimitReader(reader, 64*1024+1))
		if err != nil {
			return NodePlan{}, errors.New("读取 package.json 失败")
		}
		if len(parts) == 1 {
			direct = content
		} else {
			wrapped = content
		}
	}
	if len(direct) == 0 && len(topLevels) == 1 {
		direct = wrapped
		directLock = wrappedLock
	}
	if len(direct) == 0 {
		return NodePlan{}, errors.New("未找到根目录 package.json；请在高级设置中指定运行入口文件")
	}
	var manifest struct {
		Main    string            `json:"main"`
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(direct, &manifest); err != nil {
		return NodePlan{}, errors.New("package.json 格式不正确")
	}
	plan := NodePlan{HasLockfile: directLock}
	if strings.TrimSpace(manifest.Scripts["build"]) != "" {
		plan.BuildCommand = "npm run build"
	}
	// start 直接指定的文件比 main 更能反映仓库的实际运行方式。
	if command := strings.Fields(manifest.Scripts["start"]); len(command) >= 2 && command[0] == "node" {
		for _, part := range command[1:] {
			if strings.HasPrefix(part, "-") {
				continue
			}
			candidate := strings.TrimPrefix(part, "./")
			if validEntry(candidate, "node") {
				plan.EntryFile = candidate
				return plan, nil
			}
			break
		}
	}
	if candidate := strings.TrimPrefix(manifest.Main, "./"); validEntry(candidate, "node") {
		plan.EntryFile = candidate
		return plan, nil
	}
	return NodePlan{}, errors.New("package.json 未提供可识别的 Node.js 运行入口；请在高级设置中指定")
}
