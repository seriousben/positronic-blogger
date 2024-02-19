package server

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/seriousben/positronic-blogger/internal/github"
	"github.com/seriousben/positronic-blogger/internal/template"
)

var (
	commands = []discordgo.ApplicationCommand{
		{
			Name:        "serious-post",
			Description: "Post a new Curated Link to seriousben.com",
		},
	}
	commandsHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
		"serious-post": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseModal,
				Data: &discordgo.InteractionResponseData{
					CustomID: "serious-post",
					Title:    "Post a new curated link",
					Components: []discordgo.MessageComponent{
						discordgo.ActionsRow{
							Components: []discordgo.MessageComponent{
								discordgo.TextInput{
									CustomID:  "title",
									Label:     "Title of article",
									Style:     discordgo.TextInputShort,
									Required:  true,
									MaxLength: 300,
									MinLength: 1,
								},
							},
						},
						discordgo.ActionsRow{
							Components: []discordgo.MessageComponent{
								discordgo.TextInput{
									CustomID:  "URL",
									Label:     "URL of article",
									Style:     discordgo.TextInputShort,
									Required:  true,
									MaxLength: 300,
									MinLength: 1,
								},
							},
						},
						discordgo.ActionsRow{
							Components: []discordgo.MessageComponent{
								discordgo.TextInput{
									CustomID:  "thoughts",
									Label:     "Thoughts about the article",
									Style:     discordgo.TextInputParagraph,
									Required:  false,
									MaxLength: 2000,
								},
							},
						},
					},
				},
			})
			if err != nil {
				panic(err)
			}
		},
	}
)

const (
	envDryRun          = "POSITRONIC_DRY_RUN"
	envGithubRepo      = "POSITRONIC_GITHUB_REPO"
	envGithubToken     = "POSITRONIC_GITHUB_TOKEN"
	envDiscordGuildID  = "POSITRONIC_DISCORD_GUILDID"
	envDiscordToken    = "POSITRONIC_DISCORD_TOKEN"
	envDiscordAppID    = "POSITRONIC_DISCORD_APPID"
	envBlogContentPath = "POSITRONIC_BLOG_CONTENT_PATH"
)

