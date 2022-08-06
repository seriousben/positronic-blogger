package github

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
)

const apiRequestRateLimit = 720 * time.Millisecond

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

func (c *BranchClient) CreateFile(ctx context.Context, commitMsg, path, content string) (*BranchClient, error) {
	<-c.client.apiTicker.C

	// TODO: Manage it as a tree!
	// https://stackoverflow.com/questions/11801983/how-to-create-a-commit-and-push-into-repo-with-github-api-v3

	var opts = github.RepositoryContentFileOptions{
		Branch:    github.String(c.branchName),
		Message:   &commitMsg,
		Content:   []byte(content),
		Committer: &github.CommitAuthor{Name: github.String("Benjamin Boudreau"), Email: github.String("boudreau.benjamin@gmail.com")},
	}
	_, _, err := c.client.ghClient.Repositories.CreateFile(context.TODO(), c.client.owner, c.client.repo, path, &opts)
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (c *BranchClient) PullRequestAndMerge(ctx context.Context, title, body string) (*Client, error) {
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

	i := 0
	for pr.Mergeable == nil || !*pr.Mergeable {
		i++
		log.Println("PR not mergeable")
		time.Sleep(time.Duration(i) * time.Second)
		pr, _, err = c.client.ghClient.PullRequests.Get(ctx, c.client.owner, c.client.repo, *pr.Number)
		if err != nil {
			return nil, err
		}
	}

	<-c.client.apiTicker.C

	_, _, err = c.client.ghClient.PullRequests.Merge(ctx, c.client.owner, c.client.repo, *pr.Number, "", nil)
	if err != nil {
		return nil, err
	}

	return c.DeleteBranch(ctx)
}

func (c *BranchClient) DeleteBranch(ctx context.Context) (*Client, error) {
	<-c.client.apiTicker.C
	_, err := c.client.ghClient.Git.DeleteRef(ctx, c.client.owner, c.client.repo, c.branchRef)
	if err != nil {
		return nil, err
	}
	return c.client, nil
}
