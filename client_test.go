package main_test

// SPDX-License-Identifier: GPL-3.0-only

import (
	"bytes"
	"errors"
	"fmt"
	main "furtrap"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gotest.tools/assert"
)

func SampleDataHandler(w http.ResponseWriter, r *http.Request) {
	webroot := filepath.Join("sample_data", "www.furaffinity.net")

	// Prevent directory traversal attacks
	fn := filepath.Join(webroot, r.URL.Path)
	if fn != filepath.Clean(fn) {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	data, err := os.ReadFile(fn)
	switch {
	case err == nil:
		// continue
	case errors.Is(err, fs.ErrNotExist):
		http.NotFound(w, r)
		return
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_, err = w.Write(data)
	if err != nil {
		panic(fmt.Errorf("SampleDataHandler write failure: %w", err))
	}
}

type Flaky502Handler struct {
	failuresRemaining int
}

func (h *Flaky502Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.failuresRemaining > 0 {
		h.failuresRemaining--
		http.Error(w, "502 Bad Gateway", http.StatusBadGateway)
		return
	}
	SampleDataHandler(w, r)
}

func TestHTTPClient_Get(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(SampleDataHandler))
	defer server.Close()
	client := main.NewHTTPClient(NewTestLogger(t))
	client.SetRetryPolicy(3, 0*time.Millisecond)
	t.Run("Basic GET", func(t *testing.T) {
		have, err := client.Get(server.URL + "/view/101")
		assert.NilError(t, err)

		got := bytes.Contains(have, []byte("Test Submission 101"))
		assert.Assert(t, got, "Should get submission 101 page")
	})

	t.Run("404", func(t *testing.T) {
		_, err := client.Get(server.URL + "/view/00000")
		assert.Assert(t, errors.Is(err, main.ErrHTTPNotFound), "Should get ErrHTTPNotFound on 404")
	})

	t.Run("HTTPClient#Get with flaky 502 succeeds after retry", func(t *testing.T) {
		flakyHandler := &Flaky502Handler{failuresRemaining: 2}
		flakyServer := httptest.NewServer(flakyHandler)
		defer flakyServer.Close()

		have, err := client.Get(flakyServer.URL + "/view/101")
		assert.NilError(t, err)

		got := bytes.Contains(have, []byte("Test Submission 101"))
		assert.Assert(t, got, "Should get submission 101 page")

		// Make sure it actually retried
		assert.Equal(t, flakyHandler.failuresRemaining, 0)
	})

	t.Run("HTTPClient#Get with flaky 502 fails after retries exceeded", func(t *testing.T) {
		flakyHandler := &Flaky502Handler{failuresRemaining: 5}
		flakyServer := httptest.NewServer(flakyHandler)
		defer flakyServer.Close()

		_, err := client.Get(flakyServer.URL + "/view/101")
		assert.ErrorContains(t, err, "502 Bad Gateway")
	})

	t.Run("HTTPClient sends correct User-Agent", func(t *testing.T) {
		handler := func(w http.ResponseWriter, r *http.Request) {
			ua := r.Header.Get("User-Agent")
			_, _ = w.Write([]byte(ua))
		}
		uaServer := httptest.NewServer(http.HandlerFunc(handler))
		defer uaServer.Close()

		have, err := client.Get(uaServer.URL)
		assert.NilError(t, err)

		want := "furtrap/2.0 (+https://github.com/keepiru/furtrap)"
		assert.Equal(t, string(have), want)
	})
}

// Integration test for GetWithDelay, calling down through parseRegisteredUsersOnline,
// and ensuring it calls the delayFunc callback with the number of registered users online.
func TestHTTPClient_GetWithDelay(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(SampleDataHandler))
	defer server.Close()

	tests := []struct {
		uri         string
		want        int
		expectError bool
	}{
		{"/view/103", 14541, false},
		{"/view/101", 0, true},
		{"/watchlist/by/test-watcher/2", 0, true},
		{"/gallery/test-artist/2", 14608, false},
		// 104 has a different structure matching the "classic" layout
		{"/view/104", 13944, false},
	}

	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			registeredUsers := 0
			client := main.NewHTTPClient(NewTestLogger(t))
			client.SetDelayFunc(func(ru int) { registeredUsers = ru })

			_, err := client.GetWithDelay(server.URL + tt.uri)
			if tt.expectError {
				assert.Error(t, err, "could not find registered users count")
			} else {
				assert.NilError(t, err)
			}
			assert.Equal(t, registeredUsers, tt.want)
		})
	}
}

