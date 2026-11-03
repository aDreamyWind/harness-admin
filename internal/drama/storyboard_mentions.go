package drama

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

func visitStoryboardMentions(seg StoryboardSegment, visit func(string, []StoryboardMention) error) error {
	if err := visit(seg.Scene, seg.SceneMentions); err != nil {
		return err
	}
	if err := visit(seg.Purpose, seg.PurposeMentions); err != nil {
		return err
	}
	for _, shot := range seg.Shots {
		if err := visit(shot.Description, shot.DescriptionMentions); err != nil {
			return err
		}
		if err := visit(shot.Sound, shot.SoundMentions); err != nil {
			return err
		}
		for _, u := range shot.Utterances {
			if err := visit(u.Text, u.Mentions); err != nil {
				return err
			}
			if err := visit(u.Speaker, u.SpeakerMentions); err != nil {
				return err
			}
		}
	}
	return nil
}
func includeMentionForms(seg *StoryboardSegment) {
	known := map[string]bool{}
	for _, id := range seg.FormIDs {
		known[id] = true
	}
	_ = visitStoryboardMentions(*seg, func(_ string, refs []StoryboardMention) error {
		for _, ref := range refs {
			if !known[ref.FormID] {
				seg.FormIDs = append(seg.FormIDs, ref.FormID)
				known[ref.FormID] = true
			}
		}
		return nil
	})
}
func validateStoryboardMentions(seg StoryboardSegment, bound map[string]bool) error {
	return visitStoryboardMentions(seg, func(text string, refs []StoryboardMention) error {
		if len(refs) > 200 {
			return fmt.Errorf("单段文字最多包含200个资产引用")
		}
		previous := 0
		for _, ref := range refs {
			if ref.Start < previous || ref.Start < 0 || ref.End <= ref.Start || ref.End > utf8.RuneCountInString(text) {
				return fmt.Errorf("文字资产引用越界、重叠或未按文字顺序排列")
			}
			if !bound[ref.FormID] {
				return fmt.Errorf("文字引用了未关联或无权限的资产形象")
			}
			previous = ref.End
		}
		return nil
	})
}

const storyboardDescriptionAssetContract = "\n另返回 descriptionAssets 数组，每项为 {\"shot\":1,\"quote\":\"画面描述中的原词\",\"formId\":\"目录里的形象ID\"}。根据当前镜头上下文、剧本和资产描述，识别画面里实际出现的角色、场景和道具，包括语义明确的简称与别称。quote 必须逐字复制该镜头 description 中唯一出现的连续文字，尽量只引用名称。形象不确定或有歧义时跳过，不能编造形象，不改写原文或覆盖已有引用。shot 从 1 开始。"

type storyboardDescriptionAsset struct {
	Shot   int    `json:"shot"`
	Quote  string `json:"quote"`
	FormID string `json:"formId"`
}

// AI 判断实体身份，程序只定位原文并校验资产范围。
func applyStoryboardDescriptionAssets(seg *StoryboardSegment, assets []storyboardDescriptionAsset, forms []AssetForm) {
	if len(assets) == 0 {
		return
	}
	allowed := make(map[string]bool, len(forms))
	for _, form := range forms {
		allowed[form.ID] = true
	}
	for _, asset := range assets {
		if asset.Shot < 1 || asset.Shot > len(seg.Shots) || !allowed[asset.FormID] || strings.TrimSpace(asset.Quote) == "" {
			continue
		}
		shot := &seg.Shots[asset.Shot-1]
		if len(shot.DescriptionMentions) >= 200 {
			continue
		}
		at := strings.Index(shot.Description, asset.Quote)
		if at < 0 || at != strings.LastIndex(shot.Description, asset.Quote) {
			continue
		}
		start := utf8.RuneCountInString(shot.Description[:at])
		end := start + utf8.RuneCountInString(asset.Quote)
		refs := shot.DescriptionMentions
		index := sort.Search(len(refs), func(i int) bool { return refs[i].End > start })
		if index < len(refs) && refs[index].Start < end {
			continue
		}
		refs = append(refs, StoryboardMention{})
		copy(refs[index+1:], refs[index:])
		refs[index] = StoryboardMention{Start: start, End: end, FormID: asset.FormID}
		shot.DescriptionMentions = refs
	}
	includeMentionForms(seg)
}
