package main_test

// SPDX-License-Identifier: GPL-3.0-only

import (
	"errors"
	main "furtrap"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gotest.tools/assert"
)

func TestSubmission_ID(t *testing.T) {
	client := NewTestClient()

	t.Run("get URI from Submission", func(t *testing.T) {
		submission := main.NewSubmission(NewTestLogger(t), client, 12345, t.TempDir())
		assert.Equal(t, submission.ID(), uint64(12345))
	})
}

func TestSubmission_Save(t *testing.T) {
	client := NewTestClient()
	t.Run("Save a submission", func(t *testing.T) {
		submissionDir := t.TempDir()
		submission := main.NewSubmission(NewTestLogger(t), client, 101, submissionDir)
		err := submission.Save()
		assert.NilError(t, err)

		// The page should be saved
		savedPageFN := filepath.Join(submissionDir,
			"1111111111.artist-with-two-submissions_test-image-1.jpg.101.html")
		//#nosec G304 - filename is from test data
		savedPageContent, err := os.ReadFile(savedPageFN)
		assert.NilError(t, err)
		savedPageWant, err := client.Get("https://www.furaffinity.net/view/101")
		assert.NilError(t, err)
		assert.DeepEqual(t, savedPageWant, savedPageContent)

		// The actual submission should be saved
		savedImageFN := filepath.Join(submissionDir,
			"1111111111.artist-with-two-submissions_test-image-1.jpg")
		//#nosec G304 - filename is from test data
		SavedImageContent, err := os.ReadFile(savedImageFN)
		assert.NilError(t, err)
		assert.DeepEqual(t, SavedImageContent, []byte("one\n"))
	})

	t.Run("Save fails when client returns error for submission page", func(t *testing.T) {
		client.SetResponse(
			"https://www.furaffinity.net/view/12345",
			nil,
			errors.New("network error"), //nolint:err113 // dynamic test error
		)

		submission := main.NewSubmission(NewTestLogger(t), client, 12345, t.TempDir())
		err := submission.Save()
		assert.ErrorContains(t, err, "failed to get submission page")
		assert.ErrorContains(t, err, "network error")
	})

	t.Run("Save fails when no download link found", func(t *testing.T) {
		client.SetResponse(
			"https://www.furaffinity.net/view/12345",
			[]byte("<html><body><p>No download link here</p></body></html>"),
			nil,
		)

		submission := main.NewSubmission(NewTestLogger(t), client, 12345, t.TempDir())
		err := submission.Save()
		assert.Equal(t, err, main.ErrSubmissionImageNotFound)
	})

	t.Run("Save fails when download link has no href", func(t *testing.T) {
		client.SetResponse(
			"https://www.furaffinity.net/view/12345",
			[]byte("<html><body><a>Download</a></body></html>"),
			nil,
		)

		submission := main.NewSubmission(NewTestLogger(t), client, 12345, t.TempDir())
		err := submission.Save()
		assert.Equal(t, err, main.ErrSubmissionImageNotFound)
	})

	t.Run("Save fails when download link format is unexpected", func(t *testing.T) {
		client.SetResponse(
			"https://www.furaffinity.net/view/12345",
			[]byte(`<html><body><a href="http://example.com/file.jpg">Download</a></body></html>`),
			nil,
		)

		submission := main.NewSubmission(NewTestLogger(t), client, 12345, t.TempDir())
		err := submission.Save()
		assert.Assert(t, errors.Is(err, main.ErrUnexpectedLinkFormat))
	})

	t.Run("Save fails when download file request fails", func(t *testing.T) {
		client.SetResponse(
			"https://www.furaffinity.net/view/12345",
			[]byte(`<html><body><a href="//d.furaffinity.net/art/artist/file.jpg">Download</a></body></html>`),
			nil,
		)
		client.SetResponse(
			"https://d.furaffinity.net/art/artist/file.jpg",
			nil,
			errors.New("download failed"), //nolint:err113 // dynamic test error
		)

		submission := main.NewSubmission(NewTestLogger(t), client, 12345, t.TempDir())
		err := submission.Save()
		assert.ErrorContains(t, err, "failed to download file")
		assert.ErrorContains(t, err, "download failed")
	})

	t.Run("Save fails when target directory creation fails", func(t *testing.T) {
		// Create a read-only directory
		readOnlyDir := t.TempDir()
		//#nosec G302 - dir needs to be executable
		err := os.Chmod(readOnlyDir, 0500)
		assert.NilError(t, err)

		artistDir := filepath.Join(readOnlyDir, "testartist")
		submission := main.NewSubmission(NewTestLogger(t), client, 12345, artistDir)
		err = submission.Save()
		assert.ErrorContains(t, err, "failed to create target directory")
	})

	t.Run("Save fails when file write fails due to permissions", func(t *testing.T) {
		// Create a directory, then make it read-only after creation
		tempdir := t.TempDir()
		artistDir := filepath.Join(tempdir, "writetest")
		err := os.MkdirAll(artistDir, 0500)
		assert.NilError(t, err)

		submission := main.NewSubmission(NewTestLogger(t), client, 101, artistDir)
		err = submission.Save()
		assert.ErrorContains(t, err, "failed to save file")
	})

	t.Run("Save fails when downloaded file write fails due to permissions", func(t *testing.T) {
		// Create a directory and a file, make the file read-only to prevent overwrite
		artistDir := t.TempDir()

		// Create a read-only file with the same name as the download target
		imageFilename := "1111111111.artist-with-two-submissions_test-image-1.jpg"
		targetFile := filepath.Join(artistDir, imageFilename)
		err := os.WriteFile(targetFile, []byte("existing"), 0400) // read-only
		assert.NilError(t, err)

		submission := main.NewSubmission(NewTestLogger(t), client, 101, artistDir)
		err = submission.Save()
		assert.ErrorContains(t, err, "failed to save file")
	})

	t.Run("Save should no-op if file already exists", func(t *testing.T) {
		artistDir := t.TempDir()
		submission := main.NewSubmission(NewTestLogger(t), client, 101, artistDir)

		htmlFilename := "1111111111.artist-with-two-submissions_test-image-1.jpg.101.html"
		imageFilename := "1111111111.artist-with-two-submissions_test-image-1.jpg"
		htmlPath := filepath.Join(artistDir, htmlFilename)
		imagePath := filepath.Join(artistDir, imageFilename)

		// Write the file with old content
		err := os.WriteFile(htmlPath, []byte("old html content"), 0600)
		assert.NilError(t, err)

		// Set file to have a timestamp from 1990
		oldTime := time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC)
		err = os.Chtimes(htmlPath, oldTime, oldTime)
		assert.NilError(t, err)

		// Run Save() - should be a no-op since files exist
		err = submission.Save()
		assert.NilError(t, err)

		// Verify timestamps haven't changed
		htmlStatAfter, err := os.Stat(htmlPath)
		assert.NilError(t, err)
		mtimeStaysTheSame := htmlStatAfter.ModTime().Equal(oldTime)
		assert.Equal(t, mtimeStaysTheSame, true)

		// Verify content hasn't changed
		//#nosec G304: filename is from test data
		htmlContent, err := os.ReadFile(htmlPath)
		assert.NilError(t, err)
		assert.DeepEqual(t, htmlContent, []byte("old html content"))

		// Verify the image file was not created
		_, err = os.Stat(imagePath)
		assert.Assert(t, os.IsNotExist(err))
	})

	t.Run("Save returns without doing anything if submission download 404s", func(t *testing.T) {
		client.SetResponse(
			"https://www.furaffinity.net/view/54321",
			[]byte(`<html><body><a href="//d.furaffinity.net/art/artist/missing-file.jpg">Download</a></body></html>`),
			nil,
		)

		tempdir := t.TempDir()

		submission := main.NewSubmission(NewTestLogger(t), client, 54321, tempdir)
		err := submission.Save()
		assert.NilError(t, err)

		// Verify that no files were created
		files, err := os.ReadDir(tempdir)
		assert.NilError(t, err)
		assert.Equal(t, len(files), 0)
	})
}

