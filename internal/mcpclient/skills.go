package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"harness-admin/internal/mcpadmin"
)

const SkillsExtension = "io.modelcontextprotocol/skills"

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var skillDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type SkillResource struct {
	URI    string `json:"uri"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

type SkillEntry struct {
	URI         string          `json:"uri"`
	Frontmatter map[string]any  `json:"frontmatter"`
	Resources   json.RawMessage `json:"resources"`
}

type ListSkillsParams struct {
	mcp.ParamsBase
	Cursor string `json:"cursor,omitempty"`
}

type ListSkillsResult struct {
	mcp.ResultBase
	ResultType string       `json:"resultType"`
	Skills     []SkillEntry `json:"skills"`
	NextCursor string       `json:"nextCursor,omitempty"`
	TTLMS      int64        `json:"ttlMs"`
	CacheScope string       `json:"cacheScope"`
}

type GetSkillParams struct {
	mcp.ParamsBase
	URI string `json:"uri"`
}

type GetSkillResult struct {
	mcp.ResultBase
	ResultType string     `json:"resultType"`
	Skill      SkillEntry `json:"skill"`
	TTLMS      int64      `json:"ttlMs"`
	CacheScope string     `json:"cacheScope"`
}

func newProtocolClient() (*mcp.Client, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "harness-admin", Version: "0.1.0"}, nil)
	if err := mcp.AddSendingCustomMethod[*ListSkillsParams, *ListSkillsResult](client, "skills/list"); err != nil {
		return nil, err
	}
	if err := mcp.AddSendingCustomMethod[*GetSkillParams, *GetSkillResult](client, "skills/get"); err != nil {
		return nil, err
	}
	return client, nil
}

func skillsSupported(session *mcp.ClientSession) bool {
	initial := session.InitializeResult()
	if initial == nil || initial.Capabilities == nil || initial.Capabilities.Resources == nil {
		return false
	}
	_, ok := initial.Capabilities.Extensions[SkillsExtension]
	return ok
}

func (c *Client) SupportsSkills(ctx context.Context, service mcpadmin.ServiceRecord, credentials mcpadmin.UpstreamCredentials) (bool, error) {
	session, err := c.connect(ctx, service, credentials)
	if err != nil {
		return false, err
	}
	defer session.Close()
	return skillsSupported(session), nil
}

func (c *Client) ListSkills(ctx context.Context, service mcpadmin.ServiceRecord, credentials mcpadmin.UpstreamCredentials, cursor string) (*ListSkillsResult, error) {
	if len(cursor) > 2048 {
		return nil, mcpadmin.ErrInvalidInput
	}
	session, err := c.connect(ctx, service, credentials)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	if !skillsSupported(session) {
		return nil, errors.New("上游 MCP 服务未声明 Skills 扩展")
	}
	result, err := mcp.CallCustomMethod[*ListSkillsParams, *ListSkillsResult](ctx, session, "skills/list", &ListSkillsParams{Cursor: cursor})
	if err != nil || !validSkillList(result) {
		return nil, errors.New("读取上游 Skills 目录失败或目录无效")
	}
	return result, nil
}

func (c *Client) GetSkill(ctx context.Context, service mcpadmin.ServiceRecord, credentials mcpadmin.UpstreamCredentials, uri string) (*GetSkillResult, error) {
	if !strings.HasSuffix(uri, "/SKILL.md") || !validResourceURI(uri) {
		return nil, mcpadmin.ErrInvalidInput
	}
	session, err := c.connect(ctx, service, credentials)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	if !skillsSupported(session) {
		return nil, errors.New("上游 MCP 服务未声明 Skills 扩展")
	}
	result, err := mcp.CallCustomMethod[*GetSkillParams, *GetSkillResult](ctx, session, "skills/get", &GetSkillParams{URI: uri})
	if err != nil || result == nil || result.ResultType != "complete" || !validSkillEntry(result.Skill) {
		return nil, errors.New("读取上游 Skill 失败或内容无效")
	}
	return result, nil
}

func validSkillList(result *ListSkillsResult) bool {
	if result == nil || result.ResultType != "complete" || len(result.Skills) > 500 || len(result.NextCursor) > 2048 {
		return false
	}
	for _, skill := range result.Skills {
		if !validSkillEntry(skill) {
			return false
		}
	}
	return true
}

func validSkillEntry(skill SkillEntry) bool {
	name, _ := skill.Frontmatter["name"].(string)
	description, _ := skill.Frontmatter["description"].(string)
	if len(name) == 0 || len(name) > 64 || !skillNamePattern.MatchString(name) ||
		len(description) == 0 || len(description) > 1024 || !validResourceURI(skill.URI) ||
		!strings.HasSuffix(skill.URI, "/"+name+"/SKILL.md") && skill.URI != "skill://"+name+"/SKILL.md" {
		return false
	}
	if string(skill.Resources) == `"dynamic"` {
		return true
	}
	var files []SkillResource
	if err := json.Unmarshal(skill.Resources, &files); err != nil || len(files) == 0 || len(files) > 512 {
		return false
	}
	var total int64
	entryPresent := false
	for _, file := range files {
		if !validResourceURI(file.URI) || !strings.HasPrefix(file.URI, strings.TrimSuffix(skill.URI, "SKILL.md")) ||
			!skillDigestPattern.MatchString(file.Digest) || file.Size < 0 {
			return false
		}
		total += file.Size
		if total > 16*1024*1024 {
			return false
		}
		if file.URI == skill.URI {
			entryPresent = true
		}
	}
	return entryPresent
}
