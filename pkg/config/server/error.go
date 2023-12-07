package server

import (
	"encoding/json"
	"net/http"
)

// Error defines model for Error.
type Error struct {
	// Code Error code
	Code int32 `json:"code"`

	// Message Error message
	Message string `json:"message"`
}

func (e Error) Error() string { return e.Message }

// sendServerError wraps sending of an error in the Error format, and handling the failure to
// marshal that.
func sendServerError(w http.ResponseWriter, message string, code int) {
	err := &Error{
		Code:    int32(code),
		Message: message,
	}
	sendServerErrorRaw(w, err)
}

// sendServerError wraps sending of an error in the Error format, and handling the failure to
// marshal that.
func sendServerErrorRaw(w http.ResponseWriter, err *Error) {
	w.WriteHeader(int(err.Code))
	_ = json.NewEncoder(w).Encode(err)
}
