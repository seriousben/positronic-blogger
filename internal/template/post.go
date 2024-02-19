package template

import (
	"bytes"
	"fmt"
	"strconv"
	"text/template"
	"time"

	"github.com/gosimple/slug"
)

const (
	postTimeFormat = "2006-01-02"
)

var (
	postTemplate = `+++
date = "{{ .Date | timeFormat }}"
publishDate = "{{ .Date | timeFormat }}"
title = {{ .Title | quote }}
originalUrl = "{{.URL}}"
comment = {{.Comment | quote}}
+++

### My thoughts

{{.Comment}}

Read the article: [{{.Title}}]({{.URL}})
`
	tmpl = template.Must(template.New("short").Funcs(template.FuncMap{
		"quote": strconv.Quote,
		"timeFormat": func(t time.Time) string {
			return t.Format(time.RFC3339Nano)
		},
	}).Parse(postTemplate))
)

type Post struct {
	Title   string
	URL     string
	Comment string
	Date    time.Time
}

func (p Post) ToMarkdown() (*bytes.Buffer, error) {
	buf := new(bytes.Buffer)
	if err := tmpl.Execute(buf, p); err != nil {
		return nil, fmt.Errorf("executing template: %w", err)
	}
	return buf, nil
}

func (p Post) FileName() string {
	return fmt.Sprintf("%s-%s.md", p.Date.Format(postTimeFormat), slug.Make(p.Title))
}
