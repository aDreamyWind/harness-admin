package drama

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestSegmentVideoUsesPinnedImageAndCurrentImageOnlyWhenUnpinned(t *testing.T) {
	seg := StoryboardSegment{Scene: "小院", FormIDs: []string{"scene", "actor"},
		Bindings: []StoryboardBinding{{FormID: "scene", ImageID: "adopted"}},
		Shots: []StoryboardShot{{Description: "小院里有人", Duration: 4.5, DescriptionMentions: []StoryboardMention{{Start: 0, End: 2, FormID: "scene"}},
			Utterances: []StoryboardUtterance{{Kind: "dialogue", Speaker: "林夏", Text: "你好。"}, {Kind: "vo", Delivery: "visual", SourceText: "这句已改为画面"}}}},
	}
	forms := []AssetForm{{ID: "scene", AssetName: "小院子", CurrentImageID: "newer"}, {ID: "actor", AssetName: "林夏"}}
	if _, err := planSegmentVideo(seg, forms, "水墨", "9:16"); !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), "林夏") {
		t.Fatalf("应提示具体缺图：%v", err)
	}
	forms[1].CurrentImageID = "actor-image"
	request, err := planSegmentVideo(seg, forms, "水墨", "9:16")
	if err != nil || request.Duration != 5 || !reflect.DeepEqual(request.ImageIDs, []string{"adopted", "actor-image"}) ||
		!strings.Contains(request.Prompt, "小院（图1）里有人") || !strings.Contains(request.Prompt, "你好。") || strings.Contains(request.Prompt, "这句已改为画面") {
		t.Fatalf("视频参数与分镜不符：%+v，%v", request, err)
	}
	seg.Shots[0].Duration = 30.5
	if _, err := planSegmentVideo(seg, forms, "水墨", "9:16"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("超长片段应被拒绝：%v", err)
	}
}
