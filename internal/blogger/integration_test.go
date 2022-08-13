package blogger

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/seriousben/newsblur-to-hugo/internal/github"
	"github.com/seriousben/newsblur-to-hugo/internal/newsblur"
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

	bl, err := New(Config{
		GithubClient:           ghClient,
		NewsblurClient:         nbClient,
		NewsblurContentPath:    "content/links",
		NewsblurCheckpointPath: "content/links/checkpoint",
	})
	assert.NilError(t, err)

	err = bl.Run(ctx)
	assert.NilError(t, err)
}
