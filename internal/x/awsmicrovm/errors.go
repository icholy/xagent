package awsmicrovm

import (
	"encoding/json"
	"errors"
	"fmt"
)

// APIError is a non-2xx response from the Lambda MicroVMs control plane.
type APIError struct {
	Op         string // operation, e.g. "GetMicrovm"
	StatusCode int    // HTTP status — the reliable signal
	Code       string // service error code, best-effort from the body
	Message    string // service message, best-effort; the raw body if unparseable
}

func (e *APIError) Error() string {
	switch {
	case e.Code != "" && e.Message != "":
		return fmt.Sprintf("lambda-microvms %s: status %d: %s: %s", e.Op, e.StatusCode, e.Code, e.Message)
	case e.Code != "":
		return fmt.Sprintf("lambda-microvms %s: status %d: %s", e.Op, e.StatusCode, e.Code)
	case e.Message != "":
		return fmt.Sprintf("lambda-microvms %s: status %d: %s", e.Op, e.StatusCode, e.Message)
	default:
		return fmt.Sprintf("lambda-microvms %s: status %d", e.Op, e.StatusCode)
	}
}

// IsNotFound reports whether err is a 404 from the control plane.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == 404
}

// newAPIError builds an *APIError from a non-2xx response. Code and Message are
// best-effort parsed from the body using the common AWS error envelope shapes;
// if the body does not parse, the raw body text is used as Message. This is a
// PREVIEW API with an uncertain error envelope, so an unexpected body shape must
// never cause a failure here.
func newAPIError(op string, statusCode int, body []byte) *APIError {
	e := &APIError{Op: op, StatusCode: statusCode}

	var env struct {
		Type      string `json:"__type"`
		Code      string `json:"code"`
		Message   string `json:"message"`
		MessageUp string `json:"Message"`
	}
	if err := json.Unmarshal(body, &env); err == nil {
		switch {
		case env.Code != "":
			e.Code = env.Code
		case env.Type != "":
			e.Code = env.Type
		}
		switch {
		case env.Message != "":
			e.Message = env.Message
		case env.MessageUp != "":
			e.Message = env.MessageUp
		}
	}

	// Fall back to the raw body when the envelope yielded no message (either the
	// body did not parse, or it parsed but carried no recognizable message field).
	if e.Message == "" {
		e.Message = string(body)
	}
	return e
}
