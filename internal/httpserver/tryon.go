package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"

	"harness-admin/internal/httpx"
	"harness-admin/internal/tryon"
)

type TryOnService interface {
	Models(context.Context, tryon.Owner) (tryon.Models, error)
	ListAssets(context.Context, tryon.Owner) ([]tryon.Asset, error)
	Asset(context.Context, tryon.Owner, string) (tryon.AssetImage, error)
	SaveAsset(context.Context, tryon.Owner, string, string, string, []byte, *tryon.Box) (tryon.Asset, error)
	DeleteAsset(context.Context, tryon.Owner, string) error
	PreviewAsset(context.Context, tryon.Owner, tryon.AssetPreviewInput, []byte) (tryon.Image, error)
	Detect(context.Context, tryon.Owner, string, []byte) ([]tryon.Candidate, error)
	StartGarmentJobs(context.Context, tryon.Owner, string, []tryon.GarmentJobItem, []byte) ([]tryon.GarmentJob, error)
	ListGarmentJobs(context.Context, tryon.Owner) ([]tryon.GarmentJob, error)
	GarmentJobResult(context.Context, tryon.Owner, string) (tryon.Image, error)
	SaveGarmentJob(context.Context, tryon.Owner, string) (tryon.Asset, error)
	RetryGarmentJob(context.Context, tryon.Owner, string) (tryon.GarmentJob, error)
	GeneratePerson(context.Context, tryon.Owner, tryon.PersonInput) (tryon.Asset, error)
	ListOutfits(context.Context, tryon.Owner) ([]tryon.Outfit, error)
	CreateOutfit(context.Context, tryon.Owner, tryon.OutfitInput) (tryon.Outfit, error)
	DeleteOutfit(context.Context, tryon.Owner, string) error
	ListJobs(context.Context, tryon.Owner) ([]tryon.Job, error)
	Start(context.Context, tryon.Owner, tryon.JobInput) (tryon.Job, error)
	Result(context.Context, tryon.Owner, string) (tryon.Image, error)
}

