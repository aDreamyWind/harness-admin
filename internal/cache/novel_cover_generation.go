package cache

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/redis/go-redis/v9"

	"harness-admin/internal/novel"
)

// 状态与候选图分别保存，轮询只读任务状态和候选图是否存在。
const readCoverGenerationState = `
local function readState()
    local encoded = redis.call('GET', KEYS[1])
    if not encoded then return nil end
    local state = cjson.decode(encoded)
    if redis.call('EXISTS', KEYS[3]) == 0 then
        state.previewToken = nil
        state.previewBaseRevision = nil
    end
    return state
end
`

var startCoverGeneration = redis.NewScript(readCoverGenerationState + `
local previous = readState()
local lease = redis.call('GET', KEYS[2])
if lease then
    if not previous or previous.jobId ~= lease then
        return redis.error_reply('封面任务状态与运行租约不一致')
    end
    return cjson.encode(previous)
end
local job = cjson.decode(ARGV[1])
if previous then
    job.previewToken = previous.previewToken
    job.previewBaseRevision = previous.previewBaseRevision
else
    local oldPreview = redis.call('GET', KEYS[3])
    if oldPreview then
        local preview = cjson.decode(oldPreview)
        job.previewToken = preview.Token
        job.previewBaseRevision = preview.PreviousRevision
    end
end
local encoded = cjson.encode(job)
redis.call('SET', KEYS[1], encoded, 'PX', ARGV[2])
redis.call('SET', KEYS[2], job.jobId, 'PX', ARGV[3])
return encoded
`)

var getCoverGeneration = redis.NewScript(readCoverGenerationState + `
local state = readState()
if not state then return '' end
if state.status == 'running' and redis.call('GET', KEYS[2]) ~= state.jobId then
    state.status = 'failed'
    state.errorMessage = ARGV[1]
end
return cjson.encode(state)
`)

var finishCoverGeneration = redis.NewScript(`
if redis.call('GET', KEYS[2]) ~= ARGV[1] then return 0 end
if ARGV[3] ~= '' then
    redis.call('SET', KEYS[3], ARGV[3], 'PX', ARGV[4])
end
redis.call('SET', KEYS[1], ARGV[2], 'PX', ARGV[4])
redis.call('DEL', KEYS[2])
return 1
`)

func (s *NovelCoverPreviewStore) StartGeneration(ctx context.Context, owner novel.Owner, id string, job novel.CoverGeneration) (novel.CoverGeneration, bool, error) {
	data, err := json.Marshal(job)
	if err != nil {
		return novel.CoverGeneration{}, false, err
	}
	encoded, err := startCoverGeneration.Run(ctx, s.client.raw, coverGenerationKeys(owner, id), data,
		novel.CoverGenerationRetention.Milliseconds(), novel.CoverGenerationLease.Milliseconds()).Text()
	if err != nil {
		return novel.CoverGeneration{}, false, err
	}
	var stored novel.CoverGeneration
	if err := json.Unmarshal([]byte(encoded), &stored); err != nil {
		return novel.CoverGeneration{}, false, err
	}
	return stored, stored.JobID == job.JobID, nil
}

func (s *NovelCoverPreviewStore) GetGeneration(ctx context.Context, owner novel.Owner, id string) (novel.CoverGeneration, error) {
	encoded, err := getCoverGeneration.Run(ctx, s.client.raw, coverGenerationKeys(owner, id), novel.CoverGenerationInterrupted).Text()
	if err != nil {
		return novel.CoverGeneration{}, err
	}
	if encoded == "" {
		return novel.CoverGeneration{Status: novel.CoverGenerationIdle}, nil
	}
	var job novel.CoverGeneration
	err = json.Unmarshal([]byte(encoded), &job)
	return job, err
}

func (s *NovelCoverPreviewStore) FinishGeneration(ctx context.Context, owner novel.Owner, id string, job novel.CoverGeneration, preview *novel.CoverPreview) error {
	var imageData []byte
	if preview != nil {
		job.PreviewToken, job.PreviewBaseRevision = preview.Token, preview.PreviousRevision
		var err error
		imageData, err = json.Marshal(preview)
		if err != nil {
			return err
		}
	}
	state, err := json.Marshal(job)
	if err != nil {
		return err
	}
	updated, err := finishCoverGeneration.Run(ctx, s.client.raw, coverGenerationKeys(owner, id), job.JobID, state,
		string(imageData), novel.CoverGenerationRetention.Milliseconds()).Int()
	if err != nil {
		return err
	}
	if updated != 1 {
		return errors.New("小说封面生成任务的运行租约已失效")
	}
	return nil
}

func coverGenerationKeys(owner novel.Owner, id string) []string {
	preview := novelCoverPreviewKey(owner, id)
	return []string{preview + ":generation", preview + ":lease", preview}
}
