package novel

import (
	"fmt"
	"strings"
)

const refineIdeaPrompt = `将原始故事灵感优化成一段可用于创作的简体中文构想，保留核心点子和作者明确的限制。根据优化要求改动对应内容，未提供要求时理顺主角、冲突、动机和故事钩子，可在原意范围内补足因果。涉及历史背景时，优先遵循作者提供的时代和资料，区分虚构设定与历史事实，对不确定的史实避免编造具体细节。
只输出一个 JSON 对象，brief 为优化后的完整灵感字符串，不超过 4000 字。
原始灵感和优化要求：
`

const outlineResponseFormat = `只输出 JSON 对象，字段 synopsis（全书梗概）、world（世界规则与约束）、characters（主要人物与关系）均为字符串。beats 是数组，每项含字符串 title 与 goal。
%s作品资料：%s`

const outlinePrompt = `为这部中文小说制定大纲，规划恰好 %d 章。每章给出标题和具体情节目标。
逐章规划进入状态、行动的必要条件、章末变化，以及留给下一章的任务。明确时限何时开始、何时触发和错过时的后果，留足行动所需的时间。重要线索标明首次出现、如何核验、何时回收。区分记录能证明的事实和人物推测，避免用事后信息推动此前行动。核对人物能力限制、地点和道具状态在各章之间的变化是否都有原因。
` + outlineResponseFormat

const reviseOutlinePrompt = `按修改要求修订当前小说大纲，返回恰好 %d 章的完整修订稿。以当前大纲为底稿，对作者指出的问题及其直接影响的段落做必要修订，尽量逐字保留其他设定、人物与章节。原始灵感用于理解创作意图，修改要求明确纠正之处优先采用修改要求。
涉及历史纠正时，以作者给出的时代范围和依据为准，将修订同步到受影响的故事梗概、世界设定、人物和章节。对不确定的史实使用稳妥表述，避免捏造出处。保持章节顺序和数量，核对修订后的时间线、人物关系与线索承接。
` + outlineResponseFormat

func outlineGenerationPrompt(p *Project, action Action) (string, error) {
	titlePrompt := ""
	if p.NeedsTitle {
		titlePrompt = "同时拟定一个贴合故事的简体中文书名，以顶层 JSON 字符串字段 title 返回，最多 120 字。作品资料中的“未命名小说”是占位名。\n"
	}
	instruction := strings.TrimSpace(action.Instruction)
	if !validOptional(instruction, 1200) {
		return "", fmt.Errorf("%w，修改要求最多 1200 字", ErrInvalid)
	}
	if instruction == "" {
		return fmt.Sprintf(outlinePrompt, p.ChapterCount, titlePrompt, asJSON(struct{ Title, Brief string }{p.Title, p.Brief})), nil
	}
	current := action.Outline
	if current == nil {
		current = p.Outline
	}
	if current == nil {
		return "", fmt.Errorf("%w，请先准备完整大纲，再提交修改要求", ErrInvalid)
	}
	draft := *current
	draft.Beats = append([]Beat(nil), current.Beats...)
	if !normalizeOutline(&draft, p.ChapterCount) {
		return "", fmt.Errorf("%w，请先准备完整大纲，再提交修改要求", ErrInvalid)
	}
	material := struct {
		Title, Brief, Instruction string
		CurrentOutline            Outline
	}{p.Title, p.Brief, instruction, draft}
	return fmt.Sprintf(reviseOutlinePrompt, p.ChapterCount, titlePrompt, asJSON(material)), nil
}

const writePrompt = `撰写当前章的简体中文小说正文，目标约 1200 至 2000 字。以已定稿正文和摘要为连续性依据，当前章目标用于安排本章剧情，后续章目标只用于铺垫。定稿正文优先于摘要和大纲。若有现稿、审稿意见或修改要求，针对有证据的问题修改，保留无关情节。
输出前在内部复核并修正以下内容，无需输出检查过程：
1. 时间。核对开场承接点、事件发生与记录的先后、临近时限的行动耗时。若承诺的触发时点已经过去，在正文交代结果或为何未触发。回忆和现时场景要明确区分。
2. 线索。延续相关线索的名称或编号、持有人、存放地和状态。交接、取用、失窃、损毁、重现都需有来源和动作。只能使用人物当时能接触的证据，区分相似、证实和推断。无需提及与本章无关的线索，但关键未回收线索应继续留在摘要。
3. 人物与因果。保留伤病、权限、所知事实和信任状态，变化要有触发事件。先满足行动前置条件，再写结果。
只输出 JSON 对象，body 为完整正文字符串，summary 为不超过 450 字的连续性摘要字符串。摘要以正文为准，依次交接【时间】起止时点、关键事件和未到期时限，【人物】关键位置、状态和认知变化，【线索】编号、持有者、状态及未回收线索。简要保留影响后续的事实，禁止在摘要新增正文中没有的进展。
创作资料：
`

