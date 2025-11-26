package main

// SPDX-License-Identifier: GPL-3.0-only

import (
	"bytes"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

const (
	// Maximum number of pages to crawl in a user's watchlist.  This is an
	// unreasonably high number to prevent infinite loops in case of unexpected
	// site behavior.  100 pages would be 20,000 artists.
	maxWatchlistPages = 100

	// Maximum number of pages to crawl in a user's gallery or scraps.  This is
	// also unreasonably high, only used to prevent infinite loops.
	maxGalleryPages = 1000

	// How many new usernames must be found on a watchlist page to continue
	// crawling.
	minNewUsernamesThreshold = 2
)

var (
	// Regex to extract usernames from watchlist page HTML.
	// Security: This pattern ensures no slashes are included in the username
	// capture group.  This prevents directory traversal attacks from a
	// maliciously crafted username.
	watchlistUserRegex = regexp.MustCompile(`/user/([^/]+)/`)
)

// Artist represents a FurAffinity artist and provides methods for retrieving
// their submissions from both gallery and scraps sections.
type Artist struct {
	logger    *slog.Logger
	client    Client
	username  string
	artistDir string
}

// NewArtist creates a new Artist instance with the specified logger, client,
// username, and output directory for downloaded submissions.
//
// Parameters:
//   - logger: Logger instance
//   - client: HTTP client interface for making web requests
//   - artistUsername: The FurAffinity username of the artist
//   - artistDir: Local directory path where submissions will be saved
//
// Returns:
//   - *Artist: A new Artist instance ready for use
func NewArtist(logger *slog.Logger, client Client, artistUsername string, artistDir string) *Artist {
	return &Artist{
		logger:    logger,
		client:    client,
		username:  artistUsername,
		artistDir: artistDir,
	}
}

// Username returns the FurAffinity username of this artist.
//
// Returns:
//   - string: The artist's username
func (a *Artist) Username() string {
	return a.username
}

// Submissions retrieves the list of submissions for the artist from their
// gallery and optionally from their scraps section. The crawling behavior can
// be controlled with the reCrawl parameter to either stop at already-saved
// submissions or continue through the entire gallery, checking for any which
// may not have been previously downloaded.
//
// Parameters:
//   - reCrawl: If false, stops crawling when encountering an already-saved submission;
//     if true, crawls through all submissions regardless of save status
//   - skipScraps: If false, also retrieves submissions from the artist's scraps section
//
// Returns:
//   - []*Submission: A slice of all found submissions, with scraps appended after gallery items
//   - error: An error if the submissions could not be retrieved
func (a *Artist) Submissions(reCrawl bool, skipScraps bool) ([]*Submission, error) {
	a.logger.Debug("getting submissions for artist", "username", a.username, "reCrawl", reCrawl)
	submissions, err := a.crawlSubmissions(reCrawl, false)
	if err != nil {
		return nil, err
	}

	if !skipScraps {
		a.logger.Debug("getting scraps for artist", "username", a.username, "reCrawl", reCrawl)
		scraps, err := a.crawlSubmissions(reCrawl, true)
		if err != nil {
			return nil, err
		}
		submissions = append(submissions, scraps...)
	}

	a.logger.Info("total submissions found", "user", a.username, "count", len(submissions))

	return submissions, nil
}

// crawlSubmissions is the internal implementation for Artist.Submissions that handles
// the pagination logic for crawling either gallery or scraps submissions.
//
// Parameters:
//   - reCrawl: If false, stops when encountering already-saved submissions
//   - scraps: If true, crawls the scraps section; if false, crawls the main gallery
//
// Returns:
//   - []*Submission: A slice of submissions found, in reverse chronological order
//   - error: An error if the submissions could not be retrieved
func (a *Artist) crawlSubmissions(reCrawl bool, scraps bool) ([]*Submission, error) {
	var galleryOrScraps string
	var submissionDir string
	if scraps {
		galleryOrScraps = "scraps"
		submissionDir = filepath.Join(a.artistDir, "scraps")
	} else {
		galleryOrScraps = "gallery"
		submissionDir = a.artistDir
	}

	var submissions []*Submission

	for pageNum := 1; ; pageNum++ {
		// Sanity check to prevent infinite loops
		if pageNum > maxGalleryPages {
			a.logger.Error("maximum gallery pages exceeded", "user", a.username, "maxPages", maxGalleryPages)
			fatalInvariant("maximum gallery pages exceeded")
		}

		url := fmt.Sprintf("https://www.furaffinity.net/%s/%s/%d",
			galleryOrScraps, a.username, pageNum)

		body, err := a.client.GetWithDelay(url)
		if err != nil {
			a.logger.Error("submissions: page fetch error", "url", url, "error", err)
			return nil, fmt.Errorf("failed to fetch gallery page: %w", err)
		}

		pageSubmissions, stopCrawling := a.parseSubmissionsFromPage(body, submissionDir, reCrawl)
		submissions = append(submissions, pageSubmissions...)

		a.logger.Debug("listing "+galleryOrScraps,
			"user", a.username,
			"page", pageNum,
			"count", len(pageSubmissions),
		)

		// If no submissions found on this page, we've reached the end.
		// Also stop if we hit a known submission.
		if len(pageSubmissions) == 0 || stopCrawling {
			break
		}
	}
	slices.Reverse(submissions)
	return submissions, nil
}

// parseSubmissionsFromPage extracts submission links from a gallery or scraps page.
// This is used internally by crawlSubmissions.
//
// Parameters:
//   - body: The HTML content of the page
//   - submissionDir: The directory where submissions are saved
//   - reCrawl: If false, stops when encountering already-saved submissions
//
// Returns:
//   - []*Submission: A slice of submissions found on this page
//   - bool: True if crawling should stop (an already-saved submission was found)
func (a *Artist) parseSubmissionsFromPage(body []byte, submissionDir string, reCrawl bool) ([]*Submission, bool) {
	stopCrawling := false
	var pageSubmissions []*Submission
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		a.logger.Error("submissions: failed to parse HTML", "error", err)
		// goquery won't error just because the HTML is malformed.  An error
		// indicates a failure to read from the reader, which should never
		// happen since we're reading from an in-memory byte slice.
		fatalInvariant(err)
	}

	// Find all submission links in the gallery that contain images (to avoid duplicates)
	doc.Find("a[href^='/view/']:has(img)").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		href, exists := s.Attr("href")
		if !exists {
			// We just selected on the element.  It should have an href
			// attribute.  If it doesn't, something is seriously wrong.
			fatalInvariant("href attr we selected on doesn't exist?! HOW?!")
		}
		substr := strings.TrimSuffix(href, "/")
		substr = strings.TrimPrefix(substr, "/view/")
		id, err := strconv.ParseUint(substr, 10, 64)
		if err != nil {
			// The only way this happens is if the /view/ link has a non-numeric
			// ID, which breaks our basic assumptions about submission URLs.
			// The site must have changed in a way we can't handle.
			fatalInvariant(fmt.Sprintf("Unable to extract id from %s: %v", href, err))
		}
		submission := NewSubmission(a.logger, a.client, id, submissionDir)
		if !reCrawl && submission.IsSaved() {
			a.logger.Debug("submission already saved, stopping crawl",
				"user", a.username,
				"id", id)
			stopCrawling = true
			return false // Break out of the EachWithBreak loop
		}
		pageSubmissions = append(pageSubmissions, submission)
		return true // Continue iterating
	})

	return pageSubmissions, stopCrawling
}

