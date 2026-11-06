package httpserver

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"harness-admin/internal/coverimage"
)

func readCoverUpload(c *gin.Context) ([]byte, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, coverimage.MaxBytes+64*1024)
	reader, err := c.Request.MultipartReader()
	if err != nil {
		return nil, coverimage.ErrInvalidUpload
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "file" || part.FileName() == "" {
		return nil, coverimage.ErrInvalidUpload
	}
	data, err := io.ReadAll(io.LimitReader(part, coverimage.MaxBytes+1))
	if err != nil || len(data) == 0 || len(data) > coverimage.MaxBytes {
		return nil, coverimage.ErrInvalidUpload
	}
	if _, err := reader.NextPart(); err != io.EOF {
		return nil, coverimage.ErrInvalidUpload
	}
	return data, nil
}