func Main() {
	var (
		ctx            = context.Background()
		dryRun         = os.Getenv(envDryRun) == "true"
		ghToken        = os.Getenv(envGithubToken)
		ghRepoFull     = os.Getenv(envGithubRepo)
		discordGuildID = os.Getenv(envDiscordGuildID)
		discordToken   = os.Getenv(envDiscordToken)
		discordAppID   = os.Getenv(envDiscordAppID)
		contentPath    = "content/links"
		ghOwner        string
		ghRepo         string
	)

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	if ghToken == "" || ghRepoFull == "" {
		log.Fatalf("missing %s or %s", envGithubRepo, envGithubToken)
	}

	if ghRepoFullSplit := strings.Split(ghRepoFull, "/"); len(ghRepoFullSplit) == 2 {
		ghOwner = ghRepoFullSplit[0]
		ghRepo = ghRepoFullSplit[1]
	} else {
		log.Fatalf("malformed %s (%s) - expected format to be owner/repo", envGithubRepo, ghRepoFull)
	}

	ghClient, err := github.New(ctx, ghToken, ghOwner, ghRepo)
	if err != nil {
		log.Fatalf("error instantiating github client: %v", err)
	}

	s, err := discordgo.New("Bot " + discordToken)
	if err != nil {
		log.Fatalf("Invalid bot parameters: %v", err)
	}

	s.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Println("Bot is up!")
	})

	s.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		user := i.Member.User
		if user.ID != "905167870403702884" {
			log.Printf("user %s not allowed: %+v", user.ID, user)
			return
		}

		switch i.Type {
		case discordgo.InteractionApplicationCommand:
			if h, ok := commandsHandlers[i.ApplicationCommandData().Name]; ok {
				h(s, i)
			}
		case discordgo.InteractionModalSubmit:
			data := i.ModalSubmitData()
			switch data.CustomID {
			case "serious-post":
				inputByID := map[string]discordgo.MessageComponent{}
				for _, c := range data.Components {
					if ar, ok := c.(*discordgo.ActionsRow); ok {
						for _, c := range ar.Components {
							if ti, ok := c.(*discordgo.TextInput); ok {
								inputByID[ti.CustomID] = ti
							}
						}
					}
				}

				now := time.Now()
				p := template.Post{
					Title:   inputByID["title"].(*discordgo.TextInput).Value,
					URL:     inputByID["URL"].(*discordgo.TextInput).Value,
					Comment: inputByID["thoughts"].(*discordgo.TextInput).Value,
					Date:    now,
				}

				buf, err := p.ToMarkdown()
				if err != nil {
					log.Printf("error generating markdown: %v\n", err)
					return
				}

				err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{},
				})
				if err != nil {
					log.Printf("error sending acknowledgement: %v\n", err)
					return
				}

				// start branch on first new content.
				brc, err := ghClient.StartBranch(ctx, fmt.Sprintf("%s-positronic-blogger", now.Format("2006-01-02T1504")))
				if err != nil {
					log.Printf("error creating branch: %v\n", err)
					return
				}

				fileName := p.FileName()
				commit := fmt.Sprintf("auto: new curated link %s", fileName)

				err = brc.CreateFile(ctx, commit, path.Join(contentPath, fileName), buf.String())
				if err != nil {
					log.Printf("error creating file in branch: %v\n", err)
					return
				}

				pr, err := brc.PullRequest(
					ctx,
					fmt.Sprintf("%s-positronic-blogger", now.Format(time.RFC3339)),
					"Auto blogging done from https://github.com/seriousben/positronic-blogger",
				)
				if err != nil {
					log.Printf("error creating pull request: %v\n", err)
					return
				}
				if !dryRun {
					err = brc.WaitAndMerge(ctx, pr)
					if err != nil {
						log.Printf("error waiting and merging: %v\n", err)
						return
					}
				}

				content := p.Title + " posted successfully\n\n" + buf.String()
				_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
					Content: &content,
					Components: &[]discordgo.MessageComponent{
						discordgo.ActionsRow{
							Components: []discordgo.MessageComponent{
								/*discordgo.Button{
									Emoji: &discordgo.ComponentEmoji{
										Name: "✏️",
									},
									Label:    "Edit",
									Style:    discordgo.SecondaryButton,
									CustomID: "edit_" + p.FileName(),
								},*/
								discordgo.Button{
									Emoji: &discordgo.ComponentEmoji{
										Name: "🔍",
									},
									Label: "View",
									Style: discordgo.LinkButton,
									URL:   "https://seriousben.com/links/" + p.FileName(),
								},
							},
						},
					},
				})
				if err != nil {
					log.Printf("error with interactive component for success: %v\n", err)
				}
				return
			default:
				log.Printf("unknown customID modal submit: %s\n", data.CustomID)
				return
			}
		default:
			log.Printf("unknown interaction type: %s", i.Type.String())
		}
	})

	cmdIDs := make(map[string]string, len(commands))

	for _, cmd := range commands {
		rcmd, err := s.ApplicationCommandCreate(discordAppID, discordGuildID, &cmd)
		if err != nil {
			log.Fatalf("Cannot create slash command %q: %v", cmd.Name, err)
		}

		cmdIDs[rcmd.ID] = rcmd.Name
	}

	if err := s.Open(); err != nil {
		log.Fatalf("Cannot open the session: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()

		if dryRun {
			for id, name := range cmdIDs {
				err := s.ApplicationCommandDelete(discordAppID, discordGuildID, id)
				if err != nil {
					log.Fatalf("Cannot delete slash command %q: %v", name, err)
				}
			}
		}

		s.Close()
	}()
	wg.Wait()
}
