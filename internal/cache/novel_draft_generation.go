package cache

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"

	"harness-admin/internal/novel"
)

type NovelDraftGenerationStore struct{ client *Client }

func NewNovelDraftGenerationStore(client *Client) *NovelDraftGenerationStore {
	return &NovelDraftGenerationStore{client: client}
}

var startNovelDraft = redis.NewScript(`
local lease = redis.call('GET', KEYS[2])
if lease then
    local encoded = redis.call('GET', KEYS[1])
    if not encoded or cjson.decode(encoded).jobId ~= lease then
        return redis.error_reply('小说正文任务状态与运行租约不一致')
    end
    return encoded
end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
redis.call('SET', KEYS[2], ARGV[3], 'PX', ARGV[4])
return ARGV[1]
`)

var getNovelDraft = redis.NewScript(`
local encoded = redis.call('GET', KEYS[1])
if not encoded then return '' end
local job = cjson.decode(encoded)
if job.status == 'running' and redis.call('GET', KEYS[2]) ~= job.jobId then
    job.status = 'failed'
    job.phase = nil
    job.errorMessage = '正文后台任务已中断，请查看已保存的草稿或重新生成。'
    return cjson.encode(job)
end
return encoded
`)

var updateNovelDraft = redis.NewScript(`
if redis.call('GET', KEYS[2]) ~= ARGV[1] then return 0 end
redis.call('SET', KEYS[1], ARGV[2], 'PX', ARGV[3])
if ARGV[4] ~= 'running' then redis.call('DEL', KEYS[2]) end
return 1
`)

func (s *NovelDraftGenerationStore) StartDraft(ctx context.Context, owner novel.Owner, id string, chapter int, job novel.DraftGeneration) (novel.DraftGeneration, bool, error) {
	encoded, err := json.Marshal(job)
	if err != nil {
		return novel.DraftGeneration{}, false, err
	}
	stored, err := startNovelDraft.Run(ctx, s.client.raw, novelDraftKeys(owner, id, chapter), encoded,
		novel.DraftGenerationRetention.Milliseconds(), job.JobID, novel.DraftGenerationLease.Milliseconds()).Text()
	if err != nil {
		return novel.DraftGeneration{}, false, err
	}
	var result novel.DraftGeneration
	err = json.Unmarshal([]byte(stored), &result)
	return result, result.JobID == job.JobID, err
}

func (s *NovelDraftGenerationStore) GetDraft(ctx context.Context, owner novel.Owner, id string, chapter int) (novel.DraftGeneration, error) {
	encoded, err := getNovelDraft.Run(ctx, s.client.raw, novelDraftKeys(owner, id, chapter)).Text()
	if err != nil {
		return novel.DraftGeneration{}, err
	}
	if encoded == "" {
		return novel.DraftGeneration{Status: novel.DraftGenerationIdle}, nil
	}
	var job novel.DraftGeneration
	err = json.Unmarshal([]byte(encoded), &job)
	return job, err
}

func (s *NovelDraftGenerationStore) UpdateDraft(ctx context.Context, owner novel.Owner, id string, chapter int, job novel.DraftGeneration) (bool, error) {
	encoded, err := json.Marshal(job)
	if err != nil {
		return false, err
	}
	updated, err := updateNovelDraft.Run(ctx, s.client.raw, novelDraftKeys(owner, id, chapter), job.JobID, encoded,
		novel.DraftGenerationRetention.Milliseconds(), job.Status).Int()
	return updated == 1, err
}

func novelDraftKeys(owner novel.Owner, id string, chapter int) []string {
	key := fmt.Sprintf("novel:draft:%s:%s:%s:%d", owner.OrganizationID, owner.UserID, id, chapter)
	return []string{key + ":state", key + ":lease"}
}
