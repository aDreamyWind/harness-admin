package httpserver

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"harness-admin/internal/drama"
	"harness-admin/internal/httpx"
)

func createDramaCanvasAsset(deps Dependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
		var in drama.CanvasAssetInput
		if err := c.ShouldBindJSON(&in); err != nil {
			handleDramaError(c, deps, drama.ErrInvalidInput)
			return
		}
		u := currentSession(c).User
		out, err := deps.DramaVisual.CreateCanvasAsset(c.Request.Context(), u.OrganizationID, u.ID, c.Param("id"), in)
		if err != nil {
			handleDramaError(c, deps, err)
			return
		}
		httpx.OK(c, http.StatusCreated, out)
	}
}
