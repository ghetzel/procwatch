package main

import (
	"github.com/ghetzel/cli"
	"github.com/op/go-logging"
	"os"
	"os/signal"
)

var log = logging.MustGetLogger(`main`)

func main() {
	app := cli.NewApp()
	app.Name = `procwatch-tester`
	app.Usage = `A process that aims to misbehave`
	app.Version = `0.0.1`
	app.EnableBashCompletion = false

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  `pidfile, p`,
			Usage: `Where the pidfile for this process will be written`,
			Value: `tester.pid`,
		},
		cli.IntFlag{
			Name:  `exit-status, E`,
			Usage: `The status that a successfully-terminated process will exit with`,
		},
	}

	app.Before = func(c *cli.Context) error {
		logging.SetFormatter(logging.MustStringFormatter(`%{color}%{level:.4s}%{color:reset}[%{id:04d}] %{message}`))
		logging.SetLevel(logging.DebugLevel, ``)

		log.Infof("Starting %s %s", c.App.Name, c.App.Version)
		return nil
	}

	app.Action = func(c *cli.Context) {
		signalChan := make(chan os.Signal, 1)
		signal.Notify(signalChan, os.Interrupt)

		go func() {
			for _ = range signalChan {
				os.Exit(c.Int(`exit-status`))
			}
		}()

		if err := localPeer.Listen(); err != nil {
			log.Fatal(err)
		}
		select {}
	}

	app.Run(os.Args)
}
