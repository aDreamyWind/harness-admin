package drama

import "encoding/json"

// 导演仍收到本集完整原文。提示词只列事实ID和简述；逐字引用保存在后台证据链中。
func directorPlan(p EpisodePlan) any {
	type fact struct {
		ID    string `json:"id"`
		Claim string `json:"claim"`
	}
	facts := make([]fact, 0, len(p.Sources))
	for _, f := range p.Sources {
		facts = append(facts, fact{f.ID, f.Claim})
	}
	return struct {
		Title     string   `json:"title"`
		Hook      string   `json:"hook"`
		StartUnit int      `json:"startUnit"`
		EndUnit   int      `json:"endUnit"`
		FactIDs   []string `json:"factIds"`
		Facts     []fact   `json:"facts"`
	}{p.Title, p.Hook, p.StartUnit, p.EndUnit, p.FactIDs, facts}
}
func batchNamingContext(material string) (string, error) {
	var plan episodePlanResult
	if err := json.Unmarshal([]byte(material), &plan); err != nil {
		return "", err
	}
	type summary struct {
		Title    string `json:"title"`
		Synopsis string `json:"synopsis"`
		Hook     string `json:"hook"`
	}
	items := make([]summary, 0, len(plan.Episodes))
	for _, p := range plan.Episodes {
		items = append(items, summary{p.Title, p.Synopsis, p.Hook})
	}
	raw, err := json.Marshal(items)
	return string(raw), err
}
