package github

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

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
		_ = br.DeleteBranch(ctx)
	}()

	file1Path := fmt.Sprintf("%s/%s", branchName, "file1.md")
	file1Content := fmt.Sprintf(`
	# File1
	
	Content of file1

	%s`, uid)
	err = br.CreateFile(ctx, "adding first file", file1Path, file1Content)
	assert.NilError(t, err)

	err = br.CreateFile(ctx, "adding second file", fmt.Sprintf("%s/%s", branchName, "file2.md"), `
# File2

Content of file2
	`)
	assert.NilError(t, err)

	//t.Logf("manual verification time: %s", branchName)
	//time.Sleep(30 * time.Second)

	pr, err := br.PullRequest(ctx, branchName, "Integration testing")
	assert.NilError(t, err)

	err = br.WaitAndMerge(ctx, pr)
	assert.NilError(t, err)

	contentCtx, cancelFunc := context.WithTimeout(ctx, 5*time.Second)
	defer cancelFunc()

	content, _, err := cl.GetContent(contentCtx, file1Path)
	assert.NilError(t, err)
	assert.Equal(t, content, file1Content)

	_, _, err = cl.GetContent(contentCtx, "file-not-found")
	assert.ErrorIs(t, err, ErrFileNotFound)
}
