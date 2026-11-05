package tryon

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// 检测规则移植自「服装设计」的 lib/detection.mjs，扩充试衣所需的品类。
const DetectionPrompt = `你是服饰设计助理。识别照片中肉眼可见、可以单独选择的衣物。上衣与裤子必须分成两件。叠穿时只列出边界可以辨认的单件。不要列出人体、头发、背景、文字或影子，不要猜测照片外或遮挡下的物品。不确定时省略。每件给简短中文名称、类别和包含完整可见部分的矩形范围。类别只能是：上衣、外套、裤子、半裙、连衣裙、鞋履、包袋、首饰、其他配饰。坐标以整张图左上角为原点，横纵均取 0 至 1000 的整数。最多 8 件，只返回 JSON 对象，例如 {"items":[{"name":"蓝色衬衫","category":"上衣","box":[100,200,500,800]}]}。没有候选时返回 {"items":[]}。`

type Box [4]float64

type Candidate struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Box      Box    `json:"box"`
}

func ValidCategory(category string) bool {
	switch category {
	case "上衣", "外套", "裤子", "半裙", "连衣裙", "鞋履", "包袋", "首饰", "其他配饰":
		return true
	default:
		return false
	}
}

func (b Box) Valid() bool {
	for _, v := range b {
		if math.IsNaN(v) || v < 0 || v > 1 {
			return false
		}
	}
	return b[2]-b[0] >= .008 && b[3]-b[1] >= .008
}

func normalizeBox(coords []float64) (Box, bool) {
	if len(coords) != 4 {
		return Box{}, false
	}
	scale := 1.0
	for _, n := range coords {
		if n > 1 {
			scale = 1000
		}
	}
	var box Box
	for i, n := range coords {
		box[i] = n / scale
	}
	return box, box.Valid()
}

func overlap(a, b Box) float64 {
	w := max(0, min(a[2], b[2])-max(a[0], b[0]))
	h := max(0, min(a[3], b[3])-max(a[1], b[1]))
	intersection := w * h
	return intersection / ((a[2]-a[0])*(a[3]-a[1]) + (b[2]-b[0])*(b[3]-b[1]) - intersection)
}

func ParseDetection(content string) ([]Candidate, error) {
	start, end := strings.IndexByte(content, '{'), strings.LastIndexByte(content, '}')
	if start < 0 || end < start {
		return nil, ErrDetection
	}
	var result struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal([]byte(content[start:end+1]), &result); err != nil || result.Items == nil {
		return nil, fmt.Errorf("%w：模型未返回有效的候选列表", ErrDetection)
	}
	candidates := make([]Candidate, 0, 8)
	for _, raw := range result.Items[:min(16, len(result.Items))] {
		var item struct {
			Name     string    `json:"name"`
			Category string    `json:"category"`
			Box      []float64 `json:"box"`
			BBox     []float64 `json:"bbox"`
		}
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		if item.Box == nil {
			item.Box = item.BBox
		}
		box, valid := normalizeBox(item.Box)
		name := strings.TrimSpace(item.Name)
		if !valid || name == "" || utf8.RuneCountInString(name) > 80 || strings.ContainsRune(name, 0) || !ValidCategory(item.Category) {
			continue
		}
		duplicate := false
		for _, previous := range candidates {
			if previous.Category == item.Category && overlap(previous.Box, box) > .7 {
				duplicate = true
				break
			}
		}
		if !duplicate {
			candidates = append(candidates, Candidate{Name: name, Category: item.Category, Box: box})
		}
		if len(candidates) == 8 {
			break
		}
	}
	return candidates, nil
}
