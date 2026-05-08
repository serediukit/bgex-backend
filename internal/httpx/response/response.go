package response

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ErrorCode is a stable machine-readable code for API errors.
type ErrorCode string

const (
	CodeInvalidRequest    ErrorCode = "invalid_request"
	CodeUnauthorized      ErrorCode = "unauthorized"
	CodeForbidden         ErrorCode = "forbidden"
	CodeNotFound          ErrorCode = "not_found"
	CodeConflict          ErrorCode = "conflict"
	CodeRateLimited       ErrorCode = "rate_limited"
	CodeInternal          ErrorCode = "internal_error"
	CodeInvalidCredential ErrorCode = "invalid_credentials"
	CodeTokenExpired      ErrorCode = "token_expired"
	CodeTokenInvalid      ErrorCode = "token_invalid"
)

type ErrorBody struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

type errorEnvelope struct {
	Error ErrorBody `json:"error"`
}

// OK writes a 200 with the payload JSON-serialized.
func OK(c *gin.Context, payload any) {
	c.JSON(http.StatusOK, payload)
}

// Created writes a 201 with the payload JSON-serialized.
func Created(c *gin.Context, payload any) {
	c.JSON(http.StatusCreated, payload)
}

// NoContent writes a 204.
func NoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

// Error writes a structured error response and aborts the request.
func Error(c *gin.Context, status int, code ErrorCode, msg string) {
	c.AbortWithStatusJSON(status, errorEnvelope{Error: ErrorBody{Code: code, Message: msg}})
}

// APIError is a typed error that handlers/middleware can return.
type APIError struct {
	Status  int
	Code    ErrorCode
	Message string
}

func (e *APIError) Error() string { return e.Message }

func NewAPIError(status int, code ErrorCode, msg string) *APIError {
	return &APIError{Status: status, Code: code, Message: msg}
}

// WriteAPIError writes an APIError, or falls back to 500 for unknown errors.
func WriteAPIError(c *gin.Context, err error) {
	if apiErr, ok := errors.AsType[*APIError](err); ok {
		Error(c, apiErr.Status, apiErr.Code, apiErr.Message)
		return
	}

	Error(c, http.StatusInternalServerError, CodeInternal, "internal error")
}
