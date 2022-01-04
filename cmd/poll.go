package cmd

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/seriousben/newsblur-to-hugo/internal/newsblur"
	"github.com/urfave/cli/v2"
)

func init() {
	Register(&cli.Command{
		Name:  "poll",
		Usage: "Synchronization poller that periodically synchronizes new shared posts.",
		Flags: []cli.Flag{
			&cli.DurationFlag{
				Name:    "frequency",
				Usage:   "Poll frequency",
				EnvVars: []string{"POLL_FREQUENCY"},
				Value:   2 * time.Hour,
			},
			&cli.BoolFlag{
				Name:    "dry-run",
				Usage:   "Dry run",
				EnvVars: []string{"DRY_RUN"},
			},
			&cli.StringFlag{
				Name:     "github-token",
				Usage:    "GitHub token to read and write to hugo blog repository (Required)",
				EnvVars:  []string{"GITHUB_TOKEN"},
				Required: true,
			},
			&cli.StringFlag{
				Name:     "newsblur-username",
				Usage:    "Newsblur username (Required)",
				EnvVars:  []string{"NEWSBLUR_USERNAME"},
				Required: true,
			},
			&cli.StringFlag{
				Name:        "newsblur-password",
				Usage:       "Newsblur password (Required)",
				EnvVars:     []string{"NEWSBLUR_PASSWORD"},
				DefaultText: "***",
				Required:    true,
			},
		},
		Action: func(c *cli.Context) error {
			a := pollArgs{
				githubToken:      c.String("github-token"),
				newsblurUsername: c.String("newsblur-username"),
				newsblurPassword: c.String("newsblur-password"),
				frequency:        c.Duration("frequency"),
				dryRun:           c.Bool("dry-run"),
			}
			return doPollAction(c, a)
		},
	})
}

type pollArgs struct {
	githubToken      string
	newsblurUsername string
	newsblurPassword string
	frequency        time.Duration
	dryRun           bool
}

func doPollAction(ctx context.Context, args pollArgs) error {
	githubClient, err := NewGithubClient(ctx, args.githubToken)
	if err != nil {
		return fmt.Errorf("creating GitHub client: %w", err)
	}

	newsblurClient, err := newsblur.NewClient()
	if err != nil {
		return fmt.Errorf("creating newsblur client: %w", err)
	}

	_, err = newsblurClient.Login(ctx, args.newsblurUsername, args.newsblurPassword)
	if err != nil {
		return fmt.Errorf("error on login: %w", err)
	}

pollLoop:
	for {
		log.Printf("Syncing")
		num := SyncSharedStoriesWithPosts(ctx, githubClient, newsblurClient, args.dryRun)

		log.Printf("Posted %d stories", num)

		select {
		case <-time.After(2 * time.Hour):
		case <-ctx.Done():
			break pollLoop
		}
	}

	return nil
}