// GetArtistsFromWatchlist creates Artist instances for all artists found in
// the specified user's watchlist
//
// Parameters:
//   - logger: Logger instance
//   - client: HTTP client interface for making web requests
//   - watcherUsername: The FurAffinity username whose watchlist should be processed
//   - targetDir: Base directory where artist submissions will be saved
//
// Returns:
//   - []*Artist: A slice of Artist instances, one for each artist in the watchlist
//   - error: An error if the watchlist could not be retrieved
func GetArtistsFromWatchlist(
	logger *slog.Logger, client Client, watcherUsername string, targetDir string,
) ([]*Artist, error) {
	logger.Debug("GetArtistsFromWatchlist", "watcherUsername", watcherUsername, "targetDir", targetDir)

	artistUsernames, err := GetWatchlist(logger, client, watcherUsername)
	if err != nil {
		return nil, err
	}

	artists := make([]*Artist, len(artistUsernames))
	for i, artistUsername := range artistUsernames {
		artistDir := filepath.Join(targetDir, artistUsername)
		artists[i] = NewArtist(logger, client, artistUsername, artistDir)
	}

	return artists, nil
}

// GetWatchlist retrieves the list of usernames from a user's watchlist.
//
// Parameters:
//   - logger: Logger instance
//   - client: The Client interface used to fetch watchlist pages
//   - username: The FA username whose watchlist should be retrieved
//
// Returns:
//   - A slice of unique usernames from the watchlist, in order of first appearance
//   - An error if the watchlist could not be retrieved
//
// Panics:
//   - Maximum watchlist pages exceeded, indicating infinite loop
func GetWatchlist(logger *slog.Logger, client Client, username string) ([]string, error) {
	logger.Debug("getWatchlist", "username", username)

	// We could parse out the 'a' elements and only match against those, but
	// this is good enough.
	seen := make(map[string]bool)
	var usernames []string

	for pageNum := 1; ; pageNum++ {
		// Sanity check to prevent infinite loops
		if pageNum > maxWatchlistPages {
			logger.Error("maximum watchlist pages exceeded", "user", username, "maxPages", maxWatchlistPages)
			fatalInvariant("maximum watchlist pages exceeded")
		}
		newUsernames := 0 // Used to detect end of watchlist

		url := fmt.Sprintf("https://www.furaffinity.net/watchlist/by/%s/%d", username, pageNum)

		// We can't use GetWithDelay here because watchlist pages don't
		// contain the standard FurAffinity footer that our client uses
		// to determine when to delay.  So we just use Get directly.
		body, err := client.Get(url)
		if err != nil {
			logger.Error("getWatchlist: page fetch error", "url", url, "error", err)
			return nil, fmt.Errorf("failed to fetch watchlist page: %w", err)
		}

		matches := watchlistUserRegex.FindAllSubmatch(body, -1)

		// Add usernames found on this page, preserving order and avoiding duplicates
		for _, match := range matches {
			if len(match) <= 1 {
				continue
			}
			u := string(match[1])
			if !seen[u] {
				seen[u] = true
				usernames = append(usernames, u)
				newUsernames++
			}
		}

		logger.Info("watchlist page processed",
			"user", username,
			"page", pageNum,
			"count", len(matches),
			"new", newUsernames,
		)

		// If this page contains only a few new usernames, we've reached the
		// end.  FA will keep repeating a large number of entries so we can't
		// just check the length of usernames found on this page.
		if newUsernames < minNewUsernamesThreshold {
			break
		}
	}

	logger.Info("total watchlist entries found", "user", username, "count", len(usernames))

	return usernames, nil
}
