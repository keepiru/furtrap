package main

// SPDX-License-Identifier: GPL-3.0-only

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

const (
	// Directory permissions when creating submission directories.
	submissionDirPermissions = 0750
)

var (
	ErrSubmissionImageNotFound = errors.New("failed to find download link in HTML")
	ErrUnexpectedLinkFormat    = errors.New("unexpected download link format")
	ErrInvalidFilePath         = errors.New("invalid file path")
)

// Submission represents a single FurAffinity submission (either a main gallery
// submission or a scrap) and provides methods for downloading the associated
// file and metadata.
type Submission struct {
	logger        *slog.Logger
	client        Client
	id            uint64
	submissionDir string
}

// NewSubmission creates a new Submission instance for the specified logger,
// client, ID, and output directory. The submission can then be used to download
// and save the associated file and metadata.
//
// Parameters:
//   - logger: Logger instance
//   - client: HTTP client interface for making web requests
//   - id: The FurAffinity submission ID (numeric)
//   - submissionDir: Directory where the submission files will be saved
//
// Returns:
//   - *Submission: A new Submission instance ready for use
func NewSubmission(logger *slog.Logger, client Client, id uint64, submissionDir string) *Submission {
	return &Submission{
		logger:        logger,
		client:        client,
		id:            id,
		submissionDir: submissionDir,
	}
}

// ID returns the FurAffinity submission ID for this submission.
//
// Returns:
//   - uint64: The numeric submission ID
func (s *Submission) ID() uint64 {
	return s.id
}

// Save downloads and saves the submission file and associated HTML /view/ page.
// If the submission has already been saved (determined by the presence of the
// HTML metadata file), this method returns early without re-downloading.
//
// Returns:
//   - error: Any error encountered during the download or save process, nil on success
func (s *Submission) Save() error {
	// Don't overwrite if it's already saved
	if s.IsSaved() {
		s.logger.Debug("Submission already saved, skipping", "id", s.id)
		return nil
	}

	// Ensure target directory exists
	err := os.MkdirAll(s.submissionDir, submissionDirPermissions)
	if err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	// Get the submission page
	submissionURL := fmt.Sprintf("https://www.furaffinity.net/view/%d", s.id)
	pageContent, err := s.client.GetWithDelay(submissionURL)
	if err != nil {
		return fmt.Errorf("failed to get submission page: %w", err)
	}

	// Parse HTML with goquery
	downloadURL, filename, err := parseURLAndFilenameFromViewPage(pageContent)
	if err != nil {
		return err
	}

	// Download the actual file
	fileContent, err := s.client.Get(downloadURL)
	switch {
	case err == nil:
		// continue
	case errors.Is(err, ErrHTTPNotFound):
		// Unfortunately this just happens sometimes.  The view page exists but the
		// download link 404s.  Log and skip.
		s.logger.Error("File download 404s, skipping submission", "id", s.id, "url", downloadURL)
		return nil
	default:
		return fmt.Errorf("failed to download file: %w", err)
	}

	// Save both the file and the HTML page
	return s.saveSubmissionFiles(filename, fileContent, pageContent)
}

// parseURLAndFilenameFromViewPage extracts the download URL and filename from a
// FurAffinity submission /view/ page. It parses the HTML to find a link containing
// the text "Download" and extracts the download link for the full-resolution file.
//
// Parameters:
//   - pageContent: Raw HTML content from the submission view page
//
// Returns:
//   - string: The absolute download URL for the submission file
//   - string: The filename extracted from the download URL
//   - error: Any error encountered during HTML parsing or URL extraction
func parseURLAndFilenameFromViewPage(pageContent []byte) (string, string, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(pageContent))
	if err != nil {
		return "", "", fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Find a link containing the text "Download"
	var href string
	var found bool

	// There's not a good attribute to select on, so just iterate all links
	// until we find one contaning the text "Download"
	doc.Find("a").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if strings.TrimSpace(s.Text()) == "Download" {
			foundHref, exists := s.Attr("href")
			if exists {
				found = true
				href = foundHref
				return false // stop iterating
			}
		}
		return true // continue iterating
	})

	if !found {
		return "", "", ErrSubmissionImageNotFound
	}

	// Convert relative URL to absolute URL
	if !strings.HasPrefix(href, "//") {
		return "", "", fmt.Errorf("%w: %s", ErrUnexpectedLinkFormat, href)
	}
	downloadURL := "https:" + href

	// Extract filename from download URL.
	//
	// Security: Splitting on "/" ensures we won't accidentally include any path
	// components, which could lead to directory traversal attacks.
	urlParts := strings.Split(downloadURL, "/")
	filename := urlParts[len(urlParts)-1]

	// Sanitize filename for Windows compatibility.
	// FA already sanitizes filenames, but let's just be sure.
	replacer := strings.NewReplacer(
		"<", "_",
		">", "_",
		":", "_",
		"\"", "_",
		"\\", "_",
		"|", "_",
		"?", "_",
		"*", "_",
	)
	filename = replacer.Replace(filename)

	return downloadURL, filename, nil
}

