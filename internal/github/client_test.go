package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/github"
)

// fakeGitHub records every request it receives and delegates the response to a
// per-test handler, so tests can assert on the outgoing API calls and on the
// behavior of the client against scripted responses.
type fakeGitHub struct {
	handler  func(w http.ResponseWriter, r *http.Request)
	requests []recordedRequest
}

type recordedRequest struct {
	method   string
	path     string
	query    url.Values
	rawQuery string
	body     map[string]any
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	record := recordedRequest{
		method:   r.Method,
		path:     r.URL.Path,
		query:    r.URL.Query(),
		rawQuery: r.URL.RawQuery,
	}
	if b, err := io.ReadAll(r.Body); err == nil && len(b) > 0 {
		_ = json.Unmarshal(b, &record.body)
	}
	f.requests = append(f.requests, record)
	f.handler(w, r)
}

func (f *fakeGitHub) count(method, path string) int {
	n := 0
	for _, r := range f.requests {
		if r.method == method && r.path == path {
			n++
		}
	}
	return n
}

func (f *fakeGitHub) find(method, path string) *recordedRequest {
	for i := range f.requests {
		if f.requests[i].method == method && f.requests[i].path == path {
			return &f.requests[i]
		}
	}
	return nil
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("failed to write fake response: %v", err)
	}
}

