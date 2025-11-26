package main_test

// SPDX-License-Identifier: GPL-3.0-only

import (
	main "furtrap"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/assert"
)

func TestScraperRun(t *testing.T) {
	// This is an integration test which runs through most of the program's
	// happy path functionality, scraping a test watchlist and verifying that
	// all files are downloaded as expected.
	t.Run("two submissions, two scraps", func(t *testing.T) {
		// We'll try this two ways: once specifying an artist, once specifying a
		// watcher.  Both cases should download the same files.
		tests := []struct {
			name    string
			watcher string
			artists []string
		}{
			{
				"one artist",
				"",
				[]string{"artist-with-two-submissions"},
			},
			{
				"one watcher",
				"watcher-with-one-artist",
				[]string{},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				client := NewTestClient()
				tempdir := t.TempDir()

				// Perform the test run
				scraper := main.NewScraper(NewTestLogger(t), client, tt.watcher, tt.artists, false, false, tempdir)
				err := scraper.Run()
				assert.NilError(t, err)

				// Then verify that all expected files were downloaded
				files := []struct {
					wanturi string
					havefn  string
				}{
					{
						"https://www.furaffinity.net/view/101",
						"artist-with-two-submissions/1111111111.artist-with-two-submissions_test-image-1.jpg.101.html",
					},
					{
						"https://www.furaffinity.net/view/102",
						"artist-with-two-submissions/2222222222.artist-with-two-submissions_test-image-2.png.102.html",
					},
					{
						"https://www.furaffinity.net/view/103",
						"artist-with-two-submissions/scraps/3333333333.artist-with-two-submissions_scrap-image-1.png.103.html",
					},
					{
						"https://www.furaffinity.net/view/104",
						"artist-with-two-submissions/scraps/4444444444.artist-with-two-submissions_scrap-image-2.png.104.html",
					},
					{
						"https://d.furaffinity.net/art/artist-with-two-submissions/1111111111/" +
							"1111111111.artist-with-two-submissions_test-image-1.jpg",
						"artist-with-two-submissions/1111111111.artist-with-two-submissions_test-image-1.jpg",
					},
					{
						"https://d.furaffinity.net/art/artist-with-two-submissions/2222222222/" +
							"2222222222.artist-with-two-submissions_test-image-2.png",
						"artist-with-two-submissions/2222222222.artist-with-two-submissions_test-image-2.png",
					},
					{
						"https://d.furaffinity.net/art/artist-with-two-submissions/3333333333/" +
							"3333333333.artist-with-two-submissions_scrap-image-1.png",
						"artist-with-two-submissions/scraps/3333333333.artist-with-two-submissions_scrap-image-1.png",
					},
					{
						"https://d.furaffinity.net/art/artist-with-two-submissions/4444444444/" +
							"4444444444.artist-with-two-submissions_scrap-image-2.png",
						"artist-with-two-submissions/scraps/4444444444.artist-with-two-submissions_scrap-image-2.png",
					},
				}
				for _, file := range files {
					// Get the expected content from the test server
					want, err := client.Get(file.wanturi)
					assert.NilError(t, err)

					// Confirm the file was downloaded correctly
					havePath := filepath.Join(tempdir, file.havefn)
					//#nosec G304: filename is from test data
					have, err := os.ReadFile(havePath)
					assert.NilError(t, err)

					assert.DeepEqual(t, have, want)
				}
			})
		}
	})
}
