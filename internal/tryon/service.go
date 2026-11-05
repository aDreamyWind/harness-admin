package tryon

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	Person2D       = "person_2d"
	PersonReal     = "person_real"
	Garment        = "garment"
	Pose           = "pose"
	MaxInputBytes  = 6 << 20
	MaxResultBytes = 20 << 20
	jobTimeout     = 6 * time.Minute
)

var (
	ErrInvalid   = errors.New("试衣请求无效")
	ErrNotFound  = errors.New("找不到这项试衣资料")
	ErrBusy      = errors.New("已有试穿正在生成，请等待完成")
	ErrModel     = errors.New("请选择可用的模型，并检查模型权限")
	ErrInference = errors.New("模型未能完成请求，请查看模型调用日志后重试")
	ErrDetection = errors.New("无法读取衣物识别结果，可以手动框选")
)

type Owner struct{ OrganizationID, UserID string }

type Asset struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	Category  string    `json:"category,omitempty"`
	Builtin   bool      `json:"builtin,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type Image struct {
	Data []byte
	MIME string
}

func (im Image) DataURI() string {
	return "data:" + im.MIME + ";base64," + base64.StdEncoding.EncodeToString(im.Data)
}

type AssetImage struct {
	Asset
	Image
}

type Model struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type Models struct {
	Detection []Model `json:"detection"`
	Image     []Model `json:"image"`
	Person    []Model `json:"person"`
}

type Job struct {
	ID        string    `json:"id"`
	PersonID  string    `json:"personId"`
	GarmentID string    `json:"garmentId"`
	PoseID    string    `json:"poseId,omitempty"`
	ModelCode string    `json:"modelCode"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type JobInput struct {
	PersonID  string `json:"personId"`
	GarmentID string `json:"garmentId"`
	PoseID    string `json:"poseId"`
	ModelCode string `json:"modelCode"`
}

type PersonInput struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
	ModelCode   string `json:"modelCode"`
}

type Repository interface {
	ListAssets(context.Context, Owner) ([]Asset, error)
	SaveAsset(context.Context, Owner, AssetImage) (Asset, error)
	Asset(context.Context, Owner, string) (AssetImage, error)
	ListJobs(context.Context, Owner) ([]Job, error)
	CreateJob(context.Context, Owner, Job, time.Time, []AssetImage) (Job, error)
	FinishJob(context.Context, Owner, string, Image, string) error
	Result(context.Context, Owner, string) (Image, error)
}

type Engine interface {
	Models(context.Context, Owner) (Models, error)
	Detect(context.Context, Owner, string, Image) ([]Candidate, error)
	Generate(context.Context, Owner, string, string, string, []Image) (Image, error)
}

type Service struct {
	repo   Repository
	engine Engine
	logger *slog.Logger
}

func NewService(repo Repository, engine Engine, logger *slog.Logger) *Service {
	return &Service{repo: repo, engine: engine, logger: logger}
}

func (s *Service) Models(ctx context.Context, owner Owner) (Models, error) {
	return s.engine.Models(ctx, owner)
}
func (s *Service) ListAssets(ctx context.Context, owner Owner) ([]Asset, error) {
	items, err := s.repo.ListAssets(ctx, owner)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]int, len(items))
	for i, asset := range items {
		byID[asset.ID] = i
	}
	for _, p := range presets {
		asset := p.asset(owner)
		if i, exists := byID[asset.ID]; exists {
			items[i].Builtin = true
		} else {
			items = append(items, asset)
		}
	}
	return items, nil
}
func (s *Service) Asset(ctx context.Context, owner Owner, id string) (AssetImage, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return AssetImage{}, ErrNotFound
	}
	id = parsed.String()
	stored, err := s.repo.Asset(ctx, owner, id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return AssetImage{}, err
	}
	for _, p := range presets {
		asset := p.asset(owner)
		if asset.ID != id {
			continue
		}
		// 已被试穿记录引用的图片沿用当时保存的素材版本。
		if err == nil {
			stored.Builtin = true
			return stored, nil
		}
		image, readErr := p.image()
		return AssetImage{Asset: asset, Image: image}, readErr
	}
	return stored, err
}