func TestSubmission_IsSaved(t *testing.T) {
	t.Run("false if file does not exist", func(t *testing.T) {
		artistDir := t.TempDir()
		submission := main.NewSubmission(NewTestLogger(t), NewTestClient(), 12345, artistDir)
		got := submission.IsSaved()
		assert.Equal(t, got, false)
	})

	t.Run("true if file exists", func(t *testing.T) {
		artistDir := t.TempDir()
		fn := filepath.Join(artistDir, "randomname.png.12345.html")
		err := os.WriteFile(fn, []byte{}, 0600)
		assert.NilError(t, err)
		submission := main.NewSubmission(NewTestLogger(t), NewTestClient(), 12345, artistDir)
		got := submission.IsSaved()
		assert.Equal(t, got, true)
	})
}

func TestWriteAndFsyncFile(t *testing.T) {
	t.Run("successfully writes file", func(t *testing.T) {
		tempDir := t.TempDir()
		filePath := filepath.Join(tempDir, "test-file.txt")
		data := []byte("test data content")

		err := main.WriteAndFsyncFile(filePath, data)
		assert.NilError(t, err)

		// Verify file was created with correct content
		//#nosec G304 - filename is from test data
		savedContent, err := os.ReadFile(filePath)
		assert.NilError(t, err)
		assert.DeepEqual(t, savedContent, data)
	})

	t.Run("rejects directory traversal in file path", func(t *testing.T) {
		tempDir := t.TempDir()
		// Path with .. that would traverse outside tempDir.
		// Can't use filepath.Join because it would clean the path.
		filePath := tempDir + "/../traversal.txt"

		err := main.WriteAndFsyncFile(filePath, []byte("malicious data"))
		assert.ErrorContains(t, err, main.ErrInvalidFilePath.Error())
	})

	t.Run("rejects unclean file paths", func(t *testing.T) {
		tempDir := t.TempDir()
		// Path with unnecessary separators
		// Can't use filepath.Join because it would clean the path.
		filePath := tempDir + "/test//file.txt"

		err := main.WriteAndFsyncFile(filePath, []byte("test data"))
		assert.ErrorContains(t, err, main.ErrInvalidFilePath.Error())
	})

	t.Run("fails when directory does not exist", func(t *testing.T) {
		tempDir := t.TempDir()
		filePath := filepath.Join(tempDir, "nonexistent", "directory", "file.txt")

		err := main.WriteAndFsyncFile(filePath, []byte("test data"))
		assert.ErrorContains(t, err, "failed to create file")
		assert.ErrorContains(t, err, "no such file or directory")
	})

	t.Run("fails when target is a directory", func(t *testing.T) {
		tempDir := t.TempDir()
		// Try to write to the directory itself

		err := main.WriteAndFsyncFile(tempDir, []byte("test data"))
		assert.ErrorContains(t, err, "failed to create file")
		assert.ErrorContains(t, err, "is a directory")
	})

	t.Run("overwrites existing file", func(t *testing.T) {
		tempDir := t.TempDir()
		filePath := filepath.Join(tempDir, "overwrite-test.txt")

		// Write initial content
		err := os.WriteFile(filePath, []byte("old content"), 0600)
		assert.NilError(t, err)

		// Overwrite with new content
		newData := []byte("new content")
		err = main.WriteAndFsyncFile(filePath, newData)
		assert.NilError(t, err)

		// Verify new content
		//#nosec G304 - filename is from test data
		savedContent, err := os.ReadFile(filePath)
		assert.NilError(t, err)
		assert.DeepEqual(t, savedContent, newData)
	})
}
