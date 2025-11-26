package main_test

// SPDX-License-Identifier: GPL-3.0-only

import (
	main "furtrap"
	"io"
	"os"
	"testing"

	"github.com/spf13/pflag"
	"gotest.tools/assert"
)

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected main.Config
	}{
		{
			name: "only username provided",
			args: []string{"-u", "testuser"},
			expected: main.Config{Debug: false, ReCrawl: false, SkipScraps: false, NoThrottle: false,
				Username: "testuser", OutputDir: "dl", CookieFile: ""},
		},
		{
			name: "debug flag with username",
			args: []string{"-d", "-u", "testuser"},
			expected: main.Config{Debug: true, ReCrawl: false, SkipScraps: false, NoThrottle: false,
				Username: "testuser", OutputDir: "dl", CookieFile: ""},
		},
		{
			name: "reget flag with username",
			args: []string{"-r", "-u", "testuser"},
			expected: main.Config{Debug: false, ReCrawl: true, SkipScraps: false, NoThrottle: false,
				Username: "testuser", OutputDir: "dl", CookieFile: ""},
		},
		{
			name: "scraps flag with username",
			args: []string{"-s", "-u", "testuser"},
			expected: main.Config{Debug: false, ReCrawl: false, SkipScraps: true, NoThrottle: false,
				Username: "testuser", OutputDir: "dl", CookieFile: ""},
		},
		{
			name: "no throttle flag with username",
			args: []string{"-n", "-u", "testuser"},
			expected: main.Config{Debug: false, ReCrawl: false, SkipScraps: false, NoThrottle: true,
				Username: "testuser", OutputDir: "dl", CookieFile: ""},
		},
		{
			name: "all flags with username",
			args: []string{"-d", "-r", "-s", "-n", "-u", "testuser"},
			expected: main.Config{Debug: true, ReCrawl: true, SkipScraps: true, NoThrottle: true,
				Username: "testuser", OutputDir: "dl", CookieFile: ""},
		},
		{
			name: "mixed flags with username",
			args: []string{"-d", "-s", "-u", "testuser"},
			expected: main.Config{Debug: true, ReCrawl: false, SkipScraps: true, NoThrottle: false,
				Username: "testuser", OutputDir: "dl", CookieFile: ""},
		},
		{
			name: "custom output directory",
			args: []string{"-u", "testuser", "-o", "custom_output"},
			expected: main.Config{Debug: false, ReCrawl: false, SkipScraps: false, NoThrottle: false,
				Username: "testuser", OutputDir: "custom_output", CookieFile: ""},
		},
		{
			name: "cookie file provided",
			args: []string{"-u", "testuser", "-c", "cookies.txt"},
			expected: main.Config{Debug: false, ReCrawl: false, SkipScraps: false, NoThrottle: false,
				Username: "testuser", OutputDir: "dl", CookieFile: "cookies.txt"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset pflag.CommandLine for each test
			pflag.CommandLine = pflag.NewFlagSet(os.Args[0], pflag.ExitOnError)

			// Set os.Args to simulate command line
			oldArgs := os.Args
			os.Args = append([]string{"cmd"}, tt.args...)
			defer func() { os.Args = oldArgs }()

			config := main.ParseFlags()

			assert.DeepEqual(t, config, tt.expected)
		})
	}
}

func TestSetupLogging(t *testing.T) {
	tests := []struct {
		name  string
		debug bool
	}{
		{
			name:  "info level logging",
			debug: false,
		},
		{
			name:  "debug level logging",
			debug: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(_ *testing.T) {
			// Just ensure it doesn't panic
			main.CreateLogger(io.Discard, tt.debug)
		})
	}
}
