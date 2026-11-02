package drama

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

const storyboardVoiceContract = `声音条目只有kind、speaker、text、sourceLine以及可选的旁白原句sourceText、delivery、adaptationReason。kind只能是dialogue（角色对白）、os（内心）、vo（旁白/画外音）。没有voice必填字段，禁止要求kind使用narration或内心os。角色对白、关键心声及角色画外音不得压缩改写。只有明确的解释性旁白可在保留关键信息后精简或改成画面，原句由程序保存在sourceText。delivery省略或spoken表示口播；visual只用于旁白转画面，此时text必须为空，关键事实必须体现在画面中。`

func canonicalStoryboardKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "dialogue", "对白":
		return "dialogue"
	case "os", "内心os", "内心独白", "inner_monologue":
		return "os"
	case "vo", "narration", "旁白", "画外音", "voiceover":
		return "vo"
	default:
		return kind
	}
}
func (u *StoryboardUtterance) UnmarshalJSON(data []byte) error {
	type plain StoryboardUtterance
	if err := json.Unmarshal(data, (*plain)(u)); err != nil {
		return err
	}
	u.Kind = canonicalStoryboardKind(u.Kind)
	if u.Delivery == "spoken" {
		u.Delivery = ""
	}
	return nil
}
func narrationSpeaker(u StoryboardUtterance) bool {
	return u.Kind == "vo" && (u.Speaker == "旁白" || u.Speaker == "叙述者" || strings.EqualFold(u.Speaker, "narrator"))
}
func narrationSource(line string, forms []AssetForm) bool {
	trimmed := strings.TrimSpace(line)
	for _, label := range []string{"旁白：", "旁白:", "叙述：", "叙述:", "画外旁白：", "画外音：", "画外音:", "VO：", "VO:"} {
		if strings.HasPrefix(trimmed, label) {
			return true
		}
	}
	if at := strings.IndexAny(trimmed, "：:"); at > 0 {
		label := strings.ToLower(trimmed[:at])
		if strings.Contains(label, "内心") || strings.Contains(label, "心声") || strings.Contains(label, "os") {
			return false
		}
	}
	return !storyboardSpeakerLine(line, forms) && !strings.HasPrefix(trimmed, "【")
}
func originalStoryboardSpeech(u StoryboardUtterance) string {
	if u.SourceText != "" {
		return u.SourceText
	}
	return u.Text
}
func storyboardHasSpeech(s StoryboardShot) bool {
	for _, u := range s.Utterances {
		if u.Delivery != "visual" && strings.TrimSpace(u.Text) != "" {
			return true
		}
	}
	return false
}
func storyboardSpeechSeconds(s StoryboardShot) float64 {
	seconds := 0.0
	for _, u := range s.Utterances {
		if u.Delivery != "visual" {
			seconds += estimateStoryboardSpeech(u.Text) + .5
		}
	}
	return seconds
}

// 只补足可确定的口播时长，不修改任何文字；超过单镜范围应拆镜而不能删台词。
func repairStoryboardShotTiming(s *StoryboardShot) error {
	needed := storyboardSpeechSeconds(*s)
	if needed > 60 {
		return &workflowProblem{Origin: "程序校验", Code: "speech_requires_split", Field: "duration", Cause: fmt.Errorf("完整口播约需%.1f秒，超过单镜60秒；请保留全文按语义拆镜", needed)}
	}
	if needed > s.Duration+.25 {
		s.Duration = math.Ceil(needed*2) / 2
	}
	return nil
}
func repairStoryboardTiming(seg *StoryboardSegment) error {
	for i := range seg.Shots {
		if err := repairStoryboardShotTiming(&seg.Shots[i]); err != nil {
			return err
		}
	}
	return nil
}
func storyboardSpeechBounds(seg StoryboardSegment) string {
	var b strings.Builder
	for i, shot := range seg.Shots {
		fmt.Fprintf(&b, "镜头%d：当前%.1f秒，现有口播估时%.2f秒；未精简旁白时不得低于约%.1f秒。\n", i+1, shot.Duration, storyboardSpeechSeconds(shot), math.Max(.5, math.Ceil(storyboardSpeechSeconds(shot)*2)/2))
	}
	return b.String()
}
