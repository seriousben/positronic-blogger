package github

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gotest.tools/v3/assert"
)

const (
	envGithubRepo  = "POSITRONIC_TEST_GITHUB_REPO"
	envGithubToken = "POSITRONIC_TEST_GITHUB_TOKEN"
)

func Test_Client(t *testing.T) {
	var (
		uid        = uuid.New().String()
		ghToken    = os.Getenv(envGithubToken)
		ghRepoFull = os.Getenv(envGithubRepo)
		ghOwner    string
		ghRepo     string
	)

	if ghToken == "" || ghRepoFull == "" {
		t.Skipf("Skipping, missing %s or %s", envGithubRepo, envGithubToken)
	}

	if ghRepoFullSplit := strings.Split(ghRepoFull, "/"); len(ghRepoFullSplit) == 2 {
		ghOwner = ghRepoFullSplit[0]
		ghRepo = ghRepoFullSplit[1]
	} else {
		t.Fatalf("malformed %s (%s) - Expected format to be owner/repo", envGithubRepo, ghRepoFull)
	}

	ctx := context.Background()
	cl, err := New(ctx, ghToken, ghOwner, ghRepo)
	assert.NilError(t, err)

	branchName := fmt.Sprintf("%s/%s", strings.ToLower(t.Name()), uid)
	br, err := cl.StartBranch(ctx, branchName)
	assert.NilError(t, err)
	defer func() {
		_, _ = br.DeleteBranch(ctx)
	}()

	_, err = br.CreateFile(ctx, "adding first file", fmt.Sprintf("%s/%s", branchName, "file1.md"), `
# File1

Content of file1
	`)
	assert.NilError(t, err)

	//t.Log("manual verification time")
	//time.Sleep(30 * time.Second)

	_, err = br.PullRequestAndMerge(ctx, branchName, "Integration testing")
	assert.NilError(t, err)
}
