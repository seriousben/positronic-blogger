package cmd

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/google/go-github/github"
	"github.com/gosimple/slug"
	"github.com/seriousben/newsblur-to-hugo/internal/newsblur"
	"golang.org/x/oauth2"
)

const BLOG_REPOSITORY_USER = "seriousben"
const BLOG_REPOSITORY = "seriousben.com"
const BLOG_POST_PATH = "content/tldr/"

const API_REQUEST_RATE = 720 * time.Millisecond

var (
	apiTicker    = time.NewTicker(API_REQUEST_RATE)
	postTemplate = `
+++
date = "{{.Date}}"
publishDate = "{{.Date}}"
title = {{.Title | quote}}
originalUrl = {{.Url | quote}}
comment = {{.Comment | quote}}
+++

### Comment

{{.Comment}}

[Read more]({{.Url}})
`
)

type BlogPost struct {
	Title   string
	Url     string
	Comment string
	Date    time.Time
}

type Checkpoint struct {
	SHA        string
	Checkpoint *time.Time
}

func NewBlogPost(story newsblur.Story) (BlogPost, error) {
	date, err := time.Parse(newsblur.NEWSBLUR_TIME_FORMAT, story.SharedDate)
	if err != nil {
		return BlogPost{}, fmt.Errorf("error parsing date of story: %w", err)
	}

	return BlogPost{
		Title:   story.Title,
		Url:     story.Permalink,
		Comment: story.Comment,
		Date:    date,
	}, nil
}

func ToStringPtr(str string) *string {
	return &str
}

func retryFunc(theFunc func() (bool, error)) error {
	retry, err := theFunc()
	if retry {
		_, err = theFunc()
		return err
	}
	return err
}

func GetCheckpoint(ctx context.Context, githubClient *github.Client) (*Checkpoint, error) {
	<-apiTicker.C
	fileContent, _, _, err := githubClient.Repositories.GetContents(ctx, BLOG_REPOSITORY_USER, BLOG_REPOSITORY, BLOG_POST_PATH+"checkpoint", nil)
	if err != nil {
		return nil, fmt.Errorf("error getting checkpoint: %w", err)
	}

	content, err := fileContent.GetContent()
	if err != nil {
		return nil, fmt.Errorf("error decoding file content: %w", err)
	}
	if content == "" {
		log.Println("Checkpoint was empty, full resync will happen")
		return &Checkpoint{
			SHA:        fileContent.GetSHA(),
			Checkpoint: nil,
		}, nil
	}

	content = strings.Trim(content, "\r\n")

	date, err := time.Parse(newsblur.NEWSBLUR_TIME_FORMAT, content)
	if err != nil {
		return nil, fmt.Errorf("error parsing checkpoint: %w", err)
	}
	return &Checkpoint{
		SHA:        fileContent.GetSHA(),
		Checkpoint: &date,
	}, nil
}

func SetCheckpoint(ctx context.Context, githubClient *github.Client, prevCheckpoint *Checkpoint, checkpoint time.Time) error {
	dateStr := checkpoint.Format(newsblur.NEWSBLUR_TIME_FORMAT)

	commit := "auto: set checkpoint and deploy"
	var opts = github.RepositoryContentFileOptions{
		Message:   &commit,
		Content:   []byte(dateStr),
		SHA:       &prevCheckpoint.SHA,
		Committer: &github.CommitAuthor{Name: ToStringPtr("Benjamin Boudreau"), Email: ToStringPtr("boudreau.benjamin@gmail.com")},
	}

	// Workaround for 409 status code by github
	apiCall := func() (bool, error) {
		<-apiTicker.C
		_, resp, err := githubClient.Repositories.UpdateFile(ctx, BLOG_REPOSITORY_USER, BLOG_REPOSITORY, BLOG_POST_PATH+"checkpoint", &opts)

		if resp.StatusCode == 409 {
			return true, nil
		}

		return false, err
	}

	err := retryFunc(apiCall)

	if err != nil {
		return fmt.Errorf("updating checkpoint file in github: %w", err)
	}

	return nil
}

var tmpl = template.Must(template.New("short").Funcs(template.FuncMap{
	"quote": strconv.Quote,
}).Parse(postTemplate))

func CreateBlogPost(githubClient *github.Client, post BlogPost, dryRun bool) error {
	fileName := slug.Make(post.Title) + ".md"
	commit := "auto: new short post " + fileName + " [skip ci]"

	buf := new(bytes.Buffer)
	err := tmpl.Execute(buf, post)
	if err != nil {
		return fmt.Errorf("executing template: %w", err)
	}

	if dryRun {
		log.Printf("[DRY-RUN] Post content: %s\n", buf.String())
		return nil
	}

	var opts = github.RepositoryContentFileOptions{
		Message:   &commit,
		Content:   buf.Bytes(),
		Committer: &github.CommitAuthor{Name: ToStringPtr("Benjamin Boudreau"), Email: ToStringPtr("boudreau.benjamin@gmail.com")},
	}

	// Workaround for 409 status code by github
	apiCall := func() (bool, error) {
		<-apiTicker.C
		_, resp, err := githubClient.Repositories.CreateFile(context.TODO(), BLOG_REPOSITORY_USER, BLOG_REPOSITORY, BLOG_POST_PATH+fileName, &opts)

		if resp.StatusCode == 409 {
			return true, nil
		}

		return false, err
	}

	err = retryFunc(apiCall)

	if err != nil {
		return fmt.Errorf("creating blog post file in github: %w", err)
	}

	log.Printf("Created %s (%s)", commit, post.Date)
	return nil
}
func NewGithubClient(ctx context.Context, githubToken string) (*github.Client, error) {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: githubToken},
	)
	tc := oauth2.NewClient(ctx, ts)

	return github.NewClient(tc), nil
}

func SyncSharedStoriesWithPosts(ctx context.Context, githubClient *github.Client, newsblurAPI *newsblur.Client, dryRun bool) int {
	checkpoint, err := GetCheckpoint(ctx, githubClient)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Got checkpoint %s", checkpoint)

	ch := newsblurAPI.IterStories(ctx, checkpoint.Checkpoint)

	storiesNum := 1
	var newCheckpoint *time.Time
	for story := range ch {
		log.Printf("Creating Story[%d]: %s %+v\n", storiesNum, story.ID, &story)
		blogPost, err := NewBlogPost(story)
		if err != nil {
			log.Println("Error creating blog post", err)
			break
		}

		err = CreateBlogPost(githubClient, blogPost, dryRun)
		if err != nil {
			log.Println("Error posting blog post", err)
			break
		}
		log.Printf("Created Story[%d] successfully", storiesNum)
		storiesNum += 1

		if newCheckpoint == nil {
			newCheckpoint = &blogPost.Date
		}
	}

	if newCheckpoint != nil && (checkpoint.Checkpoint == nil || !checkpoint.Checkpoint.Equal(*newCheckpoint)) {
		if !dryRun {
			err = SetCheckpoint(ctx, githubClient, checkpoint, *newCheckpoint)
			if err != nil {
				log.Fatal("Could not save new checkpoint", err)
			}
			log.Printf("Saved new checkpoint %s -> %s", checkpoint, newCheckpoint)
		} else {
			log.Printf("[DRY-RUN] Saved new checkpoint %s -> %s", checkpoint, newCheckpoint)
		}
	}

	return storiesNum - 1
}