func registerTryOnRoutes(router *gin.Engine, deps Dependencies) {
	service := deps.TryOn
	group := router.Group("/api/v1/ai-tryon", requireSession(deps))
	write := func(c *gin.Context, status int, value any, err error) {
		if err != nil {
			writeTryOnError(c, deps, err)
			return
		}
		httpx.OK(c, status, value)
	}
	sendImage := func(c *gin.Context, image tryon.Image, err error) {
		if err != nil {
			writeTryOnError(c, deps, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Data(http.StatusOK, image.MIME, image.Data)
	}
	group.GET("/models", func(c *gin.Context) {
		items, err := service.Models(c.Request.Context(), tryOnOwner(c))
		write(c, http.StatusOK, items, err)
	})
	group.GET("/assets", func(c *gin.Context) {
		items, err := service.ListAssets(c.Request.Context(), tryOnOwner(c))
		write(c, http.StatusOK, items, err)
	})
	group.GET("/assets/:id/content", func(c *gin.Context) {
		asset, err := service.Asset(c.Request.Context(), tryOnOwner(c), c.Param("id"))
		sendImage(c, asset.Image, err)
	})
	group.DELETE("/assets/:id", requireCSRF(deps), func(c *gin.Context) {
		err := service.DeleteAsset(c.Request.Context(), tryOnOwner(c), c.Param("id"))
		write(c, http.StatusOK, gin.H{"deleted": true}, err)
	})
	group.POST("/assets", requireCSRF(deps), func(c *gin.Context) {
		data, fields, ok := readTryOnUpload(c)
		if !ok {
			return
		}
		box, ok := readTryOnBox(c, fields["box"])
		if !ok {
			return
		}
		result, err := service.SaveAsset(c.Request.Context(), tryOnOwner(c), fields["kind"], fields["name"], fields["category"], data, box)
		write(c, http.StatusCreated, result, err)
	})
	group.POST("/assets/preview", requireCSRF(deps), func(c *gin.Context) {
		data, fields, ok := readTryOnUpload(c)
		if !ok {
			return
		}
		box, ok := readTryOnBox(c, fields["box"])
		if !ok {
			return
		}
		input := tryon.AssetPreviewInput{
			Kind: fields["kind"], Name: fields["name"], Category: fields["category"],
			ModelCode: fields["modelCode"], Box: box,
		}
		result, err := service.PreviewAsset(c.Request.Context(), tryOnOwner(c), input, data)
		sendImage(c, result, err)
	})
	group.POST("/garments/detect", requireCSRF(deps), func(c *gin.Context) {
		data, fields, ok := readTryOnUpload(c)
		if !ok {
			return
		}
		items, err := service.Detect(c.Request.Context(), tryOnOwner(c), fields["modelCode"], data)
		write(c, http.StatusOK, items, err)
	})
	garmentJobs := group.Group("/garment-jobs", func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		c.Next()
	})
	garmentJobs.GET("", func(c *gin.Context) {
		jobs, err := service.ListGarmentJobs(c.Request.Context(), tryOnOwner(c))
		write(c, http.StatusOK, jobs, err)
	})
	garmentJobs.POST("", requireCSRF(deps), func(c *gin.Context) {
		data, fields, ok := readTryOnUpload(c)
		if !ok {
			return
		}
		items, ok := readTryOnGarmentJobItems(c, fields["items"])
		if !ok {
			return
		}
		jobs, err := service.StartGarmentJobs(c.Request.Context(), tryOnOwner(c), fields["modelCode"], items, data)
		write(c, http.StatusAccepted, jobs, err)
	})
	garmentJobs.GET("/:id/result", func(c *gin.Context) {
		image, err := service.GarmentJobResult(c.Request.Context(), tryOnOwner(c), c.Param("id"))
		sendImage(c, image, err)
	})
	garmentJobs.POST("/:id/save", requireCSRF(deps), func(c *gin.Context) {
		asset, err := service.SaveGarmentJob(c.Request.Context(), tryOnOwner(c), c.Param("id"))
		write(c, http.StatusCreated, asset, err)
	})
	garmentJobs.POST("/:id/retry", requireCSRF(deps), func(c *gin.Context) {
		job, err := service.RetryGarmentJob(c.Request.Context(), tryOnOwner(c), c.Param("id"))
		write(c, http.StatusAccepted, job, err)
	})
	group.POST("/people/generate", requireCSRF(deps), func(c *gin.Context) {
		var input tryon.PersonInput
		if !bindTryOnJSON(c, &input) {
			return
		}
		asset, err := service.GeneratePerson(c.Request.Context(), tryOnOwner(c), input)
		write(c, http.StatusCreated, asset, err)
	})
	group.GET("/outfits", func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		outfits, err := service.ListOutfits(c.Request.Context(), tryOnOwner(c))
		write(c, http.StatusOK, outfits, err)
	})
	group.POST("/outfits", requireCSRF(deps), func(c *gin.Context) {
		var input tryon.OutfitInput
		if !bindTryOnJSON(c, &input) {
			return
		}
		outfit, err := service.CreateOutfit(c.Request.Context(), tryOnOwner(c), input)
		write(c, http.StatusCreated, outfit, err)
	})
	group.DELETE("/outfits/:id", requireCSRF(deps), func(c *gin.Context) {
		err := service.DeleteOutfit(c.Request.Context(), tryOnOwner(c), c.Param("id"))
		write(c, http.StatusOK, gin.H{"deleted": true}, err)
	})
	group.GET("/jobs", func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		items, err := service.ListJobs(c.Request.Context(), tryOnOwner(c))
		write(c, http.StatusOK, items, err)
	})
	group.POST("/jobs", requireCSRF(deps), func(c *gin.Context) {
		var input tryon.JobInput
		if !bindTryOnJSON(c, &input) {
			return
		}
		job, err := service.Start(c.Request.Context(), tryOnOwner(c), input)
		write(c, http.StatusAccepted, job, err)
	})
	group.GET("/jobs/:id/result", func(c *gin.Context) {
		image, err := service.Result(c.Request.Context(), tryOnOwner(c), c.Param("id"))
		sendImage(c, image, err)
	})
}

func tryOnOwner(c *gin.Context) tryon.Owner {
	session := currentSession(c)
	return tryon.Owner{OrganizationID: session.User.OrganizationID, UserID: session.User.ID}
}

func bindTryOnJSON(c *gin.Context, input any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	if err := c.ShouldBindJSON(input); err != nil {
		httpx.Fail(c, http.StatusBadRequest, "invalid_tryon_input", "试衣请求格式无效或内容过长")
		return false
	}
	return true
}

func readTryOnBox(c *gin.Context, raw string) (*tryon.Box, bool) {
	if raw == "" {
		return nil, true
	}
	var coords []*float64
	if json.Unmarshal([]byte(raw), &coords) != nil || len(coords) != 4 || slices.Contains(coords, nil) {
		httpx.Fail(c, http.StatusBadRequest, "invalid_tryon_input", "衣物框选坐标无效")
		return nil, false
	}
	var box tryon.Box
	for i, coord := range coords {
		box[i] = *coord
	}
	return &box, true
}

