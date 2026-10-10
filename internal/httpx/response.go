package httpx

import "github.com/gin-gonic/gin"

type Response struct {
	Data  any        `json:"data,omitempty"`
	Error *ErrorBody `json:"error,omitempty"`
}

type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
}

func OK(ctx *gin.Context, status int, data any) {
	ctx.JSON(status, Response{Data: data})
}

func Fail(ctx *gin.Context, status int, code, message string) {
	requestID, _ := ctx.Get("request_id")
	ctx.AbortWithStatusJSON(status, Response{Error: &ErrorBody{
		Code:      code,
		Message:   message,
		RequestID: stringValue(requestID),
	}})
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}
