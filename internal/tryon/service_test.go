package tryon

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
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

func TestTryOnGarmentReferenceUsesEachItemInSelectionOrder(t *testing.T) {
	colors := []color.RGBA{{R: 235, A: 255}, {B: 235, A: 255}, {G: 235, A: 255}, {R: 225, G: 225, A: 255}}
	categories := []string{"外套", "上衣", "裤子", "包袋"}
	garments := make([]AssetImage, len(colors))
	for i, value := range colors {
		pixels := image.NewRGBA(image.Rect(0, 0, 16, 16))
		for y := 0; y < 16; y++ {
			for x := 0; x < 16; x++ {
				pixels.SetRGBA(x, y, value)
			}
		}
		var data bytes.Buffer
		if err := png.Encode(&data, pixels); err != nil {
			t.Fatal(err)
		}
		garments[i] = AssetImage{Asset: Asset{Category: categories[i]}, Image: Image{Data: data.Bytes(), MIME: "image/png"}}
	}
	ref, err := garmentReference(garments)
	if err != nil {
		t.Fatal(err)
	}
	board, _, err := image.Decode(bytes.NewReader(ref.Data))
	if err != nil {
		t.Fatal(err)
	}
	prompt := outfitTryOnPrompt(PersonReal, garments, true)
	for i, want := range colors {
		at := image.Pt((2*(i%2)+1)*board.Bounds().Dx()/4, (2*(i/2)+1)*board.Bounds().Dy()/4)
		got := color.RGBAModel.Convert(board.At(at.X, at.Y)).(color.RGBA)
		closeChannel := func(a, b uint8) bool { return int(a)-int(b) <= 5 && int(b)-int(a) <= 5 }
		if !closeChannel(got.R, want.R) || !closeChannel(got.G, want.G) || !closeChannel(got.B, want.B) {
			t.Fatalf("第 %d 件衣物在参考板中的位置不正确: %v", i+1, got)
		}
		if !strings.Contains(prompt, garmentCells[i]+"是"+categories[i]) {
			t.Fatalf("第 %d 件衣物的提示未对应参考板位置", i+1)
		}
	}
	if !strings.Contains(prompt, "图 3 只提供动作") || !strings.Contains(prompt, "外套穿在所选上衣外面") {
		t.Fatal("多件试穿未区分姿态和外套叠穿")
	}
}