const reviewPrompt = `审查当前章草稿与设定、已定稿正文、前文摘要和本章目标是否一致。以已定稿正文为优先依据，单独核对本章摘要是否遗漏或改写了会影响后续的事实。
重点核对：
1. 对齐前章结尾、本章开场、明确时刻和截止时间。逐项核对时间先后、行动耗时、预定事件是否触发，以及记录的写成时间是否早于其中提到的事件。区分倒叙、预告和实际发生。
2. 追踪关键证物或信息的编号、来源、持有人、位置和状态。核对它是否已经被取走、损坏或移交，角色是否有机会看到或取得，线索相似能否支持结论。尚未到回收时机的伏笔可以延续，不能无故消失或变成既成事实。
3. 核对角色的行动限制、信息来源和信任变化，以及行动的必要条件是否先于结果。比较本章正文与连续性摘要中的时点、持有关系和未解决事项。
只报告能定位的问题，区分明确矛盾、缺少交代和可选润色，避免把猜测当事实。每条写明问题、所在章节或简短原文依据及最小修法。没有明确问题时直接说明。只输出 JSON 对象，review 为简体中文字符串数组，每条是一则完整意见，总长不超过 2000 字。
审稿资料：
`

const (
	recentChapterRunes     = 4000
	recentHistoryChapters  = 20
	sampledHistoryChapters = 10
	sampledSummaryRunes    = 220
)

type priorChapter struct {
	Number         int
	Title          string
	Summary        string
	FinalizedProse string `json:",omitempty"`
}

func priorChapters(p Project, before int, includeRecentProse bool) []priorChapter {
	prior := make([]priorChapter, 0, before)
	for i := range before {
		chapter := p.Chapters[i]
		if !chapter.Final {
			continue
		}
		entry := priorChapter{Number: i + 1, Title: p.Outline.Beats[i].Title, Summary: chapter.Summary}
		if includeRecentProse && before-i <= 2 {
			entry.FinalizedProse = excerptEnds(chapter.Body, recentChapterRunes, "\n【中间正文省略】\n")
		}
		prior = append(prior, entry)
	}
	if len(prior) <= recentHistoryChapters+sampledHistoryChapters {
		return prior
	}

	// 长篇保留近期的完整交接，并从较早的定稿中按顺序均匀抽样，覆盖开篇与最近阶段的衔接点。
	older, recent := prior[:len(prior)-recentHistoryChapters], prior[len(prior)-recentHistoryChapters:]
	history := make([]priorChapter, 0, sampledHistoryChapters+recentHistoryChapters)
	for sample := range sampledHistoryChapters {
		entry := older[sample*(len(older)-1)/(sampledHistoryChapters-1)]
		entry.Summary = excerptEnds(entry.Summary, sampledSummaryRunes, "【中间摘要省略】")
		history = append(history, entry)
	}
	return append(history, recent...)
}

func chapterContext(p Project, index int, instruction, review string) string {
	var next *Beat
	if index+1 < len(p.Outline.Beats) {
		next = &p.Outline.Beats[index+1]
	}
	return asJSON(struct {
		Title, Brief, Synopsis, World, Characters string
		ChapterNumber                             int
		CurrentBeat                               Beat
		NextBeat                                  *Beat `json:",omitempty"`
		PriorChapters                             []priorChapter
		Draft, DraftSummary, Review, Instruction  string
	}{
		p.Title, p.Brief, p.Outline.Synopsis, p.Outline.World, p.Outline.Characters,
		index + 1, p.Outline.Beats[index], next, priorChapters(p, index, true),
		p.Chapters[index].Body, p.Chapters[index].Summary, review, instruction,
	})
}

func excerptEnds(text string, limit int, omission string) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	part := (limit - len([]rune(omission))) / 2
	return string(runes[:part]) + omission + string(runes[len(runes)-part:])
}
