package slack

import (
	"regexp"
	"strings"
)

var (
	codeBlockRe     = regexp.MustCompile("(?s)```.*?```")
	codeSpanRe      = regexp.MustCompile("`[^`]+`")
	doubleBoldRe    = regexp.MustCompile(`\*\*(.+?)\*\*`)
	underBoldRe     = regexp.MustCompile(`__(.+?)__`)
	mdLinkRe        = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	headingRe       = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	strikethroughRe = regexp.MustCompile(`~~(.+?)~~`)
)

// MarkdownToMrkdwn converts Markdown formatting to Slack mrkdwn format.
// Conversions: **bold**/​__bold__ → *bold*, [text](url) → <url|text>,
// ## heading → *heading*, ~~strike~~ → ~strike~.
// Code spans and code blocks are left unchanged.
// URLs in links are restricted to http/https to prevent Slack special-mention injection.
func MarkdownToMrkdwn(text string) string {
	// Extract code blocks and spans to protect them from conversion
	type placeholder struct {
		token    string
		original string
	}

	var placeholders []placeholder
	counter := 0

	replace := func(re *regexp.Regexp, s string) string {
		return re.ReplaceAllStringFunc(s, func(match string) string {
			token := "\x00CODE" + string(rune(counter)) + "\x00"
			counter++
			placeholders = append(placeholders, placeholder{token, match})
			return token
		})
	}

	result := replace(codeBlockRe, text)
	result = replace(codeSpanRe, result)

	result = headingRe.ReplaceAllStringFunc(result, func(match string) string {
		content := headingRe.FindStringSubmatch(match)[1]
		content = doubleBoldRe.ReplaceAllString(content, "$1")
		content = underBoldRe.ReplaceAllString(content, "$1")
		return "*" + content + "*"
	})
	result = doubleBoldRe.ReplaceAllString(result, "*$1*")
	result = underBoldRe.ReplaceAllString(result, "*$1*")
	result = strikethroughRe.ReplaceAllString(result, "~$1~")
	result = mdLinkRe.ReplaceAllStringFunc(result, func(match string) string {
		parts := mdLinkRe.FindStringSubmatch(match)
		linkText, url := parts[1], parts[2]
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			return linkText
		}
		return "<" + url + "|" + linkText + ">"
	})

	for _, p := range placeholders {
		result = strings.Replace(result, p.token, p.original, 1)
	}

	return result
}
