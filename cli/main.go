package main

import (
	"os"
	"os/signal"
	"time"

	"github.com/ghetzel/cli"
	"github.com/ghetzel/procwatch"
	"github.com/op/go-logging"
)

var log = logging.MustGetLogger(`main`)

func main() {
	app := cli.NewApp()
	app.Name = `procwatch`
	app.Usage = `A process execution monitor`
	app.Version = procwatch.Version
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
		cli.DurationFlag{
			Name:  `max-stop-timeout`,
			Usage: `The maximum amount of time to wait for programs to gracefully stop when stopping the manager before killing them.`,
			Value: (120 * time.Second),
		},
	}

	app.Before = func(c *cli.Context) error {
		logging.SetFormatter(logging.MustStringFormatter(`%{color}%{level:.4s}%{color:reset}[%{id:04d}] %{message}`))

		if level, err := logging.LogLevel(c.String(`log-level`)); err == nil {
			logging.SetLevel(level, ``)
		} else {
			return err
		}

		logging.SetLevel(logging.ERROR, `diecast`)

		log.Infof("Starting %s %s", c.App.Name, c.App.Version)
		return nil
	}

	app.Action = func(c *cli.Context) {
		manager := procwatch.NewManager(c.String(`config`))
		signalChan := make(chan os.Signal, 1)
		signal.Notify(signalChan, os.Interrupt)

		go func() {
			for sig := range signalChan {
				log.Infof("Received signal %v, stopping all programs...", sig)
				exitCode := make(chan int)

				go func() {
					manager.Stop(false)
					exitCode <- 0
				}()

				select {
				case code := <-exitCode:
					log.Debugf("Stop completed with exit code %d", code)
					os.Exit(code)
					return

				case <-time.After(c.Duration(`max-stop-timeout`)):
					log.Warningf("Timed out waiting for programs to stop, force killing them...")
					reallyStop := make(chan error)

					go func() {
						manager.Stop(true)
						reallyStop <- nil
					}()

					select {
					case err := <-reallyStop:
						log.Fatalf("Received error force killing programs: %v", err)

					case <-time.After(c.Duration(`max-stop-timeout`)):
						log.Errorf("Failed to stop all programs. Here are the PIDs that we were managing:")

						for _, program := range manager.Programs() {
							log.Errorf("  Program: name=%s, state=%s, pid=%d", program.Name, program.State, program.ProcessID)
						}
					}
				}

				os.Exit(3)
				return
			}
		}()

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
