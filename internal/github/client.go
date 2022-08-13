package github

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
)

const apiRequestRateLimit = 720 * time.Millisecond

var ErrFileNotFound = errors.New("file not found")

type Client struct {
	ghClient    *github.Client
	apiTicker   *time.Ticker
	owner, repo string
}

func New(ctx context.Context, githubToken, owner, repo string) (*Client, error) {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: githubToken},
	)
	tc := oauth2.NewClient(ctx, ts)
	c := github.NewClient(tc)

	return &Client{
		ghClient:  c,
		apiTicker: time.NewTicker(apiRequestRateLimit),
		owner:     owner,
		repo:      repo,
	}, nil
}

func (c *Client) GetContent(ctx context.Context, path string) (content string, sha string, err error) {
	mainRef, _, err := c.ghClient.Git.GetRef(ctx, c.owner, c.repo, "refs/heads/main")
	if err != nil {
		return "", "", err
	}

	tr, _, err := c.ghClient.Git.GetTree(ctx, c.owner, c.repo, *mainRef.Object.SHA, false)
	if err != nil {
		return "", "", err
	}

	for _, p := range strings.Split(path, string(filepath.Separator)) {
	process_entries:
		for _, te := range tr.Entries {
			if *te.Path == p {
				if *te.Type == "tree" {
					<-c.apiTicker.C
					tr, _, err = c.ghClient.Git.GetTree(ctx, c.owner, c.repo, *te.SHA, false)
					if err != nil {
						return "", "", err
					}
					goto process_entries
				}
				<-c.apiTicker.C
				bl, _, err := c.ghClient.Git.GetBlob(ctx, c.owner, c.repo, *te.SHA)
				if err != nil {
					return "", "", err
				}
				b, err := base64.StdEncoding.DecodeString(*bl.Content)
				if err != nil {
					return "", "", err
				}
				return string(b), *bl.SHA, nil
			}
		}
	}

	return "", "", fmt.Errorf("file not found (%s): %w", path, ErrFileNotFound)
}

type BranchClient struct {
	client     *Client
	branchName string
	branchRef  string
	baseRef    string
}

// https://git-scm.com/book/en/v2
// https://gist.github.com/ursulacj/36ade01fa6bd5011ea31f3f6b572834e
// https://stackoverflow.com/questions/53260051/github-new-branch-creation-and-pull-request-using-rest-api
func (c *Client) StartBranch(ctx context.Context, branchName string) (*BranchClient, error) {
	<-c.apiTicker.C

	ref := fmt.Sprintf("refs/heads/%s", branchName)

	mainRef, _, err := c.ghClient.Git.GetRef(ctx, c.owner, c.repo, "refs/heads/main")
	if err != nil {
		return nil, err
	}

	_, _, err = c.ghClient.Git.CreateRef(ctx, c.owner, c.repo, &github.Reference{
		Ref:    github.String(ref),
		Object: mainRef.Object,
	})
	if err != nil {
		return nil, err
	}

	return &BranchClient{
		client:     c,
		branchName: branchName,
		branchRef:  ref,
		baseRef:    *mainRef.Ref,
	}, nil
}

func (c *BranchClient) CreateFile(ctx context.Context, commitMsg, path, content string) error {
	<-c.client.apiTicker.C

	// TODO: Manage it as a tree!
	// https://stackoverflow.com/questions/11801983/how-to-create-a-commit-and-push-into-repo-with-github-api-v3

	var opts = github.RepositoryContentFileOptions{
		Branch:    &c.branchName,
		Message:   &commitMsg,
		Content:   []byte(content),
		Committer: &github.CommitAuthor{Name: github.String("Benjamin Boudreau"), Email: github.String("boudreau.benjamin@gmail.com")},
	}
	_, resp, err := c.client.ghClient.Repositories.CreateFile(ctx, c.client.owner, c.client.repo, path, &opts)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusConflict {
			log.Println("conflict on create path", path, err)
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			return c.CreateFile(ctx, commitMsg, path, content)
		}
		return err
	}

	return nil
}

func (c *BranchClient) UpdateFile(ctx context.Context, commitMsg, path, sha, content string) error {
	// TODO: Manage it as a tree!
	// https://stackoverflow.com/questions/11801983/how-to-create-a-commit-and-push-into-repo-with-github-api-v3

	<-c.client.apiTicker.C

	var opts = github.RepositoryContentFileOptions{
		Branch:    &c.branchName,
		Message:   &commitMsg,
		Content:   []byte(content),
		Committer: &github.CommitAuthor{Name: github.String("Benjamin Boudreau"), Email: github.String("boudreau.benjamin@gmail.com")},
		SHA:       &sha,
	}
	_, resp, err := c.client.ghClient.Repositories.CreateFile(ctx, c.client.owner, c.client.repo, path, &opts)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusConflict {
			log.Println("conflict on update path", path, err, sha)
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			return c.UpdateFile(ctx, commitMsg, path, sha, content)
		}
		return err
	}
	return nil
}

func (c *BranchClient) PullRequest(ctx context.Context, title, body string) (*github.PullRequest, error) {
	<-c.client.apiTicker.C

	pr, _, err := c.client.ghClient.PullRequests.Create(ctx, c.client.owner, c.client.repo, &github.NewPullRequest{
		Title: &title,
		Body:  &body,
		Head:  &c.branchName,
		Base:  &c.baseRef,
	})
	if err != nil {
		return nil, err
	}
	return pr, nil
}

func (c *BranchClient) WaitAndMerge(ctx context.Context, pr *github.PullRequest) error {
	var (
		i   = 0
		err error
	)
	for pr.Mergeable == nil || !*pr.Mergeable {
		i++
		log.Println("PR not mergeable")
		time.Sleep(time.Duration(i) * time.Second)
		pr, _, err = c.client.ghClient.PullRequests.Get(ctx, c.client.owner, c.client.repo, *pr.Number)
		if err != nil {
			return err
		}
	}

	<-c.client.apiTicker.C

	_, _, err = c.client.ghClient.PullRequests.Merge(ctx, c.client.owner, c.client.repo, *pr.Number, "", nil)
	if err != nil {
		return err
	}

	return c.DeleteBranch(ctx)
}

func (c *BranchClient) DeleteBranch(ctx context.Context) error {
	<-c.client.apiTicker.C
	_, err := c.client.ghClient.Git.DeleteRef(ctx, c.client.owner, c.client.repo, c.branchRef)
	if err != nil {
		return err
	}
	return nil
}
