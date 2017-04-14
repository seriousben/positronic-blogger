package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/go-github/github"
	"github.com/gosimple/slug"
	"github.com/pkg/errors"
	"golang.org/x/net/publicsuffix"
	"golang.org/x/oauth2"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"text/template"
	"time"
)

const NEWSBLUR_TIME_FORMAT = "2006-01-02 15:04:05.999999"
const BLOG_REPOSITORY_USER = "seriousben"
const BLOG_REPOSITORY = "seriousben.com"
const BLOG_POST_PATH = "content/nano/"

const API_REQUEST_RATE = 720 * time.Millisecond

var (
	apiTicker = time.NewTicker(API_REQUEST_RATE)
)

type NewsblurLogin struct {
	Authenticated bool        `json:"authenticated"`
	UserID        json.Number `json:"user_id"`
}

type NewsblurStoriesInfo struct {
	Authenticated bool            `json:"authenticated"`
	Stories       []NewsblurStory `json:"stories"`
}

type NewsblurStory struct {
	ID         string `json:"id"`
	Title      string `json:"story_title"`
	Permalink  string `json:"story_permalink"`
	Comment    string `json:"comments"`
	SharedDate string `json:"shared_date"`
}

type BlogPost struct {
	Title   string
	Url     string
	Comment string
	Date    time.Time
}

type Checkpoint struct {
	SHA        string
	Checkpoint *time.Time
}

func NewBlogPost(story *NewsblurStory) (*BlogPost, error) {
	date, err := time.Parse(NEWSBLUR_TIME_FORMAT, story.SharedDate)
	if err != nil {
		return nil, errors.Wrap(err, "Error parsing date of story")
	}

	return &BlogPost{
		Title:   story.Title,
		Url:     story.Permalink,
		Comment: story.Comment,
		Date:    date,
	}, nil
}

func ToStringPtr(str string) *string {
	return &str
}

func retryFunc(theFunc func() (bool, error)) error {
	retry, err := theFunc()
	if retry {
		_, err = theFunc()
		return err
	}
	return err
}

func GetCheckpoint(githubClient *github.Client) (*Checkpoint, error) {
	<-apiTicker.C
	fileContent, _, _, err := githubClient.Repositories.GetContents(context.TODO(), BLOG_REPOSITORY_USER, BLOG_REPOSITORY, BLOG_POST_PATH+"checkpoint", nil)
	if err != nil {
		return nil, errors.Wrap(err, "Error getting checkpoint")
	}

	content, err := fileContent.GetContent()
	if err != nil {
		return nil, errors.Wrap(err, "Error decoding file content")
	}
	if content == "" {
		log.Println("Checkpoint was empty, full resync will happen")
		return &Checkpoint{
			SHA:        fileContent.GetSHA(),
			Checkpoint: nil,
		}, nil
	}

	date, err := time.Parse(NEWSBLUR_TIME_FORMAT, content)
	return &Checkpoint{
		SHA:        fileContent.GetSHA(),
		Checkpoint: &date,
	}, errors.Wrap(err, "Error parsing checkpoint")
}

func SetCheckpoint(githubClient *github.Client, prevCheckpoint *Checkpoint, checkpoint time.Time) error {
	dateStr := checkpoint.Format(NEWSBLUR_TIME_FORMAT)

	commit := "auto: set checkpoint and deploy"
	var opts = github.RepositoryContentFileOptions{
		Message:   &commit,
		Content:   []byte(dateStr),
		SHA:       &prevCheckpoint.SHA,
		Committer: &github.CommitAuthor{Name: ToStringPtr("Benjamin Boudreau"), Email: ToStringPtr("boudreau.benjamin@gmail.com")},
	}

	// Workaround for 409 status code by github
	apiCall := func() (bool, error) {
		<-apiTicker.C
		_, resp, err := githubClient.Repositories.UpdateFile(context.TODO(), BLOG_REPOSITORY_USER, BLOG_REPOSITORY, BLOG_POST_PATH+"checkpoint", &opts)

		if resp.StatusCode == 409 {
			return true, nil
		}

		return false, err
	}

	err := retryFunc(apiCall)

	return errors.Wrap(err, "Updating checkpoint file in github")
}

