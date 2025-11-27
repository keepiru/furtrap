# furtrap

A FurAffinity download tool.  There are many like it, but this one is mine.

This is a rewrite of an old one I've been using for 15+ years.  It was
battle-tested and worked, but due for an overhaul.  This inherits most of the
lessons I've learned about FA's quirks, but it's a lot cleaner and more
maintainable now.

This will download everything on your watchlist, or you can select a single
artist.  It keeps track of what it's done so it won't keep fetching things over
and over.  It can be canceled and restarted without trouble.

This doesn't try to handle logins.  You need to log in with your browser, then
export the "a" and "b" cookies.  This program then picks them up with the
--cookies option.

This will monitor the load on FA's site, and will pause when required.  This can
be overridden with --no-throttle to test if things are working correctly, but
you shouldn't do large downloads that way.  Leave the program running and it
will start making progress late at night (USA time).

## Installation

### Prerequisites

- Go 1.24 or later

### Building from source

```bash
go build -o furtrap
```

## Usage

```bash
furtrap [-drsn] (-u <username> | -a <artist1>[,artist2,...]) [-o <output_dir>] [-c <cookies_file>]
```

### Required Arguments

You must specify either a username or an artist:

- `-u, --username <username>` - Download all artists in this user's watchlist
- `-a, --artists <artist1>[,artist2,...]` - Download all submissions from this specific artist

### Optional Arguments

- `-o, --output <output_dir>` - Output directory for downloads (default: `dl`)
- `-c, --cookies <cookies_file>` - Path to cookies.txt file for authentication
- `-s, --skip-scraps` - Skip downloading scraps
- `-r, --recrawl` - Re-crawl galleries looking for missed submissions
- `-n, --no-throttle` - Disable wait time between requests (use responsibly!)
- `-d, --debug` - Enable debug logging

### Examples

Download all submissions from a specific artist:
```bash
./furtrap -a artist_username
```

Download all artists from a watchlist with authentication:
```bash
./furtrap -u my_username -c cookies.txt
```

Download to a custom directory with debug output:
```bash
./furtrap -a artist_username -o /path/to/downloads -d
```

### Getting cookies
1. Log in to FurAffinity in your browser
2. Use a browser extension like "cookies.txt" to export cookies
3. Save as `cookies.txt` in Netscape format

Or manually create a file using the format in `cookies.txt.example`.

## License

GPL 3.0
