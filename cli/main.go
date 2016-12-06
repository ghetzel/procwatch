package main

import (
	"github.com/ghetzel/cli"
	"github.com/ghetzel/procwatch"
	"github.com/op/go-logging"
	"os"
)

var log = logging.MustGetLogger(`main`)

func main() {
	app := cli.NewApp()
	app.Name = `procwatch`
	app.Usage = `A process execution monitor`
	app.Version = `0.0.1`
	app.EnableBashCompletion = false

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:   `log-level, L`,
			Usage:  `Level of log output verbosity`,
			Value:  `debug`,
			EnvVar: `LOGLEVEL`,
		},
		cli.StringFlag{
			Name:   `config, c`,
			Usage:  `The configuration file to load`,
			Value:  `config.ini`,
			EnvVar: `PROCWATCH_CONFIG`,
		},
	}

	app.Before = func(c *cli.Context) error {
		logging.SetFormatter(logging.MustStringFormatter(`%{color}%{level:.4s}%{color:reset}[%{id:04d}] %{message}`))

		if level, err := logging.LogLevel(c.String(`log-level`)); err == nil {
			logging.SetLevel(level, ``)
		} else {
			return err
		}

		log.Infof("Starting %s %s", c.App.Name, c.App.Version)
		return nil
	}

	app.Action = func(c *cli.Context) {
		manager := procwatch.NewManager(c.String(`config`))

		if err := manager.Initialize(); err == nil {
			manager.Run()
		} else {
			log.Fatal(err)
		}
	}

	// app.Commands = []cli.Command{
	// 	{
	// 		Name: `status`,
	// 		Usage: `Show the current status of all registered processes.`,
	// 		Flags: []cli.Flag{
	// 			cli.IntFlag{
	// 				Name: `refresh-interval, i`,
	// 				Usage: `How frequently to refresh the status output (0 to disable).`,
	// 			},
	// 		},
	// 		Action: func(c *cli.Context) {

	// 		},
	// 	},
	// }

	app.Run(os.Args)
}
