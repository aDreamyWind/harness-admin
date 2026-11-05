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

const resourceCausalityGuidance = `关键行动要核对所需资源的用途、来源、可用量和到位时点，并说明从取得资源到起效需要多久。明确区分应急与后续恢复、备料与实际起效。少量饮水或净水能力不能视为灌溉水源，取得种子也不能当成庄稼已经种成或眼前缺水已解决。若前提尚未满足，可以备料、试错或寻找资源，但应交代行动仍受阻，不能提前兑现结果。已有设定赋予特殊能力时，也要核对能力的适用条件和作用范围。
`

const outlineContinuityGuidance = resourceCausalityGuidance + `逐章规划进入状态、行动的必要条件、章末变化，以及留给下一章的任务。明确时限何时开始、何时触发和错过时的后果，留足行动所需的时间。重要线索标明首次出现、如何核验、何时回收。区分记录能证明的事实和人物推测，避免用事后信息推动此前行动。核对人物能力限制、地点和道具状态在各章之间的变化是否都有原因。
`

const outlinePrompt = `为这部中文小说制定大纲，规划恰好 %d 章。每章给出标题和具体情节目标。
` + outlineContinuityGuidance + outlineResponseFormat

const serialOutlineGuidance = `这是一部长篇连载。先用 synopsis 确定全书主线，写清主角的长期目标、核心冲突、后续阶段可以推进的方向，以及需要逐步揭开的伏笔。灵感即使包含全书结局，也只用于规划长线。把 beats 留给首个开篇事件，从主角卷入到做出有代价的选择，逐步展示世界规则。每章安排不同的具体行动，并改变人物处境、关系或掌握的信息。开篇末章给出局部结果和下一步，保留全书核心冲突及后续阶段的伏笔。
`

const serialOpeningPrompt = serialOutlineGuidance + `beats 数组必须包含 %d 至 %d 项。根据开篇事件的复杂程度和情节转折在此范围内选定首批章数，每项给出标题和具体情节目标。剩余情节留在全书主线，输出前核对 beats 的实际数量。
` + outlineContinuityGuidance + outlineResponseFormat

const repairSerialOpeningPrompt = `上一次生成的连载首批大纲有 %d 章。参考原始灵感、既有全书主线、世界设定、人物关系和上次的开篇计划，重新组织首个开篇事件，生成恰好 %d 章。保持事件的因果连贯和阶段结果，保留全书核心冲突及后续阶段的伏笔。
` + resourceCausalityGuidance + `
只返回一个 JSON 对象，仅包含 beats 数组，数组恰好有 %d 项。每项包含简体中文字符串 title 和 goal。输出前确认数组长度，避免简单丢弃尾章或用空章节凑数。
创作资料：%s`

func serialOpeningRepairPrompt(brief string, outline Outline, target int) string {
	material := asJSON(struct {
		Brief   string
		Outline Outline
	}{brief, outline})
	return fmt.Sprintf(repairSerialOpeningPrompt, len(outline.Beats), target, target, material)
}

const reviseOutlinePrompt = `按修改要求修订当前小说大纲，返回恰好 %d 章的完整修订稿。以当前大纲为底稿，对作者指出的问题及其直接影响的段落做必要修订，尽量逐字保留其他设定、人物与章节。原始灵感用于理解创作意图，修改要求明确纠正之处优先采用修改要求。
涉及历史纠正时，以作者给出的时代范围和依据为准，将修订同步到受影响的故事梗概、世界设定、人物和章节。对不确定的史实使用稳妥表述，避免捏造出处。保持章节顺序和数量，核对修订后的时间线、人物关系与线索承接。
` + resourceCausalityGuidance + outlineResponseFormat

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
		material := asJSON(struct {
			Title, Brief   string
			AuthorGuidance string `json:",omitempty"`
		}{p.Title, p.Brief, p.AuthorGuidance})
		if p.StoryMode == StoryModeSerial {
			if p.Outline == nil {
				return fmt.Sprintf(serialOpeningPrompt, minSerialOpeningChapters, maxSerialOpeningChapters, titlePrompt, material), nil
			}
			return serialOutlineGuidance + fmt.Sprintf(outlinePrompt, p.ChapterCount, titlePrompt, material), nil
		}
		return fmt.Sprintf(outlinePrompt, p.ChapterCount, titlePrompt, material), nil
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
		AuthorGuidance            string `json:",omitempty"`
		CurrentOutline            Outline
	}{p.Title, p.Brief, instruction, p.AuthorGuidance, draft}
	prompt := fmt.Sprintf(reviseOutlinePrompt, p.ChapterCount, titlePrompt, asJSON(material))
	if p.StoryMode == StoryModeSerial {
		prompt = serialOutlineGuidance + prompt
	}
	return prompt, nil
}

