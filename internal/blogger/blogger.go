package blogger

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/github"
	"github.com/gosimple/slug"
	"github.com/seriousben/newsblur-to-hugo/internal/github"
	"github.com/seriousben/newsblur-to-hugo/internal/newsblur"
)

func Blog() {
	// 1. Sync newsblur shared stories
	// 2. Other things: Twitter, github, other apps
}

const blogRepositoryUser = "seriousben"
const blogRepository = "seriousben.com"
const blogPostPath = "content/links/"


var (
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
	URL     string
	Comment string
	Date    time.Time
}

type Checkpoint struct {
	SHA        string
	Checkpoint *time.Time
}

func NewBlogPost(story newsblur.Story) (BlogPost, error) {
	date, err := time.Parse(newsblur.NewsblurTimeFormat, story.SharedDate)
	if err != nil {
		return BlogPost{}, fmt.Errorf("error parsing date of story: %w", err)
	}

	return BlogPost{
		Title:   story.Title,
		URL:     story.Permalink,
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
	fileContent, _, _, err := githubClient.Repositories.GetContents(ctx, blogRepositoryUser, blogRepository, blogPostPath+"checkpoint", nil)
	if err != nil {
		return nil, fmt.Errorf("error getting checkpoint: %w", err)
	}

	content, err := fileContent.GetContent()
	if err != nil {
		return nil, fmt.Errorf("error decoding file content: %w", err)
	}
	if content == "" {
		log.Println("checkpoint was empty, full resync will happen")
		return &Checkpoint{
			SHA:        fileContent.GetSHA(),
			Checkpoint: nil,
		}, nil
	}

	content = strings.Trim(content, "\r\n")

	date, err := time.Parse(newsblur.NewsblurTimeFormat, content)
	if err != nil {
		return nil, fmt.Errorf("error parsing checkpoint: %w", err)
	}
	return &Checkpoint{
		SHA:        fileContent.GetSHA(),
		Checkpoint: &date,
	}, nil
}

func SetCheckpoint(ctx context.Context, githubClient *github.Client, prevCheckpoint *Checkpoint, checkpoint time.Time) error {
	dateStr := checkpoint.Format(newsblur.NewsblurTimeFormat)

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
		_, resp, err := githubClient.Repositories.UpdateFile(ctx, blogRepositoryUser, blogRepository, blogPostPath+"checkpoint", &opts)

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
		_, resp, err := githubClient.Repositories.CreateFile(context.TODO(), blogRepositoryUser, blogRepository, blogPostPath+fileName, &opts)

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

func syncSharedStoriesWithPosts(ctx context.Context, githubClient *github.Client, newsblurAPI *newsblur.Client, dryRun bool) int {
	checkpoint, err := GetCheckpoint(ctx, githubClient)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Got checkpoint %s", checkpoint)

	ch := newsblurAPI.IterSharedStories(ctx, checkpoint.Checkpoint)

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
		if dryRun {
			log.Printf("[DRY-RUN] Saved new checkpoint %s -> %s", checkpoint, newCheckpoint)
		} else {
			err = SetCheckpoint(ctx, githubClient, checkpoint, *newCheckpoint)
			if err != nil {
				log.Fatal("Could not save new checkpoint", err)
				return 0
			}
			log.Printf("Saved new checkpoint %s -> %s", checkpoint, newCheckpoint)
		}
	}

	return storiesNum - 1
}
