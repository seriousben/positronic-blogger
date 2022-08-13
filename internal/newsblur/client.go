package newsblur

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"

	"golang.org/x/net/publicsuffix"
)

const (
	newsblurTimeFormat  = "2006-01-02 15:04:05.999999"
	apiRequestRateLimit = 720 * time.Millisecond
)

type LoginResponse struct {
	Authenticated bool `json:"authenticated"`
	UserID        int  `json:"user_id"`
}

type StoriesInfoResponse struct {
	Authenticated bool     `json:"authenticated"`
	Stories       []*Story `json:"stories"`
}

type Story struct {
	ID         string
	Title      string
	Permalink  string
	Comment    string
	SharedDate time.Time
}

func (s *Story) UnmarshalJSON(bytes []byte) error {
	type storyJSON struct {
		ID            string `json:"id"`
		Title         string `json:"story_title"`
		Permalink     string `json:"story_permalink"`
		Comment       string `json:"comments"`
		SharedDateStr string `json:"shared_date"`
	}

	var stJSON storyJSON
	err := json.Unmarshal(bytes, &stJSON)
	if err != nil {
		return err
	}

	st := Story{
		ID:        stJSON.Title,
		Title:     stJSON.Title,
		Permalink: stJSON.Permalink,
		Comment:   stJSON.Comment,
	}
	st.SharedDate, err = time.Parse(newsblurTimeFormat, stJSON.SharedDateStr)
	if err != nil {
		return fmt.Errorf("error parsing date of story: %w", err)
	}

	*s = st
	return nil
}

type Client struct {
	client    *http.Client
	apiTicker *time.Ticker
	loginInfo *LoginResponse
}

func New(ctx context.Context, username string, password string) (*Client, error) {
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, fmt.Errorf("creating cookiejar: %w", err)
	}

	client := &http.Client{
		Jar: jar,
	}

	c := &Client{
		client:    client,
		apiTicker: time.NewTicker(apiRequestRateLimit),
	}
	lr, err := c.login(ctx, username, password)
	if err != nil {
		return nil, fmt.Errorf("login error: %w", err)
	}
	c.loginInfo = lr

	return c, err
}

func (c *Client) login(ctx context.Context, username string, password string) (*LoginResponse, error) {
	values := url.Values{}
	values.Set("username", username)
	values.Set("password", password)

	loginURL := "https://newsblur.com/api/login"

	loginRes, err := c.client.PostForm(loginURL, values)
	if err != nil {
		return nil, fmt.Errorf("post login form: %w", err)
	}
	defer loginRes.Body.Close()

	if loginRes.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("login failure")
	}

	data, err := io.ReadAll(loginRes.Body)
	if err != nil {
		return nil, fmt.Errorf("reading login response body: %w", err)
	}

	// fmt.Println("DEBUG login>", string(data), loginRes.Status)

	var login LoginResponse
	if err := json.Unmarshal(data, &login); err != nil {
		return nil, fmt.Errorf("unmarshaling login response body: %w", err)
	}

	return &login, nil
}

func (c *Client) GetSharedStories(ctx context.Context, pageNum int) ([]*Story, error) {
	log.Printf("newsblur:GetSharedStories: page=%d\n", pageNum)
	storiesURL := fmt.Sprintf("https://newsblur.com/social/stories/%d/?page=%d&&order=newest&read_filter=all", c.loginInfo.UserID, pageNum)

	<-c.apiTicker.C
	resp, err := c.client.Get(storiesURL)
	if err != nil {
		return nil, fmt.Errorf("getting stories response body: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Println(string(data))
		return nil, fmt.Errorf("reading stories response body: %w", err)
	}

	//fmt.Println("DEBUG getSocialStories>", string(data), resp.Status)

	var storiesResponse StoriesInfoResponse
	if err := json.Unmarshal(data, &storiesResponse); err != nil {
		return nil, fmt.Errorf("unmarshaling stories response body: %w", err)
	}

	return storiesResponse.Stories, nil
}

type SharedStoriesIterator struct {
	client     *Client
	pageNum    int
	newerThan  *time.Time
	curIdx     int
	curPage    []*Story
	reachedEnd bool
}

func (it *SharedStoriesIterator) Next(ctx context.Context) (*Story, error) {
	if it.reachedEnd {
		fmt.Println("DEBUG SharedStoriesIterator EOF had no next")
		return nil, io.EOF
	}

	// Out of current page's bound or no current page.
	if len(it.curPage) <= it.curIdx {
		stories, err := it.client.GetSharedStories(ctx, it.pageNum)
		if err != nil {
			return nil, fmt.Errorf("error getting shared stories: %w", err)
		}

		it.pageNum++
		it.curIdx = 0
		it.curPage = stories
	}

	// Within current's page bound.
	if len(it.curPage) > it.curIdx {
		st := it.curPage[it.curIdx]
		it.curIdx++

		if it.newerThan.After(st.SharedDate) || it.newerThan.Equal(st.SharedDate) {
			fmt.Println("DEBUG SharedStoriesIterator EOF reached newerThan")
			it.reachedEnd = true
			return nil, io.EOF
		}

		return st, nil
	}

	fmt.Println("DEBUG SharedStoriesIterator EOF has no next page")
	it.reachedEnd = true
	return nil, io.EOF
}

func (c *Client) SharedStoriesIterator(ctx context.Context, newerThan time.Time) (*SharedStoriesIterator, error) {
	return &SharedStoriesIterator{
		pageNum:   1,
		newerThan: &newerThan,
		client:    c,
	}, nil
}
