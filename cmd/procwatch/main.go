package main

import (
	"io"
	"os"
	"os/signal"
	"os/user"
	"time"

	"github.com/ghetzel/cli"
	"github.com/ghetzel/go-stockutil/executil"
	"github.com/ghetzel/go-stockutil/log"
	"github.com/ghetzel/go-stockutil/pathutil"
	"github.com/ghetzel/procwatch"
	"github.com/ghetzel/procwatch/client"
)

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
			Value:  `notice`,
			EnvVar: `LOGLEVEL`,
		},
		cli.StringFlag{
			Name:   `config, c`,
			Usage:  `The configuration file to load`,
			Value:  executil.RootOrString(`/etc/procwatch.ini`, `~/.config/procwatch/procwatch.ini`),
			EnvVar: `PROCWATCH_CONFIG`,
		},
		cli.DurationFlag{
			Name:  `max-stop-timeout`,
			Usage: `The maximum amount of time to wait for programs to gracefully stop when stopping the manager before killing them.`,
			Value: (120 * time.Second),
		},
		cli.StringFlag{
			Name:   `client-address, a`,
			Usage:  `The address to connect to for client operations`,
			EnvVar: client.DefaultClientAddress,
		},
		cli.BoolFlag{
			Name:  `dashboard, D`,
			Usage: `Show a CLI dashboard.`,
		},
	}

	app.Action = func(c *cli.Context) {
		var configFile string

		if v := c.String(`config`); v != `` {
			configFile = v
		} else if u, err := user.Current(); err == nil && u.Uid == `0` {
			configFile = `/etc/procwatch/config.ini`
		} else if v, err := pathutil.ExpandUser(`~/.config/procwatch/config.ini`); err == nil {
			configFile = v
		} else {
			log.Fatalf("failed to determine config path: %v", err)
			return
		}

		if c.Bool(`dashboard`) {
			if dashboard, err := NewDashboard(c.String(`client-address`)); err == nil {
				log.SetOutput(io.Discard)
				log.FatalIf(dashboard.Run())
			} else {
				log.FatalIf(err)
			}
		} else {
			var manager = procwatch.NewManagerFromConfig(configFile)
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
						log.Debugf("main: Stop completed with exit code %d", code)
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
				go manager.Run()
				manager.Wait()
			} else {
				log.Fatal(err)
			}
		}
	}

	app.Run(os.Args)
}
