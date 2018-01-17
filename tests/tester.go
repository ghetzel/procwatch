package main

import (
	"os"
	"os/signal"
	"time"

	"github.com/ghetzel/cli"
	"github.com/op/go-logging"
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
			Name:  `exit-status, s`,
			Usage: `The status that a successfully-terminated process will exit with`,
		},
		cli.DurationFlag{
			Name:  `exit-after, t`,
			Usage: `How long to wait before exiting.  If not set, process will run until explicitly terminated.`,
		},
	}

	app.Before = func(c *cli.Context) error {
		logging.SetFormatter(logging.MustStringFormatter(`%{color}%{level:.4s}%{color:reset}[%{id:04d}] %{message}`))
		logging.SetLevel(logging.DEBUG, ``)

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

		if exitAfter := c.Duration(`exit-after`); exitAfter == 0 {
			select {}
		} else {
			select {
			case <-time.After(exitAfter):
				os.Exit(c.Int(`exit-status`))
			}
		}
	}

	app.Run(os.Args)
}
