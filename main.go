package main

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"text/template"
	"time"

	"github.com/google/go-github/github"
	"github.com/gorilla/mux"
	"github.com/gosimple/slug"
	"github.com/subosito/gotenv"
	"golang.org/x/oauth2"
)

type NewsEntry struct {
	Title   string `json:title`
	Url     string `json:url`
	Comment string `json:comment`
	Token   string `json:token`
	Date    string
}

// Blog

var (
	client *github.Client
)

func ToStringPtr(str string) *string {
	return &str
}

func NewPost(e NewsEntry) {
	tmpl, err := template.ParseFiles("short.tmpl")
	if err != nil {
		panic(err)
	}

	fileName := slug.Make(e.Title) + ".md"
	commit := "auto: new short post " + fileName

	e.Date = time.Now().UTC().Format("2017-01-31T18:32:38-05:00")

	buf := new(bytes.Buffer)
	err = tmpl.Execute(buf, e)
	if err != nil {
		panic(err)
	}

	var opts = github.RepositoryContentFileOptions{
		Message:   &commit,
		Content:   buf.Bytes(),
		Committer: &github.CommitAuthor{Name: ToStringPtr("Benjamin Boudreau"), Email: ToStringPtr("boudreau.benjamin@gmail.com")},
	}
	_, _, err = client.Repositories.CreateFile(
		"seriousben", "seriousben.com", "content/short/"+fileName,
		&opts)

	if err != nil {
		panic(err)
	}
}

func NewsCreate(w http.ResponseWriter, r *http.Request) {
	var entry NewsEntry
	body, err := ioutil.ReadAll(io.LimitReader(r.Body, 1048576))
	if err != nil {
		panic(err)
	}
	// Debug
	log.Println(string(body))
	if err := r.Body.Close(); err != nil {
		panic(err)
	}
	if err := json.Unmarshal(body, &entry); err != nil {
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		w.WriteHeader(http.StatusUnprocessableEntity)
		if err := json.NewEncoder(w).Encode(err); err != nil {
			panic(err)
		}
		return
	}
	if token := os.Getenv("ACCESS_TOKEN"); token != entry.Token {
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		w.WriteHeader(http.StatusForbidden)
		log.Println("Wrong access token")
		return
	}
	log.Println(entry.Title)
	w.WriteHeader(http.StatusOK)
	NewPost(entry)
}

func main() {
	gotenv.Load()

	port := os.Getenv("PORT")
	if port == "" {
		log.Fatal("$PORT must be set")
	}

	if token := os.Getenv("ACCESS_TOKEN"); token == "" {
		log.Fatal("$ACCESS_TOKEN must be set")
	}

	githubToken := os.Getenv("GITHUB_TOKEN")
	if githubToken == "" {
		log.Fatal("$GITHUB_TOKEN must be set")
	}

	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: githubToken},
	)
	tc := oauth2.NewClient(oauth2.NoContext, ts)

	client = github.NewClient(tc)

	router := mux.NewRouter().StrictSlash(true)
	router.
		Methods("POST").
		Path("/news").
		Name("NewsCreate").
		Handler(http.HandlerFunc(NewsCreate))

	log.Fatal(http.ListenAndServe(":"+port, router))
}