const serialWriteGuidance = `这是一部长篇连载。Synopsis 是全书主线和未来方向，正文只推进当前章目标。即使暂时没有下一章目标，也把它视为当前批的末章，保留长线冲突和未到回收时机的伏笔。明确要求写全书终章时才收束主线。
`

const authorContinuityGuidance = `AuthorGuidance 是全书写作约定，Instruction 是作者对本章的明确要求。称呼、语气等创作选择以作者要求为准，本章明确要求优先于一般约定。前文用过不同称呼，或本章目标建议用旧称呼，都不能推翻作者决定。保留定稿中已发生的事件、人物关系和线索状态，不把措辞调整误判为事实冲突。
`

const writePrompt = `撰写当前章的简体中文小说正文，目标约 1200 至 2000 字。以已定稿正文和摘要为连续性依据，当前章目标用于安排本章剧情，后续章目标只用于铺垫。定稿正文优先于摘要和大纲。若有现稿、审稿意见或修改要求，针对有证据的问题修改，保留无关情节。旧大纲中的行动若与已定稿事实冲突或缺少必要资源，保持人物目标，先补足前提、交代受阻或安排可执行的下一步，不强行按旧大纲写成。后续章节才取得的资源，本章不能提前用来达成目标。
` + resourceCausalityGuidance + `
输出前在内部复核并修正以下内容，无需输出检查过程：
1. 时间。核对开场承接点、事件发生与记录的先后、临近时限的行动耗时。若承诺的触发时点已经过去，在正文交代结果或为何未触发。回忆和现时场景要明确区分。
2. 线索。延续相关线索的名称或编号、持有人、存放地和状态。交接、取用、失窃、损毁、重现都需有来源和动作。只能使用人物当时能接触的证据，区分相似、证实和推断。无需提及与本章无关的线索，但关键未回收线索应继续留在摘要。
3. 人物与因果。保留伤病、权限、所知事实和信任状态，变化要有触发事件。先满足行动前置条件，再写结果。
只输出 JSON 对象。body 为完整正文字符串，最多 8000 字，即使正文不变也要完整返回，不能用「同上」省略。summary 为连续性摘要字符串，目标 250 至 350 字，最多 500 字。摘要以正文为准，依次交接【时间】起止时点、关键事件和未到期时限，【人物】关键位置、状态和认知变化，【线索】编号、持有者、状态及未回收线索。简要保留影响后续的事实，包括关键资源是否到位及计划是否已生效，禁止在摘要新增正文中没有的进展。
创作资料：
`

const reviewPrompt = `审查当前章草稿与设定、已定稿正文、前文摘要和本章目标是否一致。以已定稿正文为优先依据，单独核对本章摘要是否遗漏或改写了会影响后续的事实。
` + authorContinuityGuidance + resourceCausalityGuidance + `不要把较早章节未进入资料、旧摘要省略细节或资料不足当作矛盾。没有直接依据时不报告。
重点核对：
1. 对齐前章结尾、本章开场、明确时刻和截止时间。逐项核对时间先后、行动耗时、预定事件是否触发，以及记录的写成时间是否早于其中提到的事件。区分倒叙、预告和实际发生。
2. 追踪关键证物或信息的编号、来源、持有人、位置和状态。核对它是否已经被取走、损坏或移交，角色是否有机会看到或取得，线索相似能否支持结论。尚未到回收时机的伏笔可以延续，不能无故消失或变成既成事实。
3. 核对角色的行动限制、信息来源和信任变化，以及行动的必要条件是否先于结果。比较本章正文与连续性摘要中的时点、持有关系和未解决事项。
4. 若正文把当前不可执行的方案写作可立即救急，或把备料写成已起效，应报告为实质因果问题，引用显示资源不足或尚未到位的表述，以及夸大其效果的正文或摘要。若没有断供证据，但正文已经写成了结果且没有交代必要资源如何到位，应指出缺少交代，不推断资源一定不存在。合理的准备、明确受阻的尝试和后续计划本身不构成问题。若正文为遵守定稿事实而调整了不可执行的旧大纲，不要求照搬旧大纲。
只报告有原文依据、会影响理解或后续剧情衔接的实质问题。无需报告可选润色、个人偏好或尚未到回收时机的铺垫。按影响大小最多列出三项，每条写明具体原文依据及最小修法。没有必须修正的问题时返回 {"review":[]}，不要凑数。只输出 JSON 对象，review 为简体中文字符串数组。
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
		AuthorGuidance                            string `json:",omitempty"`
		ChapterNumber                             int
		CurrentBeat                               Beat
		NextBeat                                  *Beat `json:",omitempty"`
		PriorChapters                             []priorChapter
		Draft, DraftSummary, Review, Instruction  string
	}{
		p.Title, p.Brief, p.Outline.Synopsis, p.Outline.World, p.Outline.Characters, p.AuthorGuidance,
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
