package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/urfave/cli"
	"os"
)

var Version string

func BuildApp() *cli.App {
	app := cli.NewApp()
	app.Name = "reaper"
	app.Version = Version
	app.Usage = "access logs to queues"
	app.Description = "reaper receives access logs from a web server and pushes the logs to an external message queue"

	app.Flags = []cli.Flag{
		cli.IntFlag{
			Name: "port",
			Usage: "port to listen on",
			EnvVar: "REAPER_LISTEN_PORT",
			Value: 1514,
		},
		cli.StringFlag{
			Name: "addr",
			Usage: "interface address to bind",
			EnvVar: "REAPER_LISTEN_ADDR",
			Value: "127.0.0.1",
		},
	}

	app.Action = func(c *cli.Context) error {
		entries := make(chan *Entry)
		go func() {
			_ = listen(context.Background(), c.GlobalString("addr"), c.GlobalInt("port"), entries)
		}()
		for entry := range entries {
			b, err := json.Marshal(entry)
			if err == nil {
				fmt.Println(string(b))
			}
		}
		return nil
	}

	app.Commands = []cli.Command{
		{
			Name: "kafka",
			Usage: "push access logs to kafka",
			Action: func(c *cli.Context) error {
				fmt.Println("kafka action")
				return nil
			},
		},
		{
			Name: "stdout",
			Usage: "just write access logs to stdout",
			Action: func(c *cli.Context) error {
				return nil
			},
		},
	}

	return app
}

func main() {
	_ = BuildApp().Run(os.Args)
}