func Blog(githubClient *github.Client, post *BlogPost) error {
	tmplStr, err := ioutil.ReadFile("short.tmpl")
	if err != nil {
		return errors.Wrap(err, "Reading template file")
	}
	// TODO: Do only once
	tmpl, err := template.New("short").Funcs(template.FuncMap{
		"quote": strconv.Quote,
	}).Parse(string(tmplStr))
	if err != nil {
		return errors.Wrap(err, "Parsing template file")
	}
	fileName := slug.Make(post.Title) + ".md"
	commit := "auto: new short post " + fileName + " [skip ci]"

	buf := new(bytes.Buffer)
	err = tmpl.Execute(buf, post)
	if err != nil {
		return errors.Wrap(err, "Executing template")
	}

	var opts = github.RepositoryContentFileOptions{
		Message:   &commit,
		Content:   buf.Bytes(),
		Committer: &github.CommitAuthor{Name: ToStringPtr("Benjamin Boudreau"), Email: ToStringPtr("boudreau.benjamin@gmail.com")},
	}

	// Workaround for 409 status code by github
	apiCall := func() (bool, error) {
		<-apiTicker.C
		_, resp, err := githubClient.Repositories.CreateFile(context.TODO(), BLOG_REPOSITORY_USER, BLOG_REPOSITORY, BLOG_POST_PATH+fileName, &opts)

		if resp.StatusCode == 409 {
			return true, nil
		}

		return false, err
	}

	err = retryFunc(apiCall)

	if err != nil {
		return errors.Wrap(err, "Creating blog post file in github")
	}

	log.Printf("Created %s (%s)", commit, post.Date)
	return nil
}

type NewsblurAPI struct {
	username string
	password string
	client   *http.Client
}

func NewNewsblurAPI(username string, password string) (*NewsblurAPI, error) {
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return &NewsblurAPI{}, errors.Wrap(err, "Creating cookiejar")
	}

	<-apiTicker.C
	client := &http.Client{
		Jar: jar,
	}

	return &NewsblurAPI{username, password, client}, nil
}

func (api *NewsblurAPI) Login() (*NewsblurLogin, error) {
	values := url.Values{}
	values.Set("username", api.username)
	values.Set("password", api.password)

	loginURL := "https://newsblur.com/api/login"

	<-apiTicker.C
	loginRes, err := api.client.PostForm(loginURL, values)
	if err != nil {
		return &NewsblurLogin{}, errors.Wrap(err, "Post login form")
	}
	defer loginRes.Body.Close()

	data, err := ioutil.ReadAll(io.LimitReader(loginRes.Body, 1048576))
	if err != nil {
		return &NewsblurLogin{}, errors.Wrap(err, "Reading login response body")
	}
	var login NewsblurLogin
	if err := json.Unmarshal(data, &login); err != nil {
		return &NewsblurLogin{}, errors.Wrap(err, "Unmarshaling login response body")
	}

	return &login, nil
}

func (api *NewsblurAPI) GetSharedStories(userID json.Number, pageNum int) (*[]NewsblurStory, error) {
	log.Printf("newsblur:GetSharedStories: page=%d", pageNum)
	storiesURL := "https://newsblur.com/social/stories/" + string(userID) + "/?page=" + strconv.Itoa(pageNum) + "&order=newest&read_filter=all"

	<-apiTicker.C
	storiesResp, err := api.client.Get(storiesURL)
	if err != nil {
		return &[]NewsblurStory{}, errors.Wrap(err, "Getting stories response body")
	}
	defer storiesResp.Body.Close()

	storiesData, err := ioutil.ReadAll(io.LimitReader(storiesResp.Body, 1048576))
	if err != nil {
		log.Println(string(storiesData))
		return &[]NewsblurStory{}, errors.Wrap(err, "Reading stories response body")
	}

	var storiesResponse NewsblurStoriesInfo
	if err := json.Unmarshal(storiesData, &storiesResponse); err != nil {
		return &[]NewsblurStory{}, errors.Wrap(err, "Unmarshaling stories response body")
	}
	//log.Print("> call", storiesResponse.Stories)
	return &storiesResponse.Stories, nil
}

