package errfmt

import "fmt"

type Error struct {
	Code       int    `json:"code"`
	Type       string `json:"type"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"http_status,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
}

func (e *Error) Error() string { return e.Message }
func New(code int, kind, format string, args ...any) *Error {
	return &Error{Code: code, Type: kind, Message: fmt.Sprintf(format, args...)}
}

var ExitCodes = map[int]string{
	0: "success", 1: "general_error", 2: "usage", 3: "empty_result_reserved",
	4: "authentication", 5: "not_found", 6: "forbidden", 7: "rate_limited",
	8: "retryable", 9: "unverified", 10: "configuration",
}
