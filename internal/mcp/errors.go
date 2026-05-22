package mcp

import (
	"errors"
	"fmt"
)

type toolCallError struct {
	public string
	err    error
}

func userToolError(format string, args ...any) error {
	return toolCallError{public: fmt.Sprintf(format, args...)}
}

func internalToolError(public string, err error) error {
	return toolCallError{public: public, err: err}
}

func (e toolCallError) Error() string {
	if e.err != nil {
		return e.public + ": " + e.err.Error()
	}
	return e.public
}

func (e toolCallError) Unwrap() error {
	return e.err
}

func publicToolErrorMessage(err error) string {
	var toolErr toolCallError
	if errors.As(err, &toolErr) {
		return toolErr.public
	}
	return "tool call failed"
}

func shouldLogToolError(err error) bool {
	var toolErr toolCallError
	if errors.As(err, &toolErr) {
		return toolErr.err != nil
	}
	return true
}
