package ws

import (
	"fmt"
)

type SpxError struct {
	Code   int    `json:"code"`
	Text   string `json:"text"`
	Detail string `json:"error"`
}

// Implement error interface
func (e SpxError) Error() string {
	msg := fmt.Sprintf("Error code: %d, msg: %s, detail: %s", e.Code, e.Text, e.Detail)
	return msg
}

// Only use a small set of HTTP error codes
//
// 200 - StatusOK
// 400 - StatusBadRequest
// 401 - StatusUnauthorized
// 403 - StatusForbidden
// 404 - StatusNotFound
// 500 - StatusInternalServerError

var (
	ErrorHttpTimeout    = SpxError{100, "we did not get a response from the server.", "http timeout"}
	ErrorJWTInvalid     = SpxError{101, "invalid API security credentials", "missing or invalid jwt access token"}
	ErrorInvalidRequest = SpxError{102, "unexpected request parameters", "invalid parameters"}
	ErrorTimeout        = SpxError{103, "operation timeout", "operation timeout"}
	ErrorClosed         = SpxError{104, "socket closed", "underlying fd/socket closed normally"}
	ErrorInternal       = SpxError{999, "unexpected error", "internal error"}
)

// TODO: Adderror should return a new error with the added extension
func (e SpxError) AddError(err error) SpxError {
	e.Detail = e.Detail + "; " + err.Error()
	return e
}
