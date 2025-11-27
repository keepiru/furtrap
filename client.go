package main

// SPDX-License-Identifier: GPL-3.0-only

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	// Rate limiting constants.
	highUserThreshold = 10000
	highUserDelayTime = 5 * time.Minute
	// Even during low traffic, we add a small delay to be kind to the server.
	defaultDelayTime = 1 * time.Second

	// HTTP client retry constants.
	defaultRetryCount    = 3
	defaultRetryInterval = 5 * time.Second

	httpTimeout   = 90 * time.Second
	httpUserAgent = "furtrap/2.0 (+https://github.com/keepiru/furtrap)"

	// How many regex capture groups registeredUsersRegexp should have.
	registeredUsersRegexpCaptures = 2

	// Number of tab-separated fields in a Netscape/Mozilla cookies.txt file.
	cookiesTxtFieldCount = 7

	oneWeekDuration = 7 * 24 * time.Hour
)

var (
	ErrRegisteredUsersNotFound = errors.New("could not find registered users count")
	ErrHTTPStatusNotOK         = errors.New("HTTP request failed with non-200 status")
	ErrHTTPNotFound            = errors.New("HTTP 404 Not Found")
	ErrExpiredCookie           = errors.New("cookie is expiring, update your cookies.txt file")
	ErrInvalidCookie           = errors.New("invalid cookie format")

	// Regex to extract number of registered users online from FA HTML.
	registeredUsersRegexp = regexp.MustCompile(`(\d+)\s+registered`)
)

// Client is an abstract HTTP client.  In prod, this wraps http.Client.  In
// test, it is a TestClient mock.
type Client interface {
	Get(uri string) ([]byte, error)
	GetWithDelay(uri string) ([]byte, error)
}

// HTTPClient is a concrete implementation of the Client interface which
// performs GETs with retry logic and rate limiting.
type HTTPClient struct {
	logger        *slog.Logger
	client        *http.Client
	tryCount      int
	retryInterval time.Duration
	delayFunc     func(int)
}

// NewHTTPClient creates a new HTTPClient instance with default settings for
// rate limiting and retries.  The defaults are appropriate for prod use, and
// are overridden for integration tests.
//
// Parameters:
//   - logger: Logger instance
//
// Returns:
//   - *HTTPClient: A new HTTPClient instance ready for use
func NewHTTPClient(logger *slog.Logger) *HTTPClient {
	delayFunc := func(registeredUsers int) {
		if registeredUsers > highUserThreshold {
			logger.Info("High registered user count detected, delaying 5 minutes", "count", registeredUsers)
			time.Sleep(highUserDelayTime)
		} else {
			time.Sleep(defaultDelayTime)
		}
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		// There are no conditions where cookiejar.New returns an error, ever,
		// as of Go 1.23.  Just in case that changes in the future, we'll handle
		// it here.  Fatal because we have no idea what the future error
		// conditions are.
		fatalInvariant(fmt.Errorf("failed to create cookie jar: %w", err))
	}

	client := &http.Client{
		Jar: jar,
	}

	return &HTTPClient{
		logger:        logger,
		client:        client,
		tryCount:      defaultRetryCount,
		retryInterval: defaultRetryInterval,
		delayFunc:     delayFunc,
	}
}

// SetRetryPolicy configures the retry behavior for failed HTTP requests.  This
// method is intended for integration testing where we don't actually want to
// wait between retries.
//
// Parameters:
//   - count: Number of retry attempts before giving up
//   - interval: Time to wait between retry attempts
func (h *HTTPClient) SetRetryPolicy(count int, interval time.Duration) {
	h.tryCount = count
	h.retryInterval = interval
}

// SetDelayFunc overrides the default delay function.  This is intended to
// inject test spies during integration tests instead of sleeping.
//
// Parameters:
//   - fn: Function that takes registered user count and implements delay logic
func (h *HTTPClient) SetDelayFunc(fn func(int)) {
	h.delayFunc = fn
}

// LoadCookies loads cookies from a Netscape/Mozilla format cookies.txt file and
// adds them to the client's cookie jar. This allows access to pages only
// available to logged-in users.
//
// The method parses the "standard" cookies.txt format with tab-separated
// fields: domain, flag, path, secure, expiration, name, value
//
// Parameters:
//   - filename: Path to the cookies.txt file to load
//
// Returns:
//   - error: Any error encountered while reading or parsing the cookies file
func (h *HTTPClient) LoadCookies(filename string) error {
	//#nosec G304: filename is intentionally from user input
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open cookies file: %w", err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		err := h.parseCookieLine(line)
		if err != nil {
			return fmt.Errorf("failed to load cookie: %w", err)
		}
	}

	err = scanner.Err()
	if err != nil {
		return fmt.Errorf("error reading cookies file: %w", err)
	}

	h.logger.Info("Loaded cookies from file", "file", filename)
	return nil
}

// GetWithDelay wraps Get with a ratelimiting function.  It simply adds a very
// long delay if too many are online.  This is to comply with FA's request:
// "Limit bot activity to periods with less than 10k registered users online."
// Even when fewer users are online, we'll still add a short delay to be kind.
//
// Parameters:
//   - uri: The URL to fetch
//
// Returns:
//   - []byte: The response body content
//   - error: Any error encountered during the request or delay logic
func (h *HTTPClient) GetWithDelay(uri string) ([]byte, error) {
	ret, err := h.Get(uri)
	if err != nil {
		return ret, err
	}

	registeredUsers, err := h.parseRegisteredUsersOnline(ret)
	if err != nil {
		return ret, err
	}

	// Delay if the number of registered users is high.
	// In prod, this will log a message and sleep for a while.
	// In test this will be a spy or no-op.
	h.delayFunc(registeredUsers)

	return ret, nil
}

