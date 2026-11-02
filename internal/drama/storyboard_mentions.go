package drama

import (
	"fmt"
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
