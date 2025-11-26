package main_test

// SPDX-License-Identifier: GPL-3.0-only

import (
	"fmt"
	main "furtrap"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/assert"
)

func TestGetArtists(t *testing.T) {
	client := NewTestClient()

	t.Run("Get a list of Artists", func(t *testing.T) {
		// Aside from making sure it loads the list from all 4 pages, this also
		// implicitly tests that page 5 is NOT loaded.  Page 5 would 404,
		// causing an error to be returned.
		got, err := main.GetArtistsFromWatchlist(NewTestLogger(t), client, "test-watcher", t.TempDir())
		assert.NilError(t, err)
		assert.Equal(t, len(got), 601)
		assert.Equal(t, got[0].Username(), "lorem")
		assert.Equal(t, got[1].Username(), "ipsum")
		assert.Equal(t, got[150].Username(), "anim2")
		assert.Equal(t, got[450].Username(), "voluptatum5")
		assert.Equal(t, got[599].Username(), "quis7")
		assert.Equal(t, got[600].Username(), "nostrud7")
	})

	t.Run("error when getting artists from invalid watchlist user", func(t *testing.T) {
		_, err := main.GetArtistsFromWatchlist(NewTestLogger(t), client, "invalidusername", t.TempDir())
		assert.ErrorContains(t, err, "failed to fetch watchlist page")
	})
}

func TestGetWatchlist(t *testing.T) {
	client := NewTestClient()

	t.Run("Get watchlist", func(t *testing.T) {
		got, err := main.GetWatchlist(NewTestLogger(t), client, "test-watcher")
		assert.NilError(t, err)
		assert.Equal(t, len(got), 601)
		assert.DeepEqual(t, got[0:3], []string{"lorem", "ipsum", "dolor"})
	})

	t.Run("Panic when maximum watchlist pages exceeded", func(t *testing.T) {
		// This also implicitly tests that we stop at page 100 and do not try to
		// load page 101, which would 404 and return a different error.
		client := NewTestClient()

		// Set up responses for 100 pages
		for i := 1; i <= 100; i++ {
			url := fmt.Sprintf("https://www.furaffinity.net/watchlist/by/infinite-watcher/%d", i)
			// Create unique usernames for each page
			response := fmt.Appendf(nil, `<a href="/user/artist%d/">Artist %d</a> <a href="/user/artist%d/">Artist %d</a>`,
				i*2-1, i*2-1, i*2, i*2)
			client.SetResponse(url, response, nil)
		}

		// Discard logs to avoid clutter
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))

		got := CapturePanic(t, func() {
			_, err := main.GetWatchlist(logger, client, "infinite-watcher")
			assert.NilError(t, err)
		})

		assert.Equal(t, fmt.Sprint(got), "maximum watchlist pages exceeded")
	})
}

func TestSubmissions(t *testing.T) {
	client := NewTestClient()
	t.Run("Get all submissions", func(t *testing.T) {
		artist := main.NewArtist(NewTestLogger(t), client, "test-artist", t.TempDir())
		submissions, err := artist.Submissions(false, true)
		assert.NilError(t, err)
		assert.Equal(t, len(submissions), 56)
		assert.Equal(t, submissions[0].ID(), uint64(244))
		assert.Equal(t, submissions[2].ID(), uint64(246))
		assert.Equal(t, submissions[32].ID(), uint64(276))
		assert.Equal(t, submissions[54].ID(), uint64(298))
		assert.Equal(t, submissions[55].ID(), uint64(299))
	})

	t.Run("Get submissions with synthetic data", func(t *testing.T) {
		artist := main.NewArtist(NewTestLogger(t), client, "artist-with-two-submissions", t.TempDir())
		submissions, err := artist.Submissions(false, true)
		assert.NilError(t, err)
		assert.Equal(t, len(submissions), 2)
		assert.Equal(t, submissions[0].ID(), uint64(102))
		assert.Equal(t, submissions[1].ID(), uint64(101))
	})

	t.Run("Get submissions and scraps with synthetic data", func(t *testing.T) {
		artist := main.NewArtist(NewTestLogger(t), client, "artist-with-two-submissions", t.TempDir())
		submissions, err := artist.Submissions(false, false)
		assert.NilError(t, err)
		assert.Equal(t, len(submissions), 4)
		assert.Equal(t, submissions[0].ID(), uint64(102))
		assert.Equal(t, submissions[1].ID(), uint64(101))
		assert.Equal(t, submissions[2].ID(), uint64(104))
		assert.Equal(t, submissions[3].ID(), uint64(103))
	})

	t.Run("ReCrawl", func(t *testing.T) {
		client.SetResponse("https://www.furaffinity.net/gallery/testartist/1",
			[]byte(`<a href="/view/103"> <img> </a> <a href="/view/104"> <img> </a>`),
			nil)

		client.SetResponse("https://www.furaffinity.net/gallery/testartist/2",
			[]byte(`<a href="/view/55555"> <img> </a>`),
			nil)

		client.SetResponse("https://www.furaffinity.net/gallery/testartist/3",
			[]byte(``),
			nil)

		artistDir := t.TempDir()

		fn := filepath.Join(artistDir, "12345.testartist_test-image-4.104.html")
		//#nosec G304: filename is from test data
		f, err := os.Create(fn)
		assert.NilError(t, err)
		err = f.Close()
		assert.NilError(t, err)

		artist := main.NewArtist(NewTestLogger(t), client, "testartist", artistDir)

		t.Run("Stop crawling watchlist when we reach known submission", func(t *testing.T) {
			submissions, err := artist.Submissions(false, true)
			assert.NilError(t, err)
			assert.Equal(t, len(submissions), 1)
			assert.Equal(t, submissions[0].ID(), uint64(103))

			// The fact that there were was only 1 submission and it wasn't
			// 55555 implies that crawling stopped before 55555, but let's be
			// explicit about the intended behavior:
			for _, submission := range submissions {
				assert.Assert(t, submission.ID() != uint64(55555))
			}
		})

		t.Run("ReCrawl gets all submissions including known ones", func(t *testing.T) {
			submissions, err := artist.Submissions(true, true)
			assert.NilError(t, err)
			assert.Equal(t, len(submissions), 3)
			assert.Equal(t, submissions[0].ID(), uint64(55555))
			assert.Equal(t, submissions[1].ID(), uint64(104))
			assert.Equal(t, submissions[2].ID(), uint64(103))
		})
	})

	t.Run("Panic when maximum gallery pages exceeded", func(t *testing.T) {
		// This also implicitly tests that we stop at page 1000 and do not try
		// to load page 1001, which would 404 and return a different error.
		client := NewTestClient()

		// Set up responses for 1000 pages
		for i := 1; i <= 1000; i++ {
			url := fmt.Sprintf("https://www.furaffinity.net/gallery/infinite-artist/%d", i)
			response := fmt.Appendf(nil, `<a href="/view/%d"> <img> </a>`, 100000+i)
			client.SetResponse(url, response, nil)
		}

		// Discard logs to avoid clutter
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		artist := main.NewArtist(logger, client, "infinite-artist", t.TempDir())

		got := CapturePanic(t, func() {
			_, err := artist.Submissions(false, true)
			assert.NilError(t, err)
		})

		assert.Equal(t, fmt.Sprint(got), "maximum gallery pages exceeded")
	})
}
