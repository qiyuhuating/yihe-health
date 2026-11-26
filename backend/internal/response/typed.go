package response

import (
	"errors"
	"net/http"
)

// BaseResponse preserves the existing flat JSON response fields.
type BaseResponse struct {
	ReturnCode string `json:"returnCode"`
	Code       string `json:"code,omitempty"`
	Message    string `json:"message,omitempty"`
	Reference  string `json:"reference,omitempty"`
}

// Envelope is for endpoints whose public contract explicitly uses a data field.
// Existing flat endpoints must use WriteStruct with their own DTO instead.
type Envelope[T any] struct {
	BaseResponse
	Data T `json:"data"`
}

// AppError makes the HTTP status and domain error code explicit.
type AppError struct {
	HTTPStatus int
	Code       string
	Message    string
	Reference  string
	Err        error
}

func (e *AppError) Error() string { return e.Message }
func (e *AppError) Unwrap() error { return e.Err }

func BadRequest(msg string, err error) *AppError {
	return &AppError{HTTPStatus: http.StatusBadRequest, Message: msg, Err: err}
}
func Unauthorized(msg string, err error) *AppError {
	return &AppError{HTTPStatus: http.StatusUnauthorized, Message: msg, Err: err}
}
func Forbidden(msg string, err error) *AppError {
	return &AppError{HTTPStatus: http.StatusForbidden, Message: msg, Err: err}
}
func NotFound(msg string, err error) *AppError {
	return &AppError{HTTPStatus: http.StatusNotFound, Message: msg, Err: err}
}
func Conflict(msg string, err error) *AppError {
	return &AppError{HTTPStatus: http.StatusConflict, Message: msg, Err: err}
}
func Internal(msg string, err error) *AppError {
	return &AppError{HTTPStatus: http.StatusInternalServerError, Message: msg, Err: err}
}

// WriteStruct emits the supplied public DTO and preserves its exact status.
func WriteStruct[T any](w http.ResponseWriter, status int, payload T) {
	writeJSON(w, status, payload)
}

// WriteOK is only for endpoints with an explicit {returnCode,data} contract.
func WriteOK[T any](w http.ResponseWriter, data T) {
	writeJSON(w, http.StatusOK, Envelope[T]{BaseResponse: BaseResponse{ReturnCode: CodeOK}, Data: data})
}

func WriteError(w http.ResponseWriter, err error) {
	var appErr *AppError
	if !errors.As(err, &appErr) {
		code, message := errorDefaults(http.StatusInternalServerError)
		writeJSON(w, http.StatusInternalServerError, BaseResponse{ReturnCode: CodeFail, Code: code, Message: message})
		return
	}
	status := appErr.HTTPStatus
	if status < 400 || status > 599 {
		status = http.StatusInternalServerError
	}
	defaultCode, defaultMessage := errorDefaults(status)
	code := appErr.Code
	if code == "" {
		code = defaultCode
	}
	message := appErr.Message
	// Never expose raw backend failure details to API clients.
	if status == http.StatusInternalServerError {
		message = defaultMessage
	}
	if message == "" {
		message = defaultMessage
	}
	writeJSON(w, status, BaseResponse{ReturnCode: CodeFail, Code: code, Message: message, Reference: appErr.Reference})
}
