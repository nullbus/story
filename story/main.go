package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/nullbus/story"
)

type ShowConfig struct {
	BlogName string
	PostID   string
}

func (c *ShowConfig) Parse(args []string) error {
	flag := flag.NewFlagSet("story show", flag.ExitOnError)
	flag.StringVar(&c.BlogName, "blog", "", "tistory blog name, ex> {blog}.tistory.com")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "story show -blog=[blog id] [options] postID")
		flag.PrintDefaults()
	}

	if err := flag.Parse(args); err != nil {
		return err
	}

	if flag.NArg() == 0 {
		return errors.New("too few arguments")
	}

	c.PostID = flag.Arg(0)

	if c.BlogName == "" {
		return errors.New("missing blog name")
	}

	return nil
}

func usageAndExit() {
	write := func(args ...interface{}) { fmt.Fprintln(os.Stderr, args...) }
	write("Usage: story <command> [options...]")
	write("  story init")
	write("  story auth")
	write("  story info")
	write("  story show")
	write("  story edit")
	write("  story post")
	write("")
	write("-h for each command to get more information")

	os.Exit(1)
}

func main() {
	if len(os.Args) == 1 {
		usageAndExit()
	}

	switch os.Args[1] {
	case "init":
		var config story.InitConfig
		if err := config.Parse(os.Args[2:]); err != nil {
			log.Fatalln(err)
			return
		}

		for {
			fmt.Print("Client ID: ")
			fmt.Scanf("%s\n", &config.ClientID)

			if config.ClientID != "" {
				break
			}
		}

		for {
			fmt.Print("Client Secret: ")
			fmt.Scanf("%s\n", &config.ClientSecret)

			if config.ClientSecret != "" {
				break
			}
		}

		if err := config.Authorize(); err != nil {
			log.Fatalln(err)
			return
		}

	case "auth":
		var config story.InitConfig
		if err := config.Load(); err != nil {
			log.Fatalln("failed to load config file, try `story init` first")
		}

		if err := config.Authorize(); err != nil {
			log.Fatalln(err)
		}

	case "info":
		var config story.InitConfig
		if err := config.Load(); err != nil {
			log.Fatalln("failed to load config file, try `story init` first")
		}

		info, err := story.Info(config.AccessToken)
		if err != nil {
			log.Fatalln(err)
		}

		fmt.Println(info)

	case "show":
		var baseConfig story.InitConfig
		if err := baseConfig.Load(); err != nil {
			log.Fatalln(err)
		}

		var config ShowConfig
		if err := config.Parse(os.Args[2:]); err != nil {
			log.Fatalln(err)
		}

		post, err := story.FindPost(baseConfig.AccessToken, config.BlogName, config.PostID)
		if err != nil {
			log.Fatalln(err)
		}

		fmt.Printf("%+v\n", post)

	case "edit":
		var baseConfig story.InitConfig
		if err := baseConfig.Load(); err != nil {
			log.Println("failed to load config file, try `story init` first")
			os.Exit(1)
			return
		}

		var edit story.EditConfig
		if err := edit.Parse(os.Args[2:]); err != nil {
			log.Fatalln(err)
		}

		if err := edit.Do(baseConfig.AccessToken); err != nil {
			log.Fatalln(err)
		}

	case "post":
		var baseConfig story.InitConfig
		if err := baseConfig.Load(); err != nil {
			log.Println("failed to load config file, try `story init` first")
			os.Exit(1)
			return
		}

		var post story.PostConfig
		if err := post.Parse(os.Args[2:]); err != nil {
			log.Fatalln(err)
			return
		}

		if err := post.Do(baseConfig.AccessToken); err != nil {
			log.Fatalln(err)
		}

	default:
		usageAndExit()
	}

}
