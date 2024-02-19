package newsblurposter

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/seriousben/positronic-blogger/internal/github"
	"github.com/seriousben/positronic-blogger/internal/newsblur"
	"gotest.tools/v3/assert"
)

const (
	envNewsblurUsername = "POSITRONIC_TEST_NEWSBLUR_USERNAME"
	envNewsblurPassword = "POSITRONIC_TEST_NEWSBLUR_PASSWORD"
	envGithubRepo       = "POSITRONIC_TEST_GITHUB_REPO"
	envGithubToken      = "POSITRONIC_TEST_GITHUB_TOKEN"
)

func Test_Blogger(t *testing.T) {
	var (
		uid        = uuid.New().String()
		nbUsername = os.Getenv(envNewsblurUsername)
		nbPassword = os.Getenv(envNewsblurPassword)
		ghToken    = os.Getenv(envGithubToken)
		ghRepoFull = os.Getenv(envGithubRepo)
		ghOwner    string
		ghRepo     string
	)

	if nbUsername == "" || nbPassword == "" {
		t.Skipf("Skipping, missing %s or %s", envNewsblurUsername, envNewsblurPassword)
	}

	ctx := context.Background()
	nbClient, err := newsblur.New(ctx, nbUsername, nbPassword)
	assert.NilError(t, err)

	if ghToken == "" || ghRepoFull == "" {
		t.Skipf("Skipping, missing %s or %s", envGithubRepo, envGithubToken)
	}

	if ghRepoFullSplit := strings.Split(ghRepoFull, "/"); len(ghRepoFullSplit) == 2 {
		ghOwner = ghRepoFullSplit[0]
		ghRepo = ghRepoFullSplit[1]
	} else {
		t.Fatalf("malformed %s (%s) - Expected format to be owner/repo", envGithubRepo, ghRepoFull)
	}

	ghClient, err := github.New(ctx, ghToken, ghOwner, ghRepo)
	assert.NilError(t, err)

	contentPath := fmt.Sprintf("%s/%s", strings.ToLower(t.Name()), uid)

	bl, err := New(Config{
		GithubClient:              ghClient,
		NewsblurClient:            nbClient,
		NewsblurContentPath:       contentPath,
		NewsblurCheckpointPath:    fmt.Sprintf("%s/checkpoint", contentPath),
		InitialNewsblurCheckpoint: time.Now().Add(-1 * 3 * 30 * 24 * time.Hour),
		SkipMerge:                 true,
		GithubPrefix:              fmt.Sprintf("%s-", uid),
	})
	assert.NilError(t, err)

	err = bl.Run(ctx)
	assert.NilError(t, err)
}