func isPerson(kind string) bool { return kind == Person2D || kind == PersonReal }

func invalid(message string) error { return fmt.Errorf("%w：%s", ErrInvalid, message) }

func assetFields(kind, name, category string) (Asset, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 80 || strings.ContainsRune(name, 0) {
		return Asset{}, invalid("请填写 1 至 80 字的名称")
	}
	if !isPerson(kind) && kind != Garment && kind != Pose {
		return Asset{}, invalid("素材类型不受支持")
	}
	if kind == Garment {
		if !ValidCategory(category) {
			return Asset{}, invalid("请选择衣物分类")
		}
	} else if category != "" {
		return Asset{}, invalid("只有衣物可以设置分类")
	}
	return Asset{ID: uuid.NewString(), Kind: kind, Name: name, Category: category}, nil
}

func (s *Service) SaveAsset(ctx context.Context, owner Owner, kind, name, category string, data []byte, box *Box) (Asset, error) {
	meta, err := assetFields(kind, name, category)
	if err != nil {
		return Asset{}, err
	}
	if box != nil && kind != Garment {
		return Asset{}, invalid("只有衣物可以框选裁剪")
	}
	im, err := prepareImage(data, box)
	if err != nil {
		return Asset{}, err
	}
	return s.repo.SaveAsset(ctx, owner, AssetImage{Asset: meta, Image: im})
}

func (s *Service) Detect(ctx context.Context, owner Owner, model string, data []byte) ([]Candidate, error) {
	if !validModelCode(model) {
		return nil, ErrModel
	}
	im, err := prepareImage(data, nil)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	return s.engine.Detect(ctx, owner, model, im)
}

func (s *Service) GeneratePerson(ctx context.Context, owner Owner, in PersonInput) (Asset, error) {
	meta, err := assetFields(in.Kind, in.Name, "")
	if err != nil {
		return Asset{}, err
	}
	desc := strings.TrimSpace(in.Description)
	if !isPerson(in.Kind) || desc == "" || utf8.RuneCountInString(desc) > 1000 || strings.ContainsRune(desc, 0) {
		return Asset{}, invalid("请选择人物类型并填写 1 至 1000 字的人物描述")
	}
	if !validModelCode(in.ModelCode) {
		return Asset{}, ErrModel
	}
	style := "清晰的二维插画，保持所描述的画风"
	if in.Kind == PersonReal {
		style = "真实摄影风格，自然光线和真实人体比例"
	}
	prompt := "生成一张可用于服装试穿的单人人物全身底图。" + style + "。人物正面自然站立，手臂与身体稍分开，头和脚完整可见，穿着简洁的日常衣服，使用单色背景，不加文字或边框。人物描述：" + desc
	ctx, cancel := context.WithTimeout(ctx, jobTimeout)
	defer cancel()
	im, err := s.engine.Generate(ctx, owner, in.ModelCode, "person_generate", prompt, nil)
	if err != nil {
		return Asset{}, err
	}
	im, err = prepareImage(im.Data, nil)
	if err != nil {
		return Asset{}, errors.Join(ErrInference, err)
	}
	return s.repo.SaveAsset(ctx, owner, AssetImage{Asset: meta, Image: im})
}

func (s *Service) ListJobs(ctx context.Context, owner Owner) ([]Job, error) {
	return s.repo.ListJobs(ctx, owner)
}

