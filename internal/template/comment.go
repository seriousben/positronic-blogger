package template

import (
	"html"
	"regexp"
)

// commentAnchorRegex matches the only HTML construct the normalizer rewrites: an
// anchor whose href is an absolute http(s) URL, optionally followed by more
// attributes, and whose text carries no '<'. Everything else, including every
// other tag, stays byte-identical. The attribute run may hold quoted values but
// must stop at the first '>' outside them, so an anchor whose attribute value
// itself contains '>' fails to match and is preserved.
var commentAnchorRegex = regexp.MustCompile(`<a href="(https?://[^"<>]*)"(?:[^<>"]|"[^"<>]*")*>([^<]*)</a>`)

// commentEntityRegex matches one HTML entity reference: a named entity, or a
// numeric character reference in decimal or hex form.
var commentEntityRegex = regexp.MustCompile(`&(?:#[0-9]+|#[xX][0-9a-fA-F]+|[a-zA-Z][a-zA-Z0-9]*);?`)

// commentEntities is the whitelist of entity references the normalizer will
// rewrite in anchor text. Every other reference, such as &nbsp; or a numeric
// escape, is left byte-identical.
var commentEntities = map[string]struct{}{
	"&amp;":  {},
	"&lt;":   {},
	"&gt;":   {},
	"&quot;": {},
	"&#39;":  {},
}

// MarkdownizeComment converts whitelisted HTML anchors in a comment into
// Markdown links. Any other input, including empty input, is passed through
// unchanged: this whitelists a construct, it does not sanitize HTML.
func MarkdownizeComment(s string) string {
	return commentAnchorRegex.ReplaceAllStringFunc(s, markdownizeAnchor)
}

func markdownizeAnchor(anchor string) string {
	m := commentAnchorRegex.FindStringSubmatch(anchor)
	if m == nil {
		return anchor
	}
	url, text := m[1], m[2]
	return "[" + unescapeCommentEntities(text) + "](" + url + ")"
}

// unescapeCommentEntities decodes the whitelisted entity references and leaves
// every other reference alone. html.UnescapeString is applied one reference at a
// time because over a whole string it also decodes references outside the
// whitelist, such as &nbsp; or numeric escapes.
func unescapeCommentEntities(text string) string {
	return commentEntityRegex.ReplaceAllStringFunc(text, func(ref string) string {
		if _, ok := commentEntities[ref]; !ok {
			return ref
		}
		return html.UnescapeString(ref)
	})
}
