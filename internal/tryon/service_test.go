package tryon

import (
	"strings"
	"testing"
)

func TestTryOnOuterwearPreservesLayersAndHem(t *testing.T) {
	prompt := tryonPrompt(Person2D, "外套", false)
	for _, constraint := range []string{"套在图 1 原有上衣外面", "保留原内搭", "下摆与袖口的相对高度以图 2 为准", "保持图 1 的姿势"} {
		if !strings.Contains(prompt, constraint) {
			t.Errorf("外套试穿缺少约束 %q", constraint)
		}
	}
	if prompt := tryonPrompt(PersonReal, "连衣裙", true); strings.Contains(prompt, "套在图 1 原有上衣外面") || !strings.Contains(prompt, "图 3 只提供动作") {
		t.Errorf("连衣裙试穿混用了外套或姿势约束")
	}
}
