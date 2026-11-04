package novel

import (
	"encoding/json"
	"testing"
)

func TestNormalizeTextParts(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  string
		want string
	}{
		{"旧版字符串", `"  未发现明显矛盾。 "`, "未发现明显矛盾。"},
		{"字符串数组", `[" 核对 23:30。 ","","交代钥匙去向。"]`, "核对 23:30。\n\n交代钥匙去向。"},
		{"结构化数组", `[{"suggestion":"补出交接动作","issue":"钥匙突然换人","severity":"严重","evidence":"第一章章末"},"核对摘要。"]`,
			"严重程度：严重\n问题：钥匙突然换人\n依据：第一章章末\n建议：补出交接动作\n\n核对摘要。"},
		{"空数组", `[]`, ""},
		{"空对象元素", `[{}]`, ""},
		{"空值", `null`, ""},
		{"格式错误", `["合法但不能保留", 5]`, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeTextParts([]byte(test.raw), 4000)
			if got != test.want || (err == nil) != (test.want != "") {
				t.Fatalf("格式化结果 %q，错误 %v，预期 %q", got, err, test.want)
			}
		})
	}
}

func TestParseReviewWithModelPreambleAndTrailingNotes(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want string
	}{
		{"```json\n{\"review\":[\"证物仍在唐照手中。\"]}\n```\n以上为审稿意见。", "证物仍在唐照手中。"},
		{`{"review":"核对时间。"}以上为审稿意见。`, "核对时间。"},
		{`["核对持有人。"]`, "核对持有人。"},
		{`"核对时间。"`, "核对时间。"},
		{`{"review":"核对时间。"}{"review":"第二份答案"}`, ""},
		{`{"review":"核对时间。"} null`, ""},
		{`"核对时间。" "另一份答案。" 以上。`, ""},
		{`{"review":["核对时间。"`, ""},
	} {
		var output json.RawMessage
		err := decodeModelOutput(test.raw, &output)
		got := ""
		if err == nil {
			got, err = parseReview(output)
		}
		if got != test.want || (err == nil) != (test.want != "") {
			t.Errorf("输入 %q，得到 %q，错误 %v，预期 %q", test.raw, got, err, test.want)
		}
	}
}