func (s *Service) Start(ctx context.Context, owner Owner, in JobInput) (Job, error) {
	if !validModelCode(in.ModelCode) {
		return Job{}, ErrModel
	}
	person, err := s.Asset(ctx, owner, in.PersonID)
	if err != nil {
		return Job{}, err
	}
	garment, err := s.Asset(ctx, owner, in.GarmentID)
	if err != nil {
		return Job{}, err
	}
	if !isPerson(person.Kind) || garment.Kind != Garment {
		return Job{}, invalid("请选择一张人物图和一张衣物图")
	}
	assets := []AssetImage{person, garment}
	if in.PoseID != "" {
		pose, err := s.Asset(ctx, owner, in.PoseID)
		if err != nil {
			return Job{}, err
		}
		if pose.Kind != Pose {
			return Job{}, invalid("请选择姿势参考图")
		}
		assets = append(assets, pose)
		in.PoseID = pose.ID
	}
	models, err := s.engine.Models(ctx, owner)
	if err != nil {
		return Job{}, err
	}
	allowed := false
	for _, model := range models.Image {
		if model.Code == in.ModelCode {
			allowed = true
			break
		}
	}
	if !allowed {
		return Job{}, ErrModel
	}
	refs := make([]Image, 0, len(assets))
	var builtinAssets []AssetImage
	for _, asset := range assets {
		refs = append(refs, asset.Image)
		if asset.Builtin {
			builtinAssets = append(builtinAssets, asset)
		}
	}
	job, err := s.repo.CreateJob(ctx, owner, Job{
		ID: uuid.NewString(), PersonID: person.ID, GarmentID: garment.ID, PoseID: in.PoseID, ModelCode: in.ModelCode,
	}, time.Now().Add(jobTimeout+30*time.Second), builtinAssets)
	if err != nil {
		return Job{}, err
	}
	go s.run(context.WithoutCancel(ctx), owner, job, tryonPrompt(person.Kind, garment.Category, in.PoseID != ""), refs)
	return job, nil
}

func (s *Service) run(parent context.Context, owner Owner, job Job, prompt string, refs []Image) {
	ctx, cancel := context.WithTimeout(parent, jobTimeout)
	defer cancel()
	im, err := s.engine.Generate(ctx, owner, job.ModelCode, "tryon_generate", prompt, refs)
	if err == nil {
		_, err = imageInfo(im.Data, MaxResultBytes)
	}
	message := ""
	if err != nil {
		message = ErrInference.Error()
		if errors.Is(err, ErrModel) {
			message = ErrModel.Error()
		} else if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			message = "试穿生成超时，请重试"
		}
		im = Image{}
		s.logger.Warn("AI 试穿生成失败", "job_id", job.ID, "error", err)
	}
	saveCtx, saveCancel := context.WithTimeout(parent, 10*time.Second)
	defer saveCancel()
	if err := s.repo.FinishJob(saveCtx, owner, job.ID, im, message); err != nil {
		s.logger.Error("保存 AI 试穿任务状态失败", "job_id", job.ID, "error", err)
	}
}

func (s *Service) Result(ctx context.Context, owner Owner, id string) (Image, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return Image{}, ErrNotFound
	}
	return s.repo.Result(ctx, owner, parsed.String())
}

func validModelCode(code string) bool {
	return code != "" && len(code) <= 120 && strings.TrimSpace(code) == code
}

func tryonPrompt(kind, category string, pose bool) string {
	style := "保留图 1 的插画画风、线条、配色方式和人物设定。"
	if kind == PersonReal {
		style = "保留图 1 的摄影质感、肤色和自然人体比例。"
	}
	action := "保持图 1 的姿势、镜头视角与构图。"
	if pose {
		action = "图 3 只提供动作、朝向与姿势。忽略图 3 的人物身份、衣物、画风和背景。"
	}
	layering := ""
	if category == "外套" {
		layering = "把外套套在图 1 原有上衣外面。领口和门襟开口露出的部分保留原内搭的领型、颜色和纹理，不增加裸露皮肤。外套下摆与袖口的相对高度以图 2 为准，不随原有裙裤的长度延伸。"
	}
	return "生成一张虚拟试穿图。图 1 是要保留的唯一人物，保持其五官、发型、身份特征和身材。" + style +
		"图 2 是要试穿的" + category + "。按图 2 的颜色、材质、图案和版型穿戴，保持领型、袖长、下摆位置、门襟开合以及口袋和纽扣的数量及位置。若图 2 有其他人物，忽略其身份和未指定的衣物。" +
		layering + action + "保留图 1 的背景和未被目标衣物遮挡的其他衣物、配饰。输出一张完整的人物试穿效果图，不拼图、不标注文字。"
}
