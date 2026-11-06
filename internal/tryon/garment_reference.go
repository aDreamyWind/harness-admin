package tryon

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"strings"

	"golang.org/x/image/draw"
)

var garmentCells = [MaxTryOnGarments]string{"左上格", "右上格", "左下格", "右下格"}

// 将多件衣物放在独立格子里，让只支持三张参考图的模型也能同时读取人物、衣物和姿态。
func garmentReference(garments []AssetImage) (Image, error) {
	switch len(garments) {
	case 0:
		return Image{}, ErrNotFound
	case 1:
		return garments[0].Image, nil
	}
	if len(garments) > MaxTryOnGarments {
		return Image{}, invalid("试穿衣物超过四件")
	}
	const cell = 760
	const gutter = 20
	rows := (len(garments) + 1) / 2
	board := image.NewRGBA(image.Rect(0, 0, 2*cell+3*gutter, rows*cell+(rows+1)*gutter))
	draw.Draw(board, board.Bounds(), image.NewUniform(color.RGBA{R: 229, G: 232, B: 233, A: 255}), image.Point{}, draw.Src)
	for i, garment := range garments {
		if _, err := imageInfo(garment.Data, MaxInputBytes); err != nil {
			return Image{}, fmt.Errorf("衣物参考图无效: %w", err)
		}
		source, _, err := image.Decode(bytes.NewReader(garment.Data))
		if err != nil {
			return Image{}, fmt.Errorf("解码衣物参考图失败: %w", err)
		}
		left, top := gutter+(i%2)*(cell+gutter), gutter+(i/2)*(cell+gutter)
		tile := image.Rect(left, top, left+cell, top+cell)
		draw.Draw(board, tile, image.NewUniform(color.White), image.Point{}, draw.Src)
		size := source.Bounds().Size()
		scale := float64(cell-2*gutter) / float64(max(size.X, size.Y))
		w := max(1, int(math.Round(float64(size.X)*scale)))
		h := max(1, int(math.Round(float64(size.Y)*scale)))
		target := image.Rect(0, 0, w, h).Add(image.Pt(left+(cell-w)/2, top+(cell-h)/2))
		draw.CatmullRom.Scale(board, target, source, source.Bounds(), draw.Over, nil)
	}
	var data bytes.Buffer
	if err := jpeg.Encode(&data, board, &jpeg.Options{Quality: 92}); err != nil {
		return Image{}, fmt.Errorf("编码衣物参考板失败: %w", err)
	}
	return Image{Data: data.Bytes(), MIME: "image/jpeg"}, nil
}

func outfitTryOnPrompt(kind string, garments []AssetImage, pose bool) string {
	if len(garments) == 1 {
		return tryonPrompt(kind, garments[0].Category, pose)
	}
	style := "保留图 1 的插画画风、线条、配色方式和人物设定。"
	if kind == PersonReal {
		style = "保留图 1 的摄影质感、肤色和自然人体比例。"
	}
	action := "保持图 1 的姿势、镜头视角与构图。"
	if pose {
		action = "图 3 只提供动作、朝向和姿势，忽略其中的身份、衣物、画风和背景。"
	}
	var targets strings.Builder
	for i, garment := range garments {
		fmt.Fprintf(&targets, "%s是%s。", garmentCells[i], garment.Category)
	}
	return "生成一张虚拟试穿图。图 1 是唯一要保留的人物，保持五官、发型、身份特征和身材。" + style +
		"图 2 是本次服装参考板，每个有衣物的格子各是一件目标单品。" + targets.String() +
		"让图 1 的人物同时穿戴参考板中的每件衣物。分别保留每件的颜色、材质、图案、款式、长度和细节，不混合不同单品的纹理与版型。" +
		"若有外套和上衣，外套穿在所选上衣外面；若只选了外套，保留图 1 原内搭。按真实穿法安排其他单品和配饰。保留图 1 未被替换的衣物、配饰与背景。" +
		action + "只输出一张完整的人物试穿效果图，不输出参考板，不拼图，不加文字。"
}
