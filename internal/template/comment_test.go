package template

import (
	"strings"
	"testing"
	"time"
)

func Test_MarkdownizeComment(t *testing.T) {
	// html.UnescapeString is deliberately not used, so entities outside the
	// whitelist must survive untouched.
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
		{
			name:  "whitespace only",
			input: "   \n\t ",
			want:  "   \n\t ",
		},
		{
			name:  "plain text, no html",
			input: "just a thought, no links here",
			want:  "just a thought, no links here",
		},
		{
			name:  "basic anchor",
			input: `Hello <a href="https://x.com">my site</a> end`,
			want:  `Hello [my site](https://x.com) end`,
		},
		{
			name:  "url as text",
			input: `<a href="https://x.com/a">https://x.com/a</a>`,
			want:  `[https://x.com/a](https://x.com/a)`,
		},
		{
			name:  "multiple anchors",
			input: `see <a href="https://x.com">x</a> and <a href="http://y.org/a">y</a> done`,
			want:  `see [x](https://x.com) and [y](http://y.org/a) done`,
		},
		{
			name:  "entity unescape in text",
			input: `<a href="https://x.com">Tom &amp; Jerry</a>`,
			want:  `[Tom & Jerry](https://x.com)`,
		},
		{
			name:  "all whitelisted entities unescaped",
			input: `<a href="https://x.com">a &amp; b &lt; c &gt; d &quot;e&quot; &#39;f&#39;</a>`,
			want:  `[a & b < c > d "e" 'f'](https://x.com)`,
		},
		{
			name:  "entity outside whitelist preserved",
			input: `<a href="https://x.com">a&nbsp;b &copy;</a>`,
			want:  `[a&nbsp;b &copy;](https://x.com)`,
		},
		{
			name:  "entities in the url are not unescaped",
			input: `<a href="https://x.com?a=1&amp;b=2">t</a>`,
			want:  `[t](https://x.com?a=1&amp;b=2)`,
		},
		{
			name:  "numeric entity outside whitelist preserved",
			input: `<a href="https://x.com">a&#8212;b &#x26; c</a>`,
			want:  `[a&#8212;b &#x26; c](https://x.com)`,
		},
		{
			name:  "bare ampersand preserved",
			input: `<a href="https://x.com">a & b</a>`,
			want:  `[a & b](https://x.com)`,
		},
		{
			name:  "text containing a tag is rejected",
			input: `<a href="https://x.com">a<b>c</a>`,
			want:  `<a href="https://x.com">a<b>c</a>`,
		},
		{
			name:  "non-http href with tag in text is rejected",
			input: `<a href="x">a<b>c</a>`,
			want:  `<a href="x">a<b>c</a>`,
		},
		{
			name:  "nested anchor in text is rejected",
			input: `<a href="https://x.com"><b>bold</b></a>`,
			want:  `<a href="https://x.com"><b>bold</b></a>`,
		},
		{
			name:  "non-anchor tags untouched",
			input: `<b>bold</b> <img src=x>`,
			want:  `<b>bold</b> <img src=x>`,
		},
		{
			name:  "protocol-relative href not converted",
			input: `<a href="//x">t</a>`,
			want:  `<a href="//x">t</a>`,
		},
		{
			name:  "javascript href not converted",
			input: `<a href="javascript:alert(1)">t</a>`,
			want:  `<a href="javascript:alert(1)">t</a>`,
		},
		{
			name:  "data href not converted",
			input: `<a href="data:text/html,x">t</a>`,
			want:  `<a href="data:text/html,x">t</a>`,
		},
		{
			name:  "mailto href not converted",
			input: `<a href="mailto:a@b.c">t</a>`,
			want:  `<a href="mailto:a@b.c">t</a>`,
		},
		{
			name:  "anchor without href not converted",
			input: `<a name="x">t</a>`,
			want:  `<a name="x">t</a>`,
		},
		{
			name:  "extra attributes ignored",
			input: `<a href="https://x.com" target="_blank" rel="n">t</a>`,
			want:  `[t](https://x.com)`,
		},
		{
			name:  "single-quoted href not converted",
			input: "<a href='https://x.com'>t</a>",
			want:  "<a href='https://x.com'>t</a>",
		},
		{
			name:  "unclosed anchor untouched",
			input: `<a href="https://x.com">t`,
			want:  `<a href="https://x.com">t`,
		},
		{
			name:  "html outside anchors survives a converted anchor",
			input: `<b>hi</b> <a href="https://x.com">t</a> <img src=x>`,
			want:  `<b>hi</b> [t](https://x.com) <img src=x>`,
		},
		{
			name:  "empty anchor text converted",
			input: `<a href="https://x.com"></a>`,
			want:  `[](https://x.com)`,
		},
		{
			name:  "greater-than in an attribute value is preserved",
			input: `<a href="https://x.com" title="a>b">t</a>`,
			want:  `<a href="https://x.com" title="a>b">t</a>`,
		},
		{
			name:  "stray quote in attributes is preserved",
			input: `<a href="https://x.com/a"">t</a>`,
			want:  `<a href="https://x.com/a"">t</a>`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MarkdownizeComment(tc.input)
			if got != tc.want {
				t.Errorf("MarkdownizeComment(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func Test_MarkdownizeComment_Deterministic(t *testing.T) {
	inputs := []string{
		`Hello <a href="https://x.com">my site</a> end`,
		`<a href="https://x.com">a</a><a href="http://y.org">b</a>`,
		`<b>bold</b> <a href="https://x.com">t</a> <img src=x>`,
		"",
	}

	for _, in := range inputs {
		first := MarkdownizeComment(in)
		second := MarkdownizeComment(in)
		if first != second {
			t.Errorf("MarkdownizeComment(%q) returned %q then %q, want the same output", in, first, second)
		}
	}
}

// bodyMarker splits the rendered document at the body heading, since the front
// matter carries the raw comment on purpose.
const bodyMarker = "### My thoughts\n"

// Test_Post_ToMarkdown_MarkdownizesComment pins the normalization into the
// rendered body, so an HTML anchor from Newsblur cannot reach the post.
func Test_Post_ToMarkdown_MarkdownizesComment(t *testing.T) {
	p := Post{
		Title:   "A Title",
		URL:     "https://example.com/story",
		Comment: `Loved <a href="https://x.com">this piece &amp; more</a> by Tom &amp; Jerry`,
		Date:    time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}

	buf, err := p.ToMarkdown()
	if err != nil {
		t.Fatalf("ToMarkdown returned error: %v", err)
	}

	body := buf.String()

	// The front matter keeps the raw comment; only the body is normalized.
	_, bodySection, ok := strings.Cut(body, bodyMarker)
	if !ok {
		t.Fatalf("body does not contain %q:\n%s", bodyMarker, body)
	}

	// The anchor text is unescaped; the entity outside the anchor is preserved
	// verbatim, since only whitelisted anchor text is rewritten.
	want := "Loved [this piece & more](https://x.com) by Tom &amp; Jerry"
	if !strings.Contains(bodySection, want) {
		t.Errorf("body does not contain %q:\n%s", want, body)
	}
	if strings.Contains(bodySection, "<a href") {
		t.Errorf("body still contains an HTML anchor:\n%s", body)
	}
}
