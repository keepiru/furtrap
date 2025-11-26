package main

// SPDX-License-Identifier: GPL-3.0-only

import (
	"log/slog"
	"path/filepath"
)

// Scraper manages the overall scraping process, coordinating the retrieval of
// watchlist data and downloading of submissions from multiple artists.
type Scraper struct {
	logger     *slog.Logger
	client     Client
	watcher    string
	artists    []string
	reCrawl    bool
	skipScraps bool
	outputDir  string
}

// NewScraper creates a new Scraper instance with the specified logger,
// configuration, and HTTP client. The scraper will use these settings to control crawling
// behavior, output directories, and request handling.
//
// Parameters:
//   - logger: Logger instance for writing log messages
//   - client: HTTP client interface for making web requests
//   - watcher: Username whose watchlist will be scraped
//   - artists: Artist usernames to download
//   - reCrawl: Whether to re-crawl existing submissions
//   - skipScraps: Whether to skip downloading scraps
//   - outputDir: Base directory where artist subdirectories will be created
//
// Returns:
//   - *Scraper: A new Scraper instance ready for use
func NewScraper(
	logger *slog.Logger,
	client Client,
	watcher string,
	artists []string,
	reCrawl bool,
	skipScraps bool,
	outputDir string,
) *Scraper {
	return &Scraper{
		logger:     logger,
		client:     client,
		watcher:    watcher,
		artists:    artists,
		reCrawl:    reCrawl,
		skipScraps: skipScraps,
		outputDir:  outputDir,
	}
}

// Run executes the complete scraping process by retrieving the watchlist for
// the specified user, then downloading submissions from each artist on the
// list.
//
// Returns:
//   - error: Any error encountered during the scraping process, nil on success
func (s *Scraper) Run() error {
	s.logger.Debug("Scraper.Run called")
	s.logger.Info("Scraper running with config",
		"watcher", s.watcher, "artists", s.artists, "reCrawl", s.reCrawl, "skipScraps", s.skipScraps)

	var artists []*Artist

	// If a username is provided, get artists from their watchlist
	if s.watcher != "" {
		watchlist, err := GetArtistsFromWatchlist(s.logger, s.client, s.watcher, s.outputDir)
		if err != nil {
			return err
		}
		artists = append(artists, watchlist...)
	}

	// If an artist is provided, add them directly
	for _, artist := range s.artists {
		artistDir := filepath.Join(s.outputDir, artist)
		artistObj := NewArtist(s.logger, s.client, artist, artistDir)
		artists = append(artists, artistObj)
	}

	// The main loop.  Get submissions from each artist and save them.
	for _, artist := range artists {
		submissions, err := artist.Submissions(s.reCrawl, s.skipScraps)
		if err != nil {
			return err
		}

		for _, submission := range submissions {
			err := submission.Save()
			if err != nil {
				return err
			}
		}
	}
	return nil
}
