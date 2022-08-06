package newsblur

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"

	"golang.org/x/net/publicsuffix"
)

const NewsblurTimeFormat = "2006-01-02 15:04:05.999999"

type LoginResponse struct {
	Authenticated bool        `json:"authenticated"`
	UserID        json.Number `json:"user_id"`
}

type StoriesInfoResponse struct {
	Authenticated bool    `json:"authenticated"`
	Stories       []Story `json:"stories"`
}

type Story struct {
	ID         string `json:"id"`
	Title      string `json:"story_title"`
	Permalink  string `json:"story_permalink"`
	Comment    string `json:"comments"`
	SharedDate string `json:"shared_date"`
}

type Client struct {
	client    *http.Client
	loginInfo *LoginResponse
}

func NewClient(ctx context.Context, username string, password string) (*Client, error) {
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return &Client{}, fmt.Errorf("creating cookiejar: %w", err)
	}

	client := &http.Client{
		Jar: jar,
	}

	c := &Client{client: client}
	_, err = c.login(ctx, username, password)

	return c, err
}

func (api *Client) login(ctx context.Context, username string, password string) (*LoginResponse, error) {
	values := url.Values{}
	values.Set("username", username)
	values.Set("password", password)

	loginURL := "https://newsblur.com/api/login"

	loginRes, err := api.client.PostForm(loginURL, values)
	if err != nil {
		return &LoginResponse{}, fmt.Errorf("post login form: %w", err)
	}
	defer loginRes.Body.Close()

	data, err := ioutil.ReadAll(loginRes.Body)
	if err != nil {
		return &LoginResponse{}, fmt.Errorf("reading login response body: %w", err)
	}

	var login LoginResponse
	if err := json.Unmarshal(data, &login); err != nil {
		return &LoginResponse{}, fmt.Errorf("unmarshaling login response body: %w", err)
	}

	api.loginInfo = &login

	return &login, nil
}

func (api *Client) GetSharedStories(ctx context.Context, userID json.Number, pageNum int) ([]Story, error) {
	log.Printf("newsblur:GetSharedStories: page=%d", pageNum)
	storiesURL := fmt.Sprintf("https://newsblur.com/social/stories/%s/?pag=%d&&order=newest&read_filter=all", string(userID), pageNum)

	// XXX: have a newsblur specific ticker
	// <-apiTicker.C
	storiesResp, err := api.client.Get(storiesURL)
	if err != nil {
		return nil, fmt.Errorf("getting stories response body: %w", err)
	}
	defer storiesResp.Body.Close()

	storiesData, err := ioutil.ReadAll(storiesResp.Body)
	if err != nil {
		log.Println(string(storiesData))
		return nil, fmt.Errorf("reading stories response body: %w", err)
	}

	var storiesResponse StoriesInfoResponse
	if err := json.Unmarshal(storiesData, &storiesResponse); err != nil {
		return nil, fmt.Errorf("unmarshaling stories response body: %w", err)
	}

	return storiesResponse.Stories, nil
}

func (api *Client) IterSharedStories(ctx context.Context, checkpoint *time.Time) <-chan Story {
	ch := make(chan Story)

	go func() {
		defer close(ch)
		pageNum := 1
		for {
			stories, err := api.GetSharedStories(ctx, api.loginInfo.UserID, pageNum)
			if err != nil {
				// TODO: Send error down somehow
				log.Fatal(fmt.Errorf("error getting shared stories: %w", err))
				return
			}
			pageNum++

			for _, story := range stories {
				// TODO: Make this in unmarshall
				date, err := time.Parse(NewsblurTimeFormat, story.SharedDate)
				// TODO: Send error down somehow
				if err != nil {
					log.Fatal(fmt.Errorf("error parsing date of story: %w", err))
				}
				if checkpoint != nil && (checkpoint.After(date) || checkpoint.Equal(date)) {
					return
				}
				ch <- story
			}

			if len(stories) == 0 {
				return
			}
		}
	}()
	return ch
}
