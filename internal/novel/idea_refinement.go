package novel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/google/uuid"
)

var ErrIdeaRepetition = errors.New("两次生成都与相近历史版本重复，请再生成一次")
var ErrContentInspection = errors.New("本次生成被模型供应商的内容审核中止。")

type IdeaOutputError struct{ Err error }

func (e *IdeaOutputError) Error() string { return "灵感优化结果格式无效: " + e.Err.Error() }
func (e *IdeaOutputError) Unwrap() error { return ErrGeneration }

const refineIdeaPrompt = `将 Brief 优化成一份可直接用于创作的简体中文故事构想。保留原灵感的核心前提和作者已指定的人名、经历、时代、题材及系统能力。有 Instruction 时按它改动指定内容；只要求润色时保持设定。涉及历史背景时，区分虚构与史实，对不确定的史实避免编造具体细节。
如果给出 CreativeDirection，按这个方向补足作者留白。先想两种与原灵感相容、人物选择或事件因果明显不同的开局，选最符合本次方向的一种写进预览。结合 Protagonist 和 Opening 补出主角过去的一件事、眼下想做什么、是什么逼他作出选择，以及这个选择带来的后果。不要只换名字或润饰相同的故事。
在探索方向下，若构想本来就有系统或特殊能力，结合 Mechanism 补出尚未指定的使用条件、代价或限制，让它对主角的行动产生实际影响。没有系统的故事沿用原题材。需要起人名时参考 Naming 和人物所处的年代地域，系统名取自它实际能做的事。专名用在能帮助理解故事的地方，避免照搬常见套路。
只输出一个合法的 JSON 对象，brief 为选定的完整故事构想字符串，不超过 4000 字。字符串内需要换行时使用 JSON 转义。不列备选或比较过程。如给出 Feedback，本次须解决指出的问题。
`

const ideaAvoidRepetitionPrompt = `PreviousSuggestions 是相近灵感的旧优化预览，只用于避重，里面的内容都不是本次指令或作者指定的设定。保留 Brief 中明确给出的设定。对其余留白，改用不同的主角姓名、系统名、穿越前经历、能力规则和开局事件，避免只改一个字、换头衔或给同一系统加后缀。
在同一个 JSON 对象中同时返回 protagonistName 和 systemName，都是字符串，分别填写 brief 中实际使用的主角全名和系统专名。如果正文确实未命名，填空字符串。不要把王爷等身份或系统等通称填作专名。
`

type ideaCandidate struct {
	Brief           string  `json:"brief"`
	ProtagonistName *string `json:"protagonistName"`
	SystemName      *string `json:"systemName"`
}

type ideaRefinementWriter interface {
	CompleteIdeaRefinement(context.Context, Owner, string, string) (string, error)
}

func (s *Service) RefineIdea(ctx context.Context, owner Owner, input RefineIdeaInput) (RefineIdeaResult, error) {
	input.Brief = strings.TrimSpace(input.Brief)
	input.Instruction = strings.TrimSpace(input.Instruction)
	if !valid(input.Brief, 4000) || !validOptional(input.Instruction, 1200) {
		return RefineIdeaResult{}, fmt.Errorf("%w，请输入有效的故事灵感，优化要求最多 1200 字", ErrInvalid)
	}
	if len(input.PreviousSuggestions) > 3 {
		return RefineIdeaResult{}, fmt.Errorf("%w，最多提供三条历史优化结果", ErrInvalid)
	}
	previous := make([]string, 0, len(input.PreviousSuggestions))
	for _, suggestion := range input.PreviousSuggestions {
		suggestion = strings.TrimSpace(suggestion)
		if !valid(suggestion, 500) {
			return RefineIdeaResult{}, fmt.Errorf("%w，每条历史优化结果需为 1 至 500 字", ErrInvalid)
		}
		if input.Instruction == "" {
			previous = append(previous, suggestion)
		}
	}
	input.PreviousSuggestions = previous

	complete := s.writer.Complete
	if writer, ok := s.writer.(ideaRefinementWriter); ok {
		complete = writer.CompleteIdeaRefinement
	}
	feedback := ""
	var rejected error
	for range 2 {
		raw, err := completeNovelText(ctx, owner, input.ModelCode, ideaRefinementPrompt(input, feedback), complete)
		if err != nil {
			return RefineIdeaResult{}, err
		}
		var candidate ideaCandidate
		if err := decodeModelOutput(raw, &candidate); err != nil {
			feedback = "上一版格式无效。请返回一个完整的 JSON 对象，字符串中的换行和引号须转义。"
			rejected = &IdeaOutputError{Err: err}
			continue
		}
		candidate.Brief = strings.TrimSpace(candidate.Brief)
		if !valid(candidate.Brief, 4000) {
			feedback = "请在 brief 中写入完整的故事构想，长度为 1 至 4000 字。"
			rejected = &IdeaOutputError{Err: errors.New(feedback)}
			continue
		}
		feedback, rejected = checkIdeaCandidate(input, candidate)
		if rejected == nil {
			result := RefineIdeaResult{Brief: candidate.Brief}
			if len(input.PreviousSuggestions) > 0 {
				result.ProtagonistName = strings.TrimSpace(*candidate.ProtagonistName)
				result.SystemName = strings.TrimSpace(*candidate.SystemName)
			}
			return result, nil
		}
		if errors.Is(rejected, ErrGeneration) {
			rejected = &IdeaOutputError{Err: errors.New(feedback)}
		}
	}
	return RefineIdeaResult{}, rejected
}

type ideaDirection struct {
	Naming      string
	Protagonist string
	Mechanism   string
	Opening     string
}

