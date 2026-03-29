package slack

import "testing"

func TestMarkdownToMrkdwn(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "double asterisk bold",
			input: "this is **bold** text",
			want:  "this is *bold* text",
		},
		{
			name:  "underscore bold",
			input: "this is __bold__ text",
			want:  "this is *bold* text",
		},
		{
			name:  "markdown link",
			input: "check [Google](https://google.com) out",
			want:  "check <https://google.com|Google> out",
		},
		{
			name:  "multiple bold",
			input: "**one** and **two**",
			want:  "*one* and *two*",
		},
		{
			name:  "single asterisk italic unchanged",
			input: "this is *italic* text",
			want:  "this is *italic* text",
		},
		{
			name:  "code span unchanged",
			input: "run `**not bold**` command",
			want:  "run `**not bold**` command",
		},
		{
			name:  "code block unchanged",
			input: "text\n```\n**not bold**\n```\nmore",
			want:  "text\n```\n**not bold**\n```\nmore",
		},
		{
			name:  "mixed conversions",
			input: "**bold** and [link](http://example.com) and `code`",
			want:  "*bold* and <http://example.com|link> and `code`",
		},
		{
			name:  "already correct mrkdwn",
			input: "*bold* and <http://example.com|link>",
			want:  "*bold* and <http://example.com|link>",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "no markdown",
			input: "plain text",
			want:  "plain text",
		},
		{
			name:  "h2 heading",
			input: "## Section Title\nsome text",
			want:  "*Section Title*\nsome text",
		},
		{
			name:  "h3 heading",
			input: "### Sub Section\nmore text",
			want:  "*Sub Section*\nmore text",
		},
		{
			name:  "h1 heading",
			input: "# Top Level",
			want:  "*Top Level*",
		},
		{
			name:  "heading with bold content",
			input: "## **Already Bold**",
			want:  "*Already Bold*",
		},
		{
			name:  "strikethrough",
			input: "this is ~~deleted~~ text",
			want:  "this is ~deleted~ text",
		},
		{
			name:  "heading in code block unchanged",
			input: "```\n## not a heading\n```",
			want:  "```\n## not a heading\n```",
		},
		{
			name:  "mid-line hash not treated as heading",
			input: "use issue #123 for tracking",
			want:  "use issue #123 for tracking",
		},
		{
			name:  "strikethrough in code span unchanged",
			input: "run `~~not struck~~` here",
			want:  "run `~~not struck~~` here",
		},
		{
			name:  "non-http link stripped to text only",
			input: "[everyone](<!channel>)",
			want:  "everyone",
		},
		{
			name:  "http link converted normally",
			input: "[site](http://example.com)",
			want:  "<http://example.com|site>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MarkdownToMrkdwn(tt.input)
			if got != tt.want {
				t.Errorf("MarkdownToMrkdwn(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
