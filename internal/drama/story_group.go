package drama

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 组仍是一次模型请求的边界；每个通过的单元独立持久化，重试与续跑只请求未通过部分。
func (w *batchWorkflow) readStoryGroup(group []sourceUnit, number, total, unitTotal int) (evidenceDigest, error) {
	started := time.Now()
	out := evidenceDigest{Units: []evidenceUnit{}}
	prompt := fmt.Sprintf(groundedStoryPrompt, sourceEvidenceText(group))
	groupHash := inputFingerprint(w.job, prompt)
	groupStep := BatchStep{Stage: "story", ChunkNumber: number, ChunkTotal: total, InputHash: groupHash}
	if old, ok := w.saved[fmt.Sprintf("story:%d", number)]; ok && old.Status == "completed" && old.ChunkTotal == total && old.InputHash == groupHash {
		raw, err := groundedEvidence(json.RawMessage(old.ResultText), group, w.job.Batch.StartOffset)
		if err != nil {
			return out, fmt.Errorf("已保存剧情证据校验失败：%w", err)
		}
		err = json.Unmarshal(raw, &out)
		return out, err
	}
	if err := w.service.repository.StartBatchStep(w.ctx, w.job.OrganizationID, w.job.OwnerID, w.job.Batch.ProjectID, w.job.Batch.ID, w.job.Batch.AnalysisRevision, groupStep); err != nil {
		return out, err
	}
	if err := w.emit("story", number, "started", fmt.Sprintf("剧情整理开始处理第 %d/%d 组，包含%d个原文单元", number, total, len(group))); err != nil {
		return out, err
	}
	passed := map[int]evidenceUnit{}
	hashes := map[int]string{}
	pending := []sourceUnit{}
	for _, unit := range group {
		hash := inputFingerprint(w.job, "原文证据单元v1\n"+fmt.Sprintf(groundedStoryPrompt, sourceEvidenceText([]sourceUnit{unit})))
		hashes[unit.ID] = hash
		if old, ok := w.saved[fmt.Sprintf("evidence:%d", unit.ID)]; ok && old.Status == "completed" && old.ChunkTotal == unitTotal && old.InputHash == hash {
			var digest evidenceDigest
			raw, err := groundedEvidence(json.RawMessage(old.ResultText), []sourceUnit{unit}, w.job.Batch.StartOffset)
			if err == nil {
				err = json.Unmarshal(raw, &digest)
			}
			if err != nil {
				return out, fmt.Errorf("读取单元%d已保存证据失败：%w", unit.ID, err)
			}
			passed[unit.ID] = digest.Units[0]
			continue
		}
		step := BatchStep{Stage: "evidence", ChunkNumber: unit.ID, ChunkTotal: unitTotal, InputHash: hash}
		if err := w.service.repository.StartBatchStep(w.ctx, w.job.OrganizationID, w.job.OwnerID, w.job.Batch.ProjectID, w.job.Batch.ID, w.job.Batch.AnalysisRevision, step); err != nil {
			return out, err
		}
		pending = append(pending, unit)
	}
	failures := map[int]string{}
	for attempt := 0; len(pending) > 0 && attempt < 2; attempt++ {
		requestPrompt := fmt.Sprintf(groundedStoryPrompt, sourceEvidenceText(pending))
		if attempt > 0 {
			w.retried.Store(true)
			messages := []string{}
			for _, unit := range pending {
				messages = append(messages, fmt.Sprintf("单元%d：%s", unit.ID, failures[unit.ID]))
			}
			requestPrompt += "\n以下单元上次未通过，其他单元已保存，不要重新输出其他单元：\n" + strings.Join(messages, "\n")
			if err := w.emit("story", number, "retry", fmt.Sprintf("第%d组已保存%d/%d个单元，仅重试：%s", number, len(passed), len(group), strings.Join(messages, "；"))); err != nil {
				return out, err
			}
		} else if len(passed) > 0 {
			if err := w.emit("story", number, "resumed", fmt.Sprintf("第%d组复用%d个已完成单元，仅处理剩余%d个", number, len(passed), len(pending))); err != nil {
				return out, err
			}
		}
		response, callErr := w.complete("story", number, requestPrompt)
		byID := map[int][]json.RawMessage{}
		globalError := ""
		if callErr != nil {
			globalError = callErr.Error()
		} else {
			raw, err := parseObject(response)
			if err != nil {
				globalError = "JSON格式错误：" + err.Error()
			} else {
				var reply struct {
					Units []json.RawMessage `json:"units"`
				}
				if err = json.Unmarshal(raw, &reply); err != nil || len(reply.Units) == 0 {
					globalError = "证据结构错误：必须返回非空units数组"
				} else {
					for _, part := range reply.Units {
						var id struct {
							ID json.RawMessage `json:"id"`
						}
						if err = json.Unmarshal(part, &id); err != nil {
							globalError = "单元结构错误：" + err.Error()
							continue
						}
						var minimal evidenceUnit
						if err = json.Unmarshal([]byte(`{"id":`+string(id.ID)+`}`), &minimal); err != nil {
							globalError = err.Error()
							continue
						}
						byID[minimal.ID] = append(byID[minimal.ID], part)
					}
				}
			}
		}
		remaining := []sourceUnit{}
		for _, unit := range pending {
			reason := globalError
			if callErr == nil && len(byID[unit.ID]) == 1 {
				var decoded evidenceUnit
				err := json.Unmarshal(byID[unit.ID][0], &decoded)
				if err == nil {
					decoded, err = normalizeEvidenceUnit(decoded, unit, w.job.Batch.StartOffset)
				}
				if err == nil {
					raw, marshalErr := json.Marshal(evidenceDigest{Units: []evidenceUnit{decoded}})
					if marshalErr != nil {
						return out, marshalErr
					}
					step := BatchStep{Stage: "evidence", ChunkNumber: unit.ID, ChunkTotal: unitTotal, InputHash: hashes[unit.ID], ResultText: string(raw)}
					if err = w.service.repository.CompleteBatchStep(w.ctx, w.job.OrganizationID, w.job.OwnerID, w.job.Batch.ProjectID, w.job.Batch.ID, w.job.Batch.AnalysisRevision, step); err != nil {
						return out, err
					}
					passed[unit.ID] = decoded
					delete(failures, unit.ID)
					continue
				}
				reason = err.Error()
			} else if callErr == nil && len(byID[unit.ID]) > 1 {
				reason = fmt.Sprintf("单元%d重复返回，必须唯一", unit.ID)
			}
			if reason == "" {
				reason = fmt.Sprintf("模型遗漏单元%d，请只输出该单元", unit.ID)
			}
			failures[unit.ID] = reason
			remaining = append(remaining, unit)
		}
		pending = remaining
		if err := w.emit("story", number, "checkpoint", fmt.Sprintf("剧情整理第%d组已保存%d/%d个单元，剩余%d个", number, len(passed), len(group), len(pending))); err != nil {
			return out, err
		}
		if callErr != nil && !errors.Is(callErr, ErrModelTimeout) && !errors.Is(callErr, ErrEmptyModelContent) && !errors.Is(callErr, ErrModelTemporary) {
			break
		}
	}
	if len(pending) > 0 {
		messages := []string{}
		for _, unit := range pending {
			reason := failures[unit.ID]
			if err := w.service.repository.FailBatchStep(w.ctx, w.job, "evidence", unit.ID, reason); err != nil {
				return out, err
			}
			messages = append(messages, reason)
		}
		reason := fmt.Sprintf("剧情整理第%d组失败：%s", number, strings.Join(messages, "；"))
		if err := w.service.repository.FailBatchStep(w.ctx, w.job, "story", number, reason); err != nil {
			return out, err
		}
		if err := w.emit("story", number, "failed", reason); err != nil {
			return out, err
		}
		return out, errors.New(reason)
	}
	for _, unit := range group {
		out.Units = append(out.Units, passed[unit.ID])
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	groupStep.ResultText = string(raw)
	groupStep.DurationMS = time.Since(started).Milliseconds()
	if err = w.service.repository.CompleteBatchStep(w.ctx, w.job.OrganizationID, w.job.OwnerID, w.job.Batch.ProjectID, w.job.Batch.ID, w.job.Batch.AnalysisRevision, groupStep); err != nil {
		return out, err
	}
	return out, w.emit("story", number, "completed", fmt.Sprintf("剧情整理第 %d/%d 组已保存，%d个单元，用时 %d 秒", number, total, len(group), groupStep.DurationMS/1000))
}
