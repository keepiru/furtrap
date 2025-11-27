// command furtrap
package main

// SPDX-License-Identifier: GPL-3.0-only

// This is the main entry point for furtrap, a FurAffinity
// scraper and download tool.

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/pflag"
)

var (
	// Build information, set via -ldflags at build time.
	buildGitCommitHash = "unknown"
	buildTimestamp     = "unknown"
)

// Config holds the application configuration parsed from CLI flags.
type Config struct {
	Debug      bool     // Enable debug logging
	ReCrawl    bool     // Re-crawl all the way through galleries
	SkipScraps bool     // Skip downloading scraps
	NoThrottle bool     // Disable wait time between requests
	Username   string   // Username to scrape watchlist from
	OutputDir  string   // Output directory for downloads
	CookieFile string   // Path to cookies.txt file
	Artists    []string // Artists to scrape submissions from
}

func main() {
	config := ParseFlags()
	logger := CreateLogger(os.Stderr, config.Debug)
	client := NewHTTPClient(logger)
	if config.NoThrottle {
		// Even without load throttling, we still want some delay to avoid hammering the server
		client.SetDelayFunc(func(int) { time.Sleep(defaultDelayTime) })
	}
	if config.CookieFile != "" {
		err := client.LoadCookies(config.CookieFile)
		if err != nil {
			logger.Error("Failed to load cookies", "file", config.CookieFile, "error", err)
			os.Exit(1)
		}
	}

	logger.Info("Starting furtrap",
		"commit", buildGitCommitHash,
		"buildDate", buildTimestamp)
	logger.Debug("Configuration", "config", fmt.Sprintf("%+v", config))

	scraper := NewScraper(
		logger,
		client,
		config.Username,
		config.Artists,
		config.ReCrawl,
		config.SkipScraps,
		config.OutputDir)

	err := scraper.Run()
	if err != nil {
		logger.Error("Application error", "error", err)
		os.Exit(1)
	}

	logger.Info("Done!")
}

// ParseFlags parses command line flags and returns a Config.
//
// Returns:
//   - Config: A populated configuration struct with values from CLI flags
func ParseFlags() Config {
	config := Config{}

	pflag.BoolVarP(&config.Debug, "debug", "d", false, "Enable debug logging")
	pflag.BoolVarP(&config.ReCrawl, "recrawl", "r", false, "Re-crawl galleries looking for missed submissions")
	pflag.BoolVarP(&config.SkipScraps, "skip-scraps", "s", false, "Don't download scraps")
	pflag.BoolVarP(&config.NoThrottle, "no-throttle", "n", false, "Disable wait time between requests")
	pflag.StringVarP(&config.Username, "username", "u", "", "Download all artists in this user's watchlist")
	pflag.StringSliceVarP(&config.Artists, "artists", "a", nil,
		"Download all submissions from comma-separated list of artists")
	pflag.StringVarP(&config.OutputDir, "output", "o", "dl", "Output directory for downloads")
	pflag.StringVarP(&config.CookieFile, "cookies", "c", "", "Path to cookies.txt file")

	pflag.Parse()

	// Check for unexpected positional arguments
	if pflag.NArg() > 0 || (config.Username == "" && config.Artists == nil) {
		fmt.Fprintf(os.Stderr,
			"usage: %s [-drsn] (-u <username> | -a <artist1>[,artist2,...]) [-o <output_dir>] [-c <cookies_file>]\n\n",
			os.Args[0])
		pflag.PrintDefaults()
		fmt.Fprintln(os.Stderr, "\nEither --username or --artists must be specified")
		os.Exit(1)
	}

	return config
}

// CreateLogger creates a new slog.Logger instance with the specified output
// writer and log level based on the debug flag.
//
// Parameters:
//   - w: The io.Writer where log output will be written
//   - debug: If true, sets log level to Debug; otherwise sets to Info
//
// Returns:
//   - *slog.Logger: A configured logger instance
func CreateLogger(w io.Writer, debug bool) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}

	handler := slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: level,
	})
	return slog.New(handler)
}

// fatalInvariant intentionally panics when a fundamental assumption is broken.
// These checks keep the crawler from continuing in a corrupted state, so we do
// not attempt to recover or retry if one of them triggers.  This is used in
// cases where an error must not be returned up the stack, because the caller
// must not be allowed to retry or continue processing.
func fatalInvariant(message any) {
	panic(message)
}