func readTryOnGarmentJobItems(c *gin.Context, raw string) ([]tryon.GarmentJobItem, bool) {
	var fields []struct {
		Name     string          `json:"name"`
		Category string          `json:"category"`
		Box      json.RawMessage `json:"box"`
	}
	if json.Unmarshal([]byte(raw), &fields) != nil || len(fields) == 0 || len(fields) > tryon.MaxGarmentBatch {
		httpx.Fail(c, http.StatusBadRequest, "invalid_tryon_input", "请选择 1 至 8 件需要生成的衣物")
		return nil, false
	}
	items := make([]tryon.GarmentJobItem, 0, len(fields))
	for _, field := range fields {
		box, ok := readTryOnBox(c, string(field.Box))
		if !ok {
			return nil, false
		}
		items = append(items, tryon.GarmentJobItem{Name: field.Name, Category: field.Category, Box: box})
	}
	return items, true
}

func readTryOnUpload(c *gin.Context) ([]byte, map[string]string, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, tryon.MaxInputBytes+(32<<10))
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		httpx.Fail(c, http.StatusBadRequest, "invalid_tryon_input", "请上传一张不超过 6 MB 的图片")
		return nil, nil, false
	}
	defer c.Request.MultipartForm.RemoveAll()
	files := c.Request.MultipartForm.File["file"]
	if len(files) != 1 || files[0].Size <= 0 || files[0].Size > tryon.MaxInputBytes {
		httpx.Fail(c, http.StatusBadRequest, "invalid_tryon_input", "请上传一张不超过 6 MB 的图片")
		return nil, nil, false
	}
	file, err := files[0].Open()
	if err != nil {
		httpx.Fail(c, http.StatusBadRequest, "invalid_tryon_input", "无法读取上传的图片")
		return nil, nil, false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, tryon.MaxInputBytes+1))
	if err != nil || len(data) > tryon.MaxInputBytes {
		httpx.Fail(c, http.StatusBadRequest, "invalid_tryon_input", "无法读取上传的图片")
		return nil, nil, false
	}
	fields := make(map[string]string)
	for _, key := range []string{"name", "kind", "category", "box", "modelCode", "items"} {
		if values := c.Request.MultipartForm.Value[key]; len(values) > 0 {
			fields[key] = values[0]
		}
	}
	return data, fields, true
}

func writeTryOnError(c *gin.Context, deps Dependencies, err error) {
	switch {
	case errors.Is(err, tryon.ErrInvalid):
		httpx.Fail(c, http.StatusBadRequest, "invalid_tryon_input", err.Error())
	case errors.Is(err, tryon.ErrModel):
		httpx.Fail(c, http.StatusBadRequest, "tryon_model_unavailable", tryon.ErrModel.Error())
	case errors.Is(err, tryon.ErrNotFound):
		httpx.Fail(c, http.StatusNotFound, "tryon_not_found", tryon.ErrNotFound.Error())
	case errors.Is(err, tryon.ErrBuiltinAsset):
		httpx.Fail(c, http.StatusConflict, "tryon_builtin_asset", tryon.ErrBuiltinAsset.Error())
	case errors.Is(err, tryon.ErrBusy):
		httpx.Fail(c, http.StatusConflict, "tryon_busy", tryon.ErrBusy.Error())
	case errors.Is(err, tryon.ErrGarmentJobState):
		httpx.Fail(c, http.StatusConflict, "tryon_garment_job_state", tryon.ErrGarmentJobState.Error())
	case errors.Is(err, tryon.ErrGarmentJobLimit):
		httpx.Fail(c, http.StatusConflict, "tryon_garment_job_limit", tryon.ErrGarmentJobLimit.Error())
	case errors.Is(err, tryon.ErrDetectionTimeout):
		httpx.Fail(c, http.StatusGatewayTimeout, "tryon_detection_timeout", tryon.ErrDetectionTimeout.Error())
	case errors.Is(err, tryon.ErrDetection):
		httpx.Fail(c, http.StatusBadGateway, "tryon_detection_failed", tryon.ErrDetection.Error())
	case errors.Is(err, tryon.ErrInference):
		httpx.Fail(c, http.StatusBadGateway, "tryon_generation_failed", tryon.ErrInference.Error())
	default:
		deps.Logger.Error("AI 试衣请求失败", "request_id", contextString(c, "request_id"), "error", err)
		httpx.Fail(c, http.StatusInternalServerError, "tryon_failed", "AI 试衣服务暂时不可用，请稍后重试")
	}
}