// newFakeClient returns a client wired to a local server instead of the GitHub
// API. It is built by hand because New only talks to the real API. Set the
// returned handler before issuing calls.
func newFakeClient(t *testing.T) (*Client, *fakeGitHub) {
	t.Helper()

	fake := &fakeGitHub{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	baseURL, err := url.Parse(srv.URL + "/")
	if err != nil {
		t.Fatalf("failed to parse test server URL: %v", err)
	}

	ghClient := github.NewClient(srv.Client())
	ghClient.BaseURL = baseURL

	ticker := time.NewTicker(time.Millisecond)
	t.Cleanup(ticker.Stop)

	return &Client{
		ghClient:  ghClient,
		apiTicker: ticker,
		owner:     "test-owner",
		repo:      "test-repo",
	}, fake
}

func mainRefResponse(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	writeJSON(t, w, http.StatusOK, map[string]any{
		"ref":    "refs/heads/main",
		"object": map[string]any{"sha": "main-sha", "type": "commit"},
	})
}

// Test_PullRequest_QualifiesHeadFilter: the API expects owner:branch; it
// ignores an unqualified name and returns every open PR.
func Test_PullRequest_QualifiesHeadFilter(t *testing.T) {
	ctx := context.Background()

	c, fake := newFakeClient(t)
	fake.handler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/test-owner/test-repo/pulls":
			// Only an unrelated PR is open: the client must not reuse it.
			writeJSON(t, w, http.StatusOK, []any{
				map[string]any{
					"number": 1,
					"head":   map[string]any{"ref": "unrelated-branch"},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/test-owner/test-repo/pulls":
			writeJSON(t, w, http.StatusCreated, map[string]any{
				"number": 42,
				"head":   map[string]any{"ref": "test-branch"},
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}

	brc := &BranchClient{client: c, branchName: "test-branch", branchRef: "refs/heads/test-branch", baseRef: "main"}

	pr, err := brc.PullRequest(ctx, "title", "body")
	if err != nil {
		t.Fatalf("PullRequest returned error: %v", err)
	}

	listed := fake.find(http.MethodGet, "/repos/test-owner/test-repo/pulls")
	if listed == nil {
		t.Fatal("no PR list request recorded")
	}
	if got, want := listed.query.Get("head"), "test-owner:test-branch"; got != want {
		t.Errorf("head query = %q, want %q (raw query: %s)", got, want, listed.rawQuery)
	}
	// Pin the wire form too, since a decoded value could come from elsewhere.
	decoded, err := url.QueryUnescape(listed.rawQuery)
	if err != nil {
		t.Fatalf("failed to decode raw query %q: %v", listed.rawQuery, err)
	}
	if !strings.Contains(decoded, "head=test-owner:test-branch") {
		t.Errorf("raw query %q does not carry head=test-owner:test-branch", listed.rawQuery)
	}

	// The unrelated open PR must not be returned; a new PR is created instead.
	if got, want := *pr.Number, 42; got != want {
		t.Errorf("PR number = %d, want %d (unrelated PR reused)", got, want)
	}
	if n := fake.count(http.MethodPost, "/repos/test-owner/test-repo/pulls"); n != 1 {
		t.Errorf("PR create calls = %d, want 1", n)
	}
}

// Test_PullRequest_FiltersMixedList asserts an API surprise (a list containing
// PRs from other branches) never causes a wrong PR to be reused.
func Test_PullRequest_FiltersMixedList(t *testing.T) {
	ctx := context.Background()

	c, fake := newFakeClient(t)
	fake.handler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/test-owner/test-repo/pulls":
			writeJSON(t, w, http.StatusOK, []any{
				map[string]any{"number": 1, "head": map[string]any{"ref": "unrelated-branch"}},
				map[string]any{"number": 2, "head": map[string]any{"ref": "test-branch"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/test-owner/test-repo/pulls":
			t.Error("PR should have been reused, but a create was issued")
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}

	brc := &BranchClient{client: c, branchName: "test-branch", branchRef: "refs/heads/test-branch", baseRef: "main"}

	pr, err := brc.PullRequest(ctx, "title", "body")
	if err != nil {
		t.Fatalf("PullRequest returned error: %v", err)
	}

	if got, want := *pr.Number, 2; got != want {
		t.Errorf("PR number = %d, want %d", got, want)
	}
	if n := fake.count(http.MethodPost, "/repos/test-owner/test-repo/pulls"); n != 0 {
		t.Errorf("PR create calls = %d, want 0", n)
	}
}

// Test_StartBranch_RetriesCreateRefOn422 asserts the ref recreate retries past a
// "reference already exists" race with the ref delete of a just-closed PR.
func Test_StartBranch_RetriesCreateRefOn422(t *testing.T) {
	ctx := context.Background()

	c, fake := newFakeClient(t)
	fake.handler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/test-owner/test-repo/git/refs/heads/main":
			mainRefResponse(t, w)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/test-owner/test-repo/git/refs/heads/test-branch":
			// Stale branch: the ref exists but points elsewhere than main.
			writeJSON(t, w, http.StatusOK, map[string]any{
				"ref":    "refs/heads/test-branch",
				"object": map[string]any{"sha": "stale-sha", "type": "commit"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/test-owner/test-repo/pulls":
			writeJSON(t, w, http.StatusOK, []any{})
		case r.Method == http.MethodDelete && r.URL.Path == "/repos/test-owner/test-repo/git/refs/heads/test-branch":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/test-owner/test-repo/git/refs":
			// The create plus the first two retries all hit the race, so only the
			// third retry succeeds.
			if fake.count(http.MethodPost, "/repos/test-owner/test-repo/git/refs") <= 3 {
				writeJSON(t, w, http.StatusUnprocessableEntity, map[string]any{"message": "Reference already exists"})
				return
			}
			writeJSON(t, w, http.StatusCreated, map[string]any{
				"ref":    "refs/heads/test-branch",
				"object": map[string]any{"sha": "main-sha", "type": "commit"},
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}

	start := time.Now()
	brc, err := c.StartBranch(ctx, "test-branch")
	if err != nil {
		t.Fatalf("StartBranch returned error: %v", err)
	}
	if got, want := brc.branchName, "test-branch"; got != want {
		t.Errorf("branch name = %q, want %q", got, want)
	}

	if n := fake.count(http.MethodPost, "/repos/test-owner/test-repo/git/refs"); n != 4 {
		t.Errorf("CreateRef calls = %d, want 4 (create plus 3 retries)", n)
	}

	// Two retries back off (200ms then 400ms) before the third succeeds, so this
	// pins that a real wait happens between attempts.
	if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
		t.Errorf("retry took %v, want at least 300ms of backoff across two retries", elapsed)
	}
}

// Test_StartBranch_GivesUpOnPersistent422 asserts the retry is bounded: a ref
// that stays taken fails the call instead of looping forever.
func Test_StartBranch_GivesUpOnPersistent422(t *testing.T) {
	ctx := context.Background()

	c, fake := newFakeClient(t)
	fake.handler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/test-owner/test-repo/git/refs/heads/main":
			mainRefResponse(t, w)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/test-owner/test-repo/git/refs/heads/test-branch":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"ref":    "refs/heads/test-branch",
				"object": map[string]any{"sha": "stale-sha", "type": "commit"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/test-owner/test-repo/pulls":
			writeJSON(t, w, http.StatusOK, []any{})
		case r.Method == http.MethodDelete && r.URL.Path == "/repos/test-owner/test-repo/git/refs/heads/test-branch":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/test-owner/test-repo/git/refs":
			writeJSON(t, w, http.StatusUnprocessableEntity, map[string]any{"message": "Reference already exists"})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}

	if _, err := c.StartBranch(ctx, "test-branch"); err == nil {
		t.Fatal("StartBranch returned nil error, want a give-up error")
	}

	// One initial create plus maxCreateRefAttempts retries.
	if n := fake.count(http.MethodPost, "/repos/test-owner/test-repo/git/refs"); n != 1+maxCreateRefAttempts {
		t.Errorf("CreateRef calls = %d, want %d", n, 1+maxCreateRefAttempts)
	}
}

// Test_StartBranch_ClosesOnlyPRsForBranch asserts the stale-branch path closes
// only the PRs whose head ref matches the branch.
func Test_StartBranch_ClosesOnlyPRsForBranch(t *testing.T) {
	ctx := context.Background()

	c, fake := newFakeClient(t)
	fake.handler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/test-owner/test-repo/git/refs/heads/main":
			mainRefResponse(t, w)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/test-owner/test-repo/git/refs":
			if fake.count(http.MethodPost, "/repos/test-owner/test-repo/git/refs") <= 1 {
				writeJSON(t, w, http.StatusUnprocessableEntity, map[string]any{"message": "Reference already exists"})
				return
			}
			writeJSON(t, w, http.StatusCreated, map[string]any{
				"ref":    "refs/heads/test-branch",
				"object": map[string]any{"sha": "main-sha", "type": "commit"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/test-owner/test-repo/git/refs/heads/test-branch":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"ref":    "refs/heads/test-branch",
				"object": map[string]any{"sha": "stale-sha", "type": "commit"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/test-owner/test-repo/pulls":
			writeJSON(t, w, http.StatusOK, []any{
				map[string]any{"number": 1, "head": map[string]any{"ref": "unrelated-branch"}},
				map[string]any{"number": 2, "head": map[string]any{"ref": "test-branch"}},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/test-owner/test-repo/pulls/2":
			writeJSON(t, w, http.StatusOK, map[string]any{"number": 2})
		case r.Method == http.MethodDelete && r.URL.Path == "/repos/test-owner/test-repo/git/refs/heads/test-branch":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}

	_, err := c.StartBranch(ctx, "test-branch")
	if err != nil {
		t.Fatalf("StartBranch returned error: %v", err)
	}

	if n := fake.count(http.MethodPatch, "/repos/test-owner/test-repo/pulls/1"); n != 0 {
		t.Errorf("unrelated PR #1 was closed %d time(s), want 0", n)
	}
	if n := fake.count(http.MethodPatch, "/repos/test-owner/test-repo/pulls/2"); n != 1 {
		t.Errorf("matching PR #2 close calls = %d, want 1", n)
	}
	if n := fake.count(http.MethodDelete, "/repos/test-owner/test-repo/git/refs/heads/test-branch"); n != 1 {
		t.Errorf("DeleteRef calls = %d, want 1", n)
	}
	if n := fake.count(http.MethodPost, "/repos/test-owner/test-repo/git/refs"); n != 2 {
		t.Errorf("CreateRef calls = %d, want 2", n)
	}

	listed := fake.find(http.MethodGet, "/repos/test-owner/test-repo/pulls")
	if listed == nil {
		t.Fatal("no PR list request recorded")
	}
	if got, want := listed.query.Get("head"), "test-owner:test-branch"; got != want {
		t.Errorf("head query = %q, want %q", got, want)
	}
}

// Test_WaitAndMerge_Squash asserts the merge request asks for a squash commit,
// since the target repository rejects merge commits.
func Test_WaitAndMerge_Squash(t *testing.T) {
	ctx := context.Background()

	c, fake := newFakeClient(t)
	fake.handler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/test-owner/test-repo/pulls/42":
			writeJSON(t, w, http.StatusOK, map[string]any{"number": 42, "mergeable": true})
		case r.Method == http.MethodPut && r.URL.Path == "/repos/test-owner/test-repo/pulls/42/merge":
			writeJSON(t, w, http.StatusOK, map[string]any{"merged": true, "message": "Pull Request successfully merged"})
		case r.Method == http.MethodDelete && r.URL.Path == "/repos/test-owner/test-repo/git/refs/heads/test-branch":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}

	brc := &BranchClient{client: c, branchName: "test-branch", branchRef: "refs/heads/test-branch", baseRef: "main"}
	pr := &github.PullRequest{Number: github.Int(42), Mergeable: github.Bool(true)}

	if err := brc.WaitAndMerge(ctx, pr); err != nil {
		t.Fatalf("WaitAndMerge returned error: %v", err)
	}

	merge := fake.find(http.MethodPut, "/repos/test-owner/test-repo/pulls/42/merge")
	if merge == nil {
		t.Fatal("no merge request recorded")
	}
	if got, want := merge.body["merge_method"], "squash"; got != want {
		t.Errorf("merge_method = %v, want %v (body: %v)", got, want, merge.body)
	}
}