// WriteAndFsyncFile writes data to a file and fsyncs it to disk. fsync is
// required to ensure ordering: we need the submission data to be fully written
// before the metadata HTML page is saved.  In case of an interruption, this
// ensures the data will be complete, or the HTML won't exist and both will be
// downloaded again.
//
// Parameters:
//   - filePath: The target file path where data should be written
//   - data: The byte data to write to the file
//
// Returns:
//   - error: Any error encountered during file creation, writing, or syncing
func WriteAndFsyncFile(filePath string, data []byte) error {
	// Prevent directory traversal attacks.
	// This should never happen because of the way we construct file paths, but check anyway.
	if filePath != filepath.Clean(filePath) {
		return fmt.Errorf("%w: %s", ErrInvalidFilePath, filePath)
	}

	fh, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer func() { _ = fh.Close() }()

	_, err = fh.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	err = fh.Sync()
	if err != nil {
		return fmt.Errorf("failed to sync file: %w", err)
	}

	return nil
}

// IsSaved checks whether this submission has already been saved to disk by
// looking for the presence of the associated HTML /view/ file. The HTML file
// serves as a marker that indicates successful completion of the download.
//
// The method uses a glob pattern to match the expected HTML filename format:
// "<original_filename>.<submission_id>.html"
//
// Returns:
//   - bool: true if the submission has been saved (HTML metadata file exists), false otherwise
func (s *Submission) IsSaved() bool {
	filenameGlob := fmt.Sprintf("*.%d.html", s.id)
	pathGlob := filepath.Join(s.submissionDir, filenameGlob)

	matches, err := filepath.Glob(pathGlob)
	if err != nil {
		s.logger.Error("IsSaved: glob error", "pattern", pathGlob, "error", err)
		// The only possible error here is a malformed pattern, which should
		// never happen given how we construct the glob.
		fatalInvariant(err)
	}

	return len(matches) > 0
}

// saveSubmissionFiles saves the downloaded submission file and the HTML /view/
// page to disk. The HTML page is saved only after the file is successfully
// written to ensure the submission can be retried if interrupted.
//
// Parameters:
//   - filename: The original filename of the downloaded submission file
//   - fileContent: The byte content of the downloaded submission file
//   - pageContent: The byte content of the HTML /view/ page
//
// Returns:
//   - error: Any error encountered during the save process
func (s *Submission) saveSubmissionFiles(filename string, fileContent []byte, pageContent []byte) error {
	// Save the downloaded file
	filePath := filepath.Join(s.submissionDir, filename)
	err := WriteAndFsyncFile(filePath, fileContent)
	if err != nil {
		return fmt.Errorf("failed to save file: %w", err)
	}

	// Save the HTML page only after saving the file.  This ensures the
	// submission will be retried if we get interrupted.
	htmlFilename := fmt.Sprintf("%s.%d.html", filename, s.id)
	htmlPath := filepath.Join(s.submissionDir, htmlFilename)
	htmlTempfile := htmlPath + ".tmp"

	// Write to a temp file first
	err = WriteAndFsyncFile(htmlTempfile, pageContent)
	if err != nil {
		return fmt.Errorf("failed to save HTML page: %w", err)
	}

	// Rename to final filename
	err = os.Rename(htmlTempfile, htmlPath)
	if err != nil {
		return fmt.Errorf("failed to finalize HTML page save: %w", err)
	}

	s.logger.Info("Saved submission", "id", s.id, "file", filePath)
	return nil
}