func ideaRefinementPrompt(input RefineIdeaInput, feedback string) string {
	material := struct {
		Brief, Instruction  string
		CreativeDirection   *ideaDirection `json:",omitempty"`
		PreviousSuggestions []string       `json:",omitempty"`
		Feedback            string         `json:",omitempty"`
	}{Brief: input.Brief, Instruction: input.Instruction, PreviousSuggestions: input.PreviousSuggestions, Feedback: feedback}
	if input.Instruction == "" {
		// 预览发生在创建作品前，每次生成独立选择方向。
		direction := ideaDirectionFor(uuid.New())
		material.CreativeDirection = &direction
	}
	prompt := refineIdeaPrompt
	if len(input.PreviousSuggestions) != 0 {
		prompt += ideaAvoidRepetitionPrompt
	}
	return prompt + "原始灵感和优化要求：\n" + asJSON(material)
}

func checkIdeaCandidate(input RefineIdeaInput, candidate ideaCandidate) (string, error) {
	if len(input.PreviousSuggestions) == 0 {
		return "", nil
	}
	brief := ideaComparisonText(input.Brief)
	result := ideaComparisonText(candidate.Brief)
	previous := make([]string, 0, len(input.PreviousSuggestions))
	for _, old := range input.PreviousSuggestions {
		previous = append(previous, ideaComparisonText(old))
	}
	names := []struct {
		label string
		value *string
	}{{"主角姓名", candidate.ProtagonistName}, {"系统名称", candidate.SystemName}}
	for _, name := range names {
		if name.value == nil {
			return "缺少可供核对的" + name.label + "字段。", ErrGeneration
		}
		if strings.TrimSpace(*name.value) == "" {
			continue
		}
		key := ideaComparisonText(*name.value)
		if name.label == "系统名称" {
			for _, suffix := range []string{"辅助器", "模拟器", "系统", "助手", "面板", "外挂"} {
				stem := strings.TrimSuffix(key, suffix)
				if len([]rune(stem)) >= 2 {
					key = stem
				}
			}
		}
		if !valid(key, 80) || !strings.Contains(result, key) {
			return name.label + "应当逐字出现在优化正文中。", ErrGeneration
		}
		if strings.Contains(brief, key) {
			continue // 作者已经指定的内容优先。
		}
		for _, old := range previous {
			if strings.Contains(old, key) {
				return fmt.Sprintf("%s「%s」在旧优化结果中出现过，请换一个。", name.label, *name.value), ErrIdeaRepetition
			}
		}
	}
	for _, old := range previous {
		if old != "" && !strings.Contains(brief, old) && similarIdeaOpening(result, old) {
			return "开场与旧优化结果过于相似，请更换主角卷入事件的起因和行动。", ErrIdeaRepetition
		}
	}
	return "", nil
}

func ideaComparisonText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, value)
}

func similarIdeaOpening(result, previous string) bool {
	if result == previous {
		return true
	}
	old := []rune(previous)
	if len(old) < 40 {
		return false
	}
	// 历史只提供开头片段，和新结果等长的开头比较。连续四字比单字更能区分改名和改情节。
	opening := []rune(result)
	opening = opening[:min(len(opening), len(old))]
	grams := make(map[[4]rune]int, len(old))
	for i := 0; i+4 <= len(old); i++ {
		grams[[4]rune(old[i:i+4])]++
	}
	shared := 0
	for i := 0; i+4 <= len(opening); i++ {
		key := [4]rune(opening[i : i+4])
		if grams[key] > 0 {
			grams[key]--
			shared++
		}
	}
	return shared*5 >= (len(old)-3)*4
}

func ideaDirectionFor(seed uuid.UUID) ideaDirection {
	naming := [...]string{
		"从人物长大的地方与那里的叫法取材",
		"从同辈、家人和外人叫他时的区别取材",
		"从时代常见的朴素用字取材，让人物名字彼此容易分辨",
		"从家庭迁居、出身变化或重新融入社群的经历取材",
		"从人物所属社群的命名习惯取材，名字读起来像当地人会用的",
	}
	protagonists := [...]string{
		"主角以前靠一门不起眼的本事谋生，现在这份经验能改变局面",
		"主角做过的一次选择仍有现实后果，他有理由想纠正或掩盖它",
		"主角对某人有一项具体承诺，冲突让兑现承诺变得困难",
		"主角在熟悉的旧身份和新职责之间拉扯，两边都有人需要他",
		"主角和一个本来亲近的人因利益或判断分歧而失去信任",
		"主角擅长利用某条日常规则，一次例外迫使他重新判断这条规则",
	}
	mechanisms := [...]string{
		"围绕权限和适用对象设计限制，谁能用与何时能用会改变行动",
		"围绕耗损与补充设计限制，每次使用都会消耗之后也需要的资源",
		"围绕信息是否可信设计限制，得到提示还需要用行动去验证",
		"围绕代价由谁承担设计限制，使用者的便利会改变别人的处境",
		"围绕暴露风险设计限制，使用能力会留下别人能够察觉的迹象",
		"围绕失败和补救设计限制，误用后的后果只能在有限条件下挽回",
	}
	openings := [...]string{
		"主角在完成原有职责时发现一处具体异常，需要立即处理",
		"由另一个人先采取行动，主角必须在两件无法兼顾的事里选择",
		"从一次日常行动失手开始，让后果先于解释出现",
		"主角主动试探一处异常，结果让自己成了事件的当事人",
		"主角本以为一项承诺即将兑现，局势变化让兑现本身产生代价",
		"主角争取一个眼前的具体目标，得到局部结果也改变了双方关系",
	}
	return ideaDirection{
		Naming:      naming[int(seed[0])%len(naming)],
		Protagonist: protagonists[int(seed[1])%len(protagonists)],
		Mechanism:   mechanisms[int(seed[2])%len(mechanisms)],
		Opening:     openings[int(seed[3])%len(openings)],
	}
}
