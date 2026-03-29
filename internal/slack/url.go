package slack

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

func ParseThreadURL(rawURL string) (channelID, timestamp string, err error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return "", "", fmt.Errorf("invalid URL: %q", rawURL)
	}

	if !strings.HasSuffix(u.Host, ".slack.com") {
		return "", "", fmt.Errorf("not a Slack URL: %q", u.Host)
	}

	// Path: /archives/{channelID}/p{timestamp}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 3 || parts[0] != "archives" {
		return "", "", fmt.Errorf("unexpected path format: %q", u.Path)
	}

	channelID = parts[1]
	tsRaw := parts[2]

	if !strings.HasPrefix(tsRaw, "p") {
		return "", "", fmt.Errorf("timestamp must start with 'p': %q", tsRaw)
	}
	tsDigits := tsRaw[1:]

	for _, r := range tsDigits {
		if !unicode.IsDigit(r) {
			return "", "", fmt.Errorf("non-numeric timestamp: %q", tsDigits)
		}
	}

	if len(tsDigits) <= 6 {
		return "", "", fmt.Errorf("timestamp too short: %q", tsDigits)
	}

	// Insert decimal before last 6 digits: "1774549751570729" → "1774549751.570729"
	splitAt := len(tsDigits) - 6
	timestamp = tsDigits[:splitAt] + "." + tsDigits[splitAt:]

	return channelID, timestamp, nil
}