// Get performs an HTTP GET request with automatic retries. If the initial
// request fails, it will retry up to the configured number of times with delays
// between attempts.
//
// Parameters:
//   - uri: The URL to fetch
//
// Returns:
//   - []byte: The response body content
//   - error: The final error if all retry attempts fail, nil on success
func (h *HTTPClient) Get(uri string) ([]byte, error) {
	h.logger.Debug("HTTPClient GET", "uri", uri)
	var lastErr error
	for attempt := range h.tryCount {
		data, err := h.get(uri)
		if err == nil {
			return data, nil
		}
		lastErr = err
		h.logger.Info("HTTPClient GET failed attempt", "uri", uri, "attempt", attempt, "error", err)
		time.Sleep(h.retryInterval)
	}
	h.logger.Error("HTTPClient GET all attempts failed", "uri", uri, "error", lastErr)
	return nil, lastErr
}

// get performs a single HTTP GET request without retries. This is used inside
// the public Get method's retry loop.
//
// Parameters:
//   - uri: The URL to fetch
//
// Returns:
//   - []byte: The response body content
//   - error: Any error encountered during the request
func (h *HTTPClient) get(uri string) ([]byte, error) {
	// This is the only place where we need a context, so we create one with a
	// timeout here.  The whole program is intentionally designed so nothing
	// needs to be cleaned up on shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", httpUserAgent)

	response, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("resource not found: %w", ErrHTTPNotFound)
	}

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %s", ErrHTTPStatusNotOK, response.Status)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return body, nil
}

// parseRegisteredUsersOnline extracts number of online registered users from
// FurAffinity HTML content. This is used to determine the delays for rate
// limiting.
//
// Parameters:
//   - htmlContent: Raw HTML content from a FurAffinity page
//
// Returns:
//   - int: The number of registered users currently online
//   - error: ErrRegisteredUsersNotFound if the count cannot be extracted
func (h *HTTPClient) parseRegisteredUsersOnline(htmlContent []byte) (int, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(htmlContent))
	if err != nil {
		return 0, fmt.Errorf("failed to parse HTML content: %w", err)
	}

	// Try to find the online stats section.  FA doesn't include the .online-stats div when the
	// "classic" theme template is enabled, so we have to use a janky selector as a backup.
	var text string
	sel := doc.Find(".online-stats")
	if sel.Length() > 0 {
		// Logged out structure: <div class="online-stats">
		text = sel.First().Text()
	} else {
		// Classic template structure: look for span with title "Measured in the last 900 seconds"
		// and go up to the center element that contains all the stats
		sel = doc.Find(`span[title="Measured in the last 900 seconds"]`).Parent().Parent()
		if sel.Length() == 0 {
			return 0, ErrRegisteredUsersNotFound
		}
		text = sel.Text()
	}

	matches := registeredUsersRegexp.FindStringSubmatch(text)
	if len(matches) != registeredUsersRegexpCaptures {
		return 0, ErrRegisteredUsersNotFound
	}

	registeredUsers, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, ErrRegisteredUsersNotFound
	}

	return registeredUsers, nil
}

// parseCookieLine parses a single line from a cookies.txt file and adds the
// cookie to the client's cookie jar.
//
// Parameters:
//   - line: A single line from a cookies.txt file
func (h *HTTPClient) parseCookieLine(line string) error {
	// Skip comments and empty lines
	if line == "" || strings.HasPrefix(line, "#") {
		return nil
	}

	// Parse cookie line format: domain	flag	path	secure	expiration	name	value
	parts := strings.Split(line, "\t")
	if len(parts) != cookiesTxtFieldCount {
		return fmt.Errorf("%w: %v", ErrInvalidCookie, line)
	}

	domain := parts[0]
	// flag := parts[1] // not used
	path := parts[2]
	secure := strings.ToUpper(parts[3]) == "TRUE"
	expiration := parts[4]
	name := parts[5]
	value := parts[6]

	// Abort if a cookie is expired or will expire soon.  We don't want to
	// continue without being logged in because it will result in silently
	// missed submissions.  The user should update their cookies.txt file, or
	// they can re-run without a cookies.txt file if they don't need logged-in
	// access.  One week is chosen as a reasonable maximum time the program
	// might be run without user intervention.
	expireTime, err := strconv.ParseInt(expiration, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid expiration time for cookie %s: %w", name, err)
	}
	cookieExpire := time.Unix(expireTime, 0)
	oneWeekFromNow := time.Now().Add(oneWeekDuration)
	if cookieExpire.Before(oneWeekFromNow) {
		return fmt.Errorf("%w: %s", ErrExpiredCookie, name)
	}

	// Create URL for the domain
	scheme := "http"
	if secure {
		scheme = "https"
	}

	cookieURL, err := url.Parse(
		fmt.Sprintf("%s://%s%s", scheme, domain, path))
	if err != nil {
		return fmt.Errorf("invalid URL for cookie %s: %w", name, err)
	}

	// Create cookie
	cookie := &http.Cookie{
		Name:   name,
		Value:  value,
		Domain: domain,
		Path:   path,
	}

	// Add cookie to jar
	h.client.Jar.SetCookies(cookieURL, []*http.Cookie{cookie})
	return nil
}
