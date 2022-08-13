package main

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/seriousben/positronic-blogger/internal/blogger"
	"github.com/seriousben/positronic-blogger/internal/github"
	"github.com/seriousben/positronic-blogger/internal/newsblur"
)

const (
	envSkipMerge              = "POSITRONIC_SKIP_MERGE"
	envNewsblurUsername       = "POSITRONIC_NEWSBLUR_USERNAME"
	envNewsblurPassword       = "POSITRONIC_NEWSBLUR_PASSWORD"
	envNewsblurContentPath    = "POSITRONIC_NEWSBLUR_CONTENT_PATH"
	envNewsblurCheckpointPath = "POSITRONIC_NEWSBLUR_CHECKPOINT_PATH"
	envGithubRepo             = "POSITRONIC_GITHUB_REPO"
	envGithubToken            = "POSITRONIC_GITHUB_TOKEN"
)

func main() {
	var (
		ctx              = context.Background()
		skipMerge        = os.Getenv(envSkipMerge) == "true"
		nbUsername       = os.Getenv(envNewsblurUsername)
		nbPassword       = os.Getenv(envNewsblurPassword)
		nbContentPath    = os.Getenv(envNewsblurContentPath)
		nbCheckpointPath = os.Getenv(envNewsblurCheckpointPath)
		ghToken          = os.Getenv(envGithubToken)
		ghRepoFull       = os.Getenv(envGithubRepo)
		ghOwner          string
		ghRepo           string
	)

	if nbUsername == "" || nbPassword == "" {
		log.Fatalf("missing %s or %s environment variables", envNewsblurUsername, envNewsblurPassword)
	}

	nbClient, err := newsblur.New(ctx, nbUsername, nbPassword)
	if err != nil {
		log.Fatalf("error creating newsblur client: %v", err)
	}

	if ghToken == "" || ghRepoFull == "" {
		log.Fatalf("missing %s or %s", envGithubRepo, envGithubToken)
	}

	if ghRepoFullSplit := strings.Split(ghRepoFull, "/"); len(ghRepoFullSplit) == 2 {
		ghOwner = ghRepoFullSplit[0]
		ghRepo = ghRepoFullSplit[1]
	} else {
		log.Fatalf("malformed %s (%s) - expected format to be owner/repo", envGithubRepo, ghRepoFull)
	}

	if nbContentPath == "" || nbCheckpointPath == "" {
		log.Fatalf("missing %s or %s", envNewsblurContentPath, envNewsblurCheckpointPath)
	}

	ghClient, err := github.New(ctx, ghToken, ghOwner, ghRepo)
	if err != nil {
		log.Fatalf("error creating github client: %v", err)
	}

	bl, err := blogger.New(blogger.Config{
		GithubClient:           ghClient,
		NewsblurClient:         nbClient,
		NewsblurContentPath:    nbContentPath,
		NewsblurCheckpointPath: nbCheckpointPath,
		SkipMerge:              skipMerge,
	})
	if err != nil {
		log.Fatalf("error creating blogger: %v", err)
	}

	err = bl.Run(ctx)
	if err != nil {
		log.Fatalf("error running blogger: %v", err)
	}
}
