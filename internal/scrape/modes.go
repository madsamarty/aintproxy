package scrape

import "net/url"

// Mode is a site-specific scraper that turns a URL into structured data.
type Mode struct {
	Name        string
	Description string
	Match       func(u *url.URL) bool
	Run         func(rawURL string, o Options) (any, error)
}

var modes = []Mode{metaPagesMode}

// Modes lists every registered scraper mode.
func Modes() []Mode { return modes }

// ModeByName returns the mode with the given name.
func ModeByName(name string) (Mode, bool) {
	for _, m := range modes {
		if m.Name == name {
			return m, true
		}
	}
	return Mode{}, false
}

// DetectMode picks the first mode whose Match accepts rawURL.
func DetectMode(rawURL string) (Mode, bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return Mode{}, false
	}
	for _, m := range modes {
		if m.Match(u) {
			return m, true
		}
	}
	return Mode{}, false
}
