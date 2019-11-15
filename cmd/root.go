package cmd

import (
	"log"
	"os"

	"github.com/urfave/cli/v2"
)

var root = &cli.App{
	Name:  "newsblur-to-hugo",
	Usage: "A tool to expose newblur shared stories in a hugo blog.",
}

func Register(c *cli.Command) {
	root.Commands = append(root.Commands, c)
}

func Execute(args []string) {
	err := root.Run(args)
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}
}