func TestHTTPClient_LoadCookies(t *testing.T) {
	t.Run("Load some cookies", func(t *testing.T) {
		cookiesContent := `# Netscape HTTP Cookie File
.furaffinity.net	TRUE	/	TRUE	4070937600	test_cookie	test_value
.furaffinity.net	TRUE	/path	FALSE	4070937600	another_cookie	another_value`

		tempfile := filepath.Join(t.TempDir(), "cookies.txt")
		err := os.WriteFile(tempfile, []byte(cookiesContent), 0600)
		assert.NilError(t, err)

		// Just a basic smoke test
		client := main.NewHTTPClient(NewTestLogger(t))
		err = client.LoadCookies(tempfile)
		assert.NilError(t, err)
	})

	t.Run("Nonexistent file = error", func(t *testing.T) {
		client := main.NewHTTPClient(NewTestLogger(t))
		err := client.LoadCookies("nonexistent.txt")
		assert.Assert(t, err != nil)
	})

	t.Run("Verify cookies received with httptest", func(t *testing.T) {
		// Create test server that echoes back cookies
		handler := func(w http.ResponseWriter, r *http.Request) {
			for _, cookie := range r.Cookies() {
				_, _ = fmt.Fprintf(w, "%s=%s", cookie.Name, cookie.Value)
			}
		}
		server := httptest.NewServer(http.HandlerFunc(handler))
		defer server.Close()

		// Create cookies file
		serverURL, err := url.Parse(server.URL)
		assert.NilError(t, err)

		format := "# Netscape HTTP Cookie File\n" +
			"%s\tTRUE\t/\tFALSE\t4070937600\ttest_cookie\ttest_value\n" +
			"%s\tTRUE\t/\tFALSE\t4070937600\tanother_cookie\tanother_value\n"
		cookies := fmt.Sprintf(format, serverURL.Hostname(), serverURL.Hostname())

		tempfile := filepath.Join(t.TempDir(), "cookies.txt")
		err = os.WriteFile(tempfile, []byte(cookies), 0600)
		assert.NilError(t, err)

		// Load cookies into client
		client := main.NewHTTPClient(NewTestLogger(t))
		err = client.LoadCookies(tempfile)
		assert.NilError(t, err)

		// Make request to test server
		respData, err := client.Get(server.URL)
		assert.NilError(t, err)

		// Verify that the cookies were sent and received correctly
		assert.Assert(t, bytes.Contains(respData,
			[]byte("test_cookie=test_value")),
			"Response should contain the test_cookie")
		assert.Assert(t, bytes.Contains(respData,
			[]byte("another_cookie=another_value")),
			"Response should contain the another_cookie")
	})

	t.Run("Abort if cookie is expired", func(t *testing.T) {
		fiveDaysFromNow := time.Now().Add(5 * 24 * time.Hour).Unix()
		cookiesContent := fmt.Sprintf(`.furaffinity.net	TRUE	/	TRUE	%d	expired_cookie	expired_value`, fiveDaysFromNow)

		tempfile := filepath.Join(t.TempDir(), "cookies.txt")
		err := os.WriteFile(tempfile, []byte(cookiesContent), 0600)
		assert.NilError(t, err)

		client := main.NewHTTPClient(NewTestLogger(t))
		err = client.LoadCookies(tempfile)
		assert.Error(t, err, "failed to load cookie: cookie is expiring, update your cookies.txt file: expired_cookie")
	})
}