func (api *NewsblurAPI) IterStories(checkpoint *time.Time) (<-chan *NewsblurStory, error) {
	login, err := api.Login()
	if err != nil {
		return nil, errors.Wrap(err, "Error on login")
	}
	ch := make(chan *NewsblurStory)

	go func() {
		defer close(ch)
		pageNum := 1
		for true {
			stories, err := api.GetSharedStories(login.UserID, pageNum)
			if err != nil {
				// TODO: Send error down somehow
				if err != nil {
					log.Fatal(errors.Wrap(err, "Error getting shared stories"))
				}
				return
			}
			pageNum++

			// Using range results in the same pointer being reused causing a race condition :(
			for i := 0; i != len(*stories); i++ {
				story := (*stories)[i]

				// TODO: Make this in unmarshall
				date, err := time.Parse(NEWSBLUR_TIME_FORMAT, story.SharedDate)
				// TODO: Send error down somehow
				if err != nil {
					log.Fatal(errors.Wrap(err, "Error parsing date of story"))
				}
				if checkpoint != nil && (checkpoint.After(date) || checkpoint.Equal(date)) {
					ch <- nil
					return
				}
				ch <- &story
			}

			if err != nil || len(*stories) == 0 {
				ch <- nil
				return
			}
		}
	}()
	return ch, nil
}

func NewGithubClient(githubToken string) (*github.Client, error) {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: githubToken},
	)
	tc := oauth2.NewClient(oauth2.NoContext, ts)

	return github.NewClient(tc), nil
}

func SyncSharedStoriesWithPosts(githubClient *github.Client, newsblurAPI *NewsblurAPI) {
	checkpoint, err := GetCheckpoint(githubClient)
	if err != nil {
		log.Fatal(err)
	}

	ch, err := newsblurAPI.IterStories(checkpoint.Checkpoint)
	if err != nil {
		log.Fatal(err)
	}
	pageNum := 1
	var newCheckpoint *time.Time
	for story := range ch {
		// Sentinel for done
		if story == nil {
			break
		}
		log.Printf("Creating Story[%d]: %s %d\n", pageNum, story.ID, &story)
		pageNum += 1
		blogPost, err := NewBlogPost(story)
		if err != nil {
			log.Println("Error creating blog post", err)
			break
		}

		err = Blog(githubClient, blogPost)
		if err != nil {
			log.Println("Error posting blog post", err)
			break
		}
		log.Printf("Created Story[%d] successfully", pageNum-1)

		if newCheckpoint == nil {
			newCheckpoint = &blogPost.Date
		}
	}

	if newCheckpoint != nil && (checkpoint.Checkpoint == nil || !checkpoint.Checkpoint.Equal(*newCheckpoint)) {
		err = SetCheckpoint(githubClient, checkpoint, *newCheckpoint)
		if err != nil {
			log.Fatal("Could not persist new checkpoint", err)
		}
	} else {
		log.Print("Nothing to see here, carry on")
	}
}

func getenv(name string) string {
	val := os.Getenv(name)
	if val == "" {
		log.Fatalf("$%s must be set", name)
	}
	return val
}

func main() {
	githubToken := getenv("GITHUB_TOKEN")
	newsblurUsername := getenv("NEWSBLUR_USERNAME")
	newsblurPassword := getenv("NEWSBLUR_PASSWORD")

	githubClient, err := NewGithubClient(githubToken)
	if err != nil {
		log.Fatal(err)
	}

	newsblurAPI, err := NewNewsblurAPI(newsblurUsername, newsblurPassword)
	if err != nil {
		log.Fatal(err)
	}

	SyncSharedStoriesWithPosts(githubClient, newsblurAPI)
}
