package tryon

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

type AssetPreviewInput struct {
	Kind, Name, Category, ModelCode string
	Box                             *Box
}

const defaultAnimePersonStyle = "精细的现代日系动漫画风，线条有粗细变化，发丝、五官和布料褶皱清晰，上色和赛璐璐光影有层次。避免通用扁平卡通模板和塑料质感。"

// PreviewAsset 只生成入库候选，用户确认后再通过 SaveAsset 保存。
func (s *Service) PreviewAsset(ctx context.Context, owner Owner, in AssetPreviewInput, data []byte) (Image, error) {
	operation, prompt, err := assetPreviewPrompt(in)
	if err != nil {
		return Image{}, err
	}
	if !validModelCode(in.ModelCode) {
		return Image{}, ErrModel
	}
	source, err := prepareImage(data, nil)
	if err != nil {
		return Image{}, err
	}
	refs := []Image{source}
	if in.Box != nil {
		target, err := prepareImage(data, in.Box)
		if err != nil {
			return Image{}, err
		}
		refs = append(refs, target)
	}

	ctx, cancel := context.WithTimeout(ctx, jobTimeout)
	defer cancel()
	generated, err := s.engine.Generate(ctx, owner, in.ModelCode, operation, prompt, refs)
	if err != nil {
		return Image{}, err
	}
	preview, err := prepareGeneratedAsset(generated.Data)
	if err != nil {
		return Image{}, fmt.Errorf("%w：%v", ErrInference, err)
	}
	return preview, nil
}

func assetPreviewPrompt(in AssetPreviewInput) (operation string, prompt string, err error) {
	if in.Kind != Garment && (in.Box != nil || in.Category != "") {
		return "", "", invalid("只有衣物可以指定分类和框选区域")
	}
	switch in.Kind {
	case Garment:
		name := strings.TrimSpace(in.Name)
		if !ValidCategory(in.Category) {
			return "", "", invalid("请选择衣物分类")
		}
		if utf8.RuneCountInString(name) > 80 || strings.ContainsRune(name, 0) {
			return "", "", invalid("衣物名称最多 80 字")
		}
		target := "图 1 是原始衣物参考图。目标品类为" + in.Category + "。"
		if name != "" {
			target += fmt.Sprintf("目标名称为 %q，仅用于定位单品，细节以图片为准。", name)
		}
		if in.Box != nil {
			if !in.Box.Valid() {
				return "", "", invalid("请框选有效的衣物区域")
			}
			target += "图 2 是目标区域的定位特写，只选图 2 主体对应的那件衣物。用图 1 里的完整上下文确认可见细节。"
		}
		return "garment_reconstruct", target +
			"重新生成一张目标单品的完整正面商品图。衣物平铺、居中、四周留白，所有边缘完整入镜。准确保留原图可见的版型、颜色、面料纹理、印花、领口、袖口、口袋和门襟。结合可见结构合理补全遮挡或画面外的部分，不增添未经参考支持的装饰、字样或标识。使用浅色纯背景，画面只出现目标单品，鞋履按一双展示。去除原人物、人体、衣架、其他衣物、道具和背景。输出单张图片，不拼图、不添加说明。", nil
	case Pose:
		return "pose_standardize",
			"将参考图中的主要人物动作重新绘制成一张通用全身姿态参考图。使用统一的哑光浅灰色关节人台，头部光滑无脸、无头发，身体无身份与服饰特征。严格保留动作、重心、四肢弯曲角度、手脚相对位置、身体朝向和原镜头视角。被遮挡关节按动作合理连接。仅一个人台，头到脚完整入镜，纯白背景，清楚分辨各肢体。不复制原图中的人物身份、衣服、背景或道具，不加文字、不拼图。", nil
	case Person2D, PersonReal:
		style := "生成动漫角色图。参考图已有明确的动漫画风时，保留原角色设计、服饰与配饰特征，以及线条、上色和光影。参考图是照片、草图或没有明确画风时，在保留角色特征的基础上采用" + defaultAnimePersonStyle
		if in.Kind == PersonReal {
			style = "输出自然的写实摄影风格，保持真实肤色、脸部特征和人体比例。"
		}
		return "person_reconstruct",
			"根据参考图重新生成同一人物的全身试衣底图。保留五官、脸型、发型、体型和能区分身份的特征，仅出现参考图中的主要人物。" +
				style + "人物正面自然站立，手臂稍离身体，头到脚完整入镜。保留可见的衣着，合理补全被遮挡或未入镜的部分。使用浅色纯背景，移除其他人物、道具和杂物。输出一张独立人物图，不加文字、不拼图。", nil
	default:
		return "", "", invalid("素材类型不受支持")
	}
}
