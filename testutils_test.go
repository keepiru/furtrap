package main_test

// SPDX-License-Identifier: GPL-3.0-only

import (
	"errors"
	"fmt"
	main "furtrap"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var (
	ErrInvalidTestPath = errors.New("invalid path")
)

// TestResponse represents a predefined response for a specific URI in the
// TestClient mock.
type TestResponse struct {
	data  []byte
	error error
}

// TestClient is a mock Client for use in tests.  It allows setting predefined
// responses for specific URIs, and falls back to reading from sample_data if no
// response is set.
type TestClient struct {
	uris map[string]TestResponse
}

// NewTestClient creates a new TestClient instance with an empty set of
// predefined responses.
func NewTestClient() *TestClient {
	return &TestClient{
		uris: make(map[string]TestResponse),
	}
}

// SetResponse sets a predefined response for the specified URI in the
// TestClient.
//
// Parameters:
//   - uri: The URI for which to set the response
//   - response: The byte slice to return when the URI is requested
//   - err: The error to return when the URI is requested (nil for no error)
func (t *TestClient) SetResponse(uri string, response []byte, err error) {
	t.uris[uri] = TestResponse{
		data:  response,
		error: err,
	}
}

// Get simulates an HTTP GET request to the specified URI.  If a predefined
// response has been set for the URI, it returns that response.  Otherwise, it
// attempts to read the response data from a file in the sample_data directory.
// Errors may be set in SetResponse.  If the file does not exist, it returns
// ErrHTTPNotFound.
//
// Parameters:
//   - uri: The URI to request
//
// Returns:
//   - []byte: The response data
//   - error: An error if the request fails
func (t *TestClient) Get(uri string) ([]byte, error) {
	if response, ok := t.uris[uri]; ok {
		return response.data, response.error
	}

	path := strings.TrimPrefix(uri, "https://")
	fn := filepath.Join("sample_data", path)
	// Prevent directory traversal attacks
	if fn != filepath.Clean(fn) {
		return nil, ErrInvalidTestPath
	}

	data, err := os.ReadFile(fn)
	switch {
	case err == nil:
		// continue
	case errors.Is(err, os.ErrNotExist):
		return nil, fmt.Errorf("resource not found: %w", main.ErrHTTPNotFound)
	default:
		return nil, fmt.Errorf("failed to read file %s: %w", fn, err)
	}
	return data, nil
}

// GetWithDelay is identical to Get for the TestClient.  It exists to satisfy
// the Client interface.  Delays are not simulated in the test client.
//
// Parameters:
//   - uri: The URI to request
//
// Returns:
//   - []byte: The response data
//   - error: An error if the request fails
func (t *TestClient) GetWithDelay(uri string) ([]byte, error) {
	return t.Get(uri)
}

// TestLogForwarder is an io.Writer that forwards log output to testing.T.Logf.
// This is used to capture application log output and report it in the test
// output.
type TestLogForwarder struct {
	t *testing.T
}

// Write implements the io.Writer interface for TestLogForwarder.  It forwards
// the log output to the testing.T instance.
//
// Parameters:
//   - p: The byte slice containing log data
//
// Returns:
//   - int: The number of bytes written
//   - error: An error if the write operation fails
func (t TestLogForwarder) Write(p []byte) (int, error) {
	t.t.Helper()

	// Get the caller info 5 levels up the stack to find the original log call.
	_, file, line, ok := runtime.Caller(5)
	if !ok {
		// This should never happen because we're always in test with a stack at
		// least this deep.  If this starts happening we can probably just log
		// without the caller info, but I'd like to know why it happened, so
		// we'll panic here for now.
		panic("unable to get caller info for test logger")
	}

	filename := filepath.Base(file)

	// t.Logf tries to prepend the file and line number of the caller, but
	// because of the way we're wrapping it, it will always show "helper.go".
	// We'll prepend the correct file and line number ourselves.
	t.t.Logf("%s:%d: %s", filename, line, p)

	return len(p), nil
}

// NewTestLogger creates a new slog.Logger that writes to the provided
// testing.T instance.  This allows capturing log output in test logs.
//
// Parameters:
//   - t: The testing.T instance to which log output will be forwarded
func NewTestLogger(t *testing.T) *slog.Logger {
	t.Helper()
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	handler := slog.NewTextHandler(TestLogForwarder{t: t}, opts)
	return slog.New(handler)
}

// CapturePanic executes the provided function and captures any panic that
// occurs.  It returns the recovered panic value, or nil if no panic occurred.
//
// Parameters:
//   - t: The testing.T instance
//   - fn: The function to execute
//
// Returns:
//   - any: The recovered panic value, or nil if no panic occurred
func CapturePanic(t *testing.T, fn func()) any {
	t.Helper()
	var ret any

	func() {
		defer func() {
			ret = recover()
		}()
		fn()
	}()

	return ret
}
