package drama

import (
	"context"
	"fmt"
	"strings"
)

// 业务层只描述任务角色和观测事件；供应商参数由适配器决定。
type ModelCallOptions struct {
	Stage      string
	OnText     func(string) error
	OnProgress func(ModelProgress) error
}
type ModelProgress struct {
	Phase           string
	ElapsedMS       int64
	ReasoningTokens *int
	Policy          string
}
type ObservedWriter interface {
	CompleteObserved(context.Context, string, string, string, string, ModelCallOptions) (string, error)
}

func (p ModelProgress) Description() string {
	label := map[string]string{"sent": "请求已发出，等待供应商响应", "headers": "已收到响应头，等待模型输出", "thinking": "模型正在思考", "content": "模型正在生成正文", "finished": "模型生成结束", "recorded": "调用日志已保存"}[p.Phase]
	if label == "" {
		label = p.Phase
	}
	detail := ""
	if p.Phase == "sent" && p.Policy != "" {
		detail = "；" + p.Policy
	}
	if p.Phase == "finished" {
		detail = "；供应商未报告思考用量"
		if p.ReasoningTokens != nil {
			detail = fmt.Sprintf("；思考用量%d Token", *p.ReasoningTokens)
		}
	}
	return fmt.Sprintf("%s；本次调用已用%.1f秒%s", label, float64(p.ElapsedMS)/1000, detail)
}
func ModelReasoningRole(stage string) string {
	for _, word := range []string{"name", "coordinator", "命名", "中心调度", "格式修正"} {
		if stage == word || strings.Contains(stage, word) && len([]rune(word)) > 3 {
			return "light"
		}
	}
	switch stage {
	case "art", "story", "context", "plan", "review", "repair":
		return "balanced"
	}
	if strings.Contains(stage, "审阅") || strings.Contains(stage, "导演规划") {
		return "balanced"
	}
	return "creative"
}
