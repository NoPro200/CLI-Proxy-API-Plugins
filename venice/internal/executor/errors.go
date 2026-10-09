package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// statusError carries the HTTP status to CLIProxyAPI, which uses it to cool
// down rate-limited auths, re-login expired ones and spare auths on bad input.
type statusError struct {
	statusCode int
	op         string
	body       []byte
}

func (e statusError) Error() string {
	message := upstreamErrorMessage(e.body)
	if message == "" {
		message = fmt.Sprintf("status %d", e.statusCode)
	}
	if e.op == "" {
		return message
	}
	return e.op + ": " + message
}

func (e statusError) StatusCode() int {
	return e.statusCode
}

const maxStreamErrorBodyBytes = 4096

// readStreamErrorBody reads the start of a failed stream for the error message.
func readStreamErrorBody(ctx context.Context, chunks <-chan pluginapi.HTTPStreamChunk) []byte {
	if chunks == nil {
		return nil
	}
	body := make([]byte, 0)
	for len(body) < maxStreamErrorBodyBytes {
		select {
		case <-ctx.Done():
			return body
		case chunk, ok := <-chunks:
			if !ok {
				return body
			}
			remaining := maxStreamErrorBodyBytes - len(body)
			if len(chunk.Payload) > remaining {
				return append(body, chunk.Payload[:remaining]...)
			}
			body = append(body, chunk.Payload...)
			if chunk.Err != nil {
				return body
			}
		}
	}
	return body
}

// badRequest reports a client mistake, so the host does not blame the auth.
func badRequest(format string, args ...any) error {
	return statusError{statusCode: http.StatusBadRequest, body: []byte(fmt.Sprintf(format, args...))}
}

func upstreamErrorMessage(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}
	var decoded struct {
		Message string          `json:"message"`
		Error   json.RawMessage `json:"error"`
	}
	if errUnmarshal := json.Unmarshal([]byte(trimmed), &decoded); errUnmarshal == nil {
		if len(decoded.Error) > 0 {
			var errorObject struct {
				Message string `json:"message"`
			}
			if errObject := json.Unmarshal(decoded.Error, &errorObject); errObject == nil {
				if message := strings.TrimSpace(errorObject.Message); message != "" {
					return message
				}
			}
			var errorString string
			if errString := json.Unmarshal(decoded.Error, &errorString); errString == nil {
				if message := strings.TrimSpace(errorString); message != "" {
					return message
				}
			}
		}
		if message := strings.TrimSpace(decoded.Message); message != "" {
			return message
		}
	}
	if len(trimmed) > 300 {
		trimmed = trimmed[:300]
	}
	return trimmed
}
