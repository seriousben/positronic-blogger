package newsblurposter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/seriousben/positronic-blogger/internal/github"
	"github.com/seriousben/positronic-blogger/internal/newsblur"
	"github.com/seriousben/positronic-blogger/internal/template"
)

var (
	newsblurDupLinkRegex = regexp.MustCompile(`<a href="(.*)">.*</a>`)
)

func newsblurStoryToBlogPost(story *newsblur.Story) (template.Post, error) {
	comment := newsblurDupLinkRegex.ReplaceAllString(
		story.Comment,
		"$1",
	)
	return template.Post{
		Title:   story.Title,
		URL:     story.Permalink,
		Comment: comment,
		Date:    story.SharedDate,
	}, nil
}

type Config struct {
	GithubClient              *github.Client
	NewsblurClient            *newsblur.Client
	NewsblurContentPath       string
	NewsblurCheckpointPath    string
	InitialNewsblurCheckpoint time.Time
	SkipMerge                 bool
	GithubPrefix              string
}

type Poster struct {
	Config
}

func New(cfg Config) (*Poster, error) {
	return &Poster{
		Config: cfg,
	}, nil
}

func (b *Poster) Run(ctx context.Context) error {
	checkpoint, checkpointSHA, err := b.getCheckpoint(ctx)
	if err != nil {
		return err
	}

	if checkpoint.Before(b.InitialNewsblurCheckpoint) {
		checkpoint = b.InitialNewsblurCheckpoint
	}

	it, err := b.NewsblurClient.SharedStoriesIterator(ctx, checkpoint)
	if err != nil {
		return err
	}

	var brc *github.BranchClient
	lastCheckpointAt := checkpoint

	for {
		st, err := it.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		post, err := newsblurStoryToBlogPost(st)
		if err != nil {
			return err
		}

		// Safety check to make sure posts returned from
		// content providers are newer than passed in checkpoint.
		if post.Date.After(lastCheckpointAt) {
			lastCheckpointAt = post.Date
		}

		// start branch on first new content.
		if brc == nil {
			brc, err = b.GithubClient.StartBranch(ctx, fmt.Sprintf("%s%s-positronic-blogger", b.GithubPrefix, checkpoint.Format("2006-01-02T1504")))
			if err != nil {
				return err
			}
		}
		fileName := post.FileName()
		commit := fmt.Sprintf("auto: new short post %s [skip ci]", fileName)

		buf, err := post.ToMarkdown()
		if err != nil {
			return err
		}

		err = brc.CreateFile(ctx, commit, path.Join(b.NewsblurContentPath, fileName), buf.String())
		if err != nil {
			return err
		}
	}

	if brc != nil {
		if err = b.setCheckpoint(ctx, brc, lastCheckpointAt, checkpointSHA); err != nil {
			return err
		}
		pr, err := brc.PullRequest(
			ctx,
			fmt.Sprintf("%s%s-positronic-blogger", b.GithubPrefix, checkpoint.Format(time.RFC3339)),
			"Auto blogging done from https://github.com/seriousben/positronic-blogger",
		)
		if err != nil {
			return err
		}
		if !b.Config.SkipMerge {
			err = brc.WaitAndMerge(ctx, pr)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *Poster) getCheckpoint(ctx context.Context) (time.Time, string, error) {
	checkpointStr, checkpointSHA, err := b.GithubClient.GetContent(ctx, b.NewsblurCheckpointPath)
	if err != nil && !errors.Is(err, github.ErrFileNotFound) {
		return time.Time{}, "", err
	}

	if checkpointStr == "" {
		return time.Time{}, "", nil
	}

	var checkpoint time.Time
	err = json.Unmarshal([]byte(checkpointStr), &checkpoint)
	if err != nil {
		log.Printf("checkpoint is not JSON: %v", err)
	} else {
		return checkpoint, checkpointSHA, nil
	}

	// fallback on legacy newsblur time format.
	checkpoint, err = time.Parse("2006-01-02 15:04:05.999999", strings.Trim(checkpointStr, "\r\n"))
	if err != nil {
		return time.Time{}, "", fmt.Errorf("error parsing checkpoint: %w", err)
	}

	return checkpoint, checkpointSHA, nil
}

func (b *Poster) setCheckpoint(ctx context.Context, gh *github.BranchClient, checkpoint time.Time, checkpointSHA string) error {
	checkpointJSON, err := json.Marshal(checkpoint)
	if err != nil {
		return err
	}

	commit := "auto: checkpoint"

	if checkpointSHA == "" {
		err = gh.CreateFile(ctx, commit, b.NewsblurCheckpointPath, string(checkpointJSON))
		if err != nil {
			return err
		}
		return nil
	}

	err = gh.UpdateFile(ctx, commit, b.NewsblurCheckpointPath, checkpointSHA, string(checkpointJSON))
	if err != nil {
		return err
	}

	return nil
}
