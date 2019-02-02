package main

import (
	"fmt"
	"io"
	"time"

	tabwriter "github.com/NonerKao/color-aware-tabwriter"
	"github.com/fatih/color"
	"github.com/ghetzel/procwatch"
	"github.com/jroimartin/gocui"
	logging "github.com/op/go-logging"
)

type Dashboard struct {
	gui       *gocui.Gui
	manager   *procwatch.Manager
	paused    bool
	logHeight int
	lines     []string
	lineno    int
	level     logging.Level
}

func NewDashboard(manager *procwatch.Manager) *Dashboard {
	return &Dashboard{
		manager: manager,
		lines:   make([]string, 0),
	}
}

func (self *Dashboard) Run() error {
	if g, err := gocui.NewGui(gocui.OutputNormal); err == nil {
		self.gui = g
		defer self.gui.Close()
		go self.startDrawUpdates()

		self.manager.AddEventHandler(func(_ *procwatch.Event) {
			self.gui.Update(self.render)
		})
	} else {
		return err
	}

	self.gui.SetManagerFunc(self.layout)

	if err := self.setupKeybindings(); err != nil {
		return err
	}

	if err := self.gui.MainLoop(); err != nil && err != gocui.ErrQuit {
		return err
	}

	return nil
}

func (self *Dashboard) setupKeybindings() error {
	if err := self.gui.SetKeybinding(``, 'q', gocui.ModNone, self.quit); err != nil {
		return err
	}

	return nil
}

func (self *Dashboard) layout(g *gocui.Gui) error {
	maxX, maxY := g.Size()
	self.logHeight = int(maxY/2.0) - 1

	if _, err := g.SetView(`status`, 0, 0, maxX-1, maxY-self.logHeight-2); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
	}

	if _, err := g.SetView(`log`, 0, self.logHeight+1, maxX-1, maxY-1); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
	}

	return nil
}

func (self *Dashboard) quit(_ *gocui.Gui, _ *gocui.View) error {
	self.manager.Stop(false)
	return gocui.ErrQuit
}

func (self *Dashboard) startDrawUpdates() {
	for {
		self.gui.Update(self.render)
		time.Sleep(time.Second)
	}
}

func (self *Dashboard) render(g *gocui.Gui) error {
	if self.paused {
		return nil
	}

	// nop := color.New(color.Reset)

	if v, err := g.View(`status`); err == nil {
		v.Frame = true
		v.Title = `Process Status`

		table := tabwriter.NewWriter(v, 5, 2, 1, ' ', tabwriter.Debug)
		self.renderStatus(table)

		v.Clear()
		table.Flush()
	} else {
		return err
	}

	if v, err := g.View(`log`); err == nil {
		v.Frame = true
		v.Title = `Log Output`

		// table := tabwriter.NewWriter(v, 5, 2, 1, ' ', tabwriter.Debug)
		v.Clear()

		self.renderLog(v)

		// table.Flush()
	} else {
		return err
	}

	if _, err := g.SetCurrentView(`log`); err != nil {
		return err
	}

	return nil
}

func (self *Dashboard) renderStatus(w io.Writer) {
	fmt.Fprintf(w, "PROGRAM\tSTATE   \tSTATUS\n")

	for _, program := range self.manager.Programs() {
		state := program.State
		var c *color.Color

		switch state {
		case procwatch.ProgramRunning:
			c = color.New(color.FgGreen, color.Bold)
		case procwatch.ProgramStarting:
			c = color.New(color.FgBlue, color.Bold)
		case procwatch.ProgramStopped, procwatch.ProgramExited:
			c = color.New(color.FgWhite, color.Bold)
		default:
			c = color.New(color.FgRed, color.Bold)
		}

		stateStr := c.Sprintf("%- 8s", state)

		fmt.Fprintf(w, "%s\t%v\t%v\n", program.Name, stateStr, program)
	}
}

func (self *Dashboard) renderLog(w io.Writer) {
	for _, line := range self.lines {
		fmt.Fprintln(w, line)
	}
}

func (self *Dashboard) GetLevel(module string) logging.Level {
	return self.level
}

func (self *Dashboard) SetLevel(lvl logging.Level, module string) {
	self.level = lvl
}

func (self *Dashboard) IsEnabledFor(lvl logging.Level, module string) bool {
	return true
}

func (self *Dashboard) Log(lvl logging.Level, depth int, record *logging.Record) error {
	if self.logHeight > 0 && record != nil {
		var c *color.Color
		var level string

		switch lvl {
		case logging.NOTICE:
			c = color.New(color.FgGreen, color.Bold)
			level = `NN`
		case logging.DEBUG:
			c = color.New(color.FgBlue, color.Bold)
			level = `DD`
		case logging.INFO:
			c = color.New(color.FgWhite, color.Bold)
			level = `II`
		case logging.WARNING:
			c = color.New(color.FgYellow, color.Bold)
			level = `WW`
		case logging.ERROR, logging.CRITICAL:
			c = color.New(color.FgRed, color.Bold)
			level = `EE`
		default:
			c = color.New(color.FgCyan, color.Bold)
			level = `??`
		}

		level = c.Sprintf("%s", level)

		self.lines = append(
			self.lines,
			fmt.Sprintf("% 4d %s %s", self.lineno, level, record.Message()),
		)

		self.lineno += 1

		if len(self.lines) > self.logHeight {
			self.lines = self.lines[len(self.lines)-self.logHeight:]
		}
	} else {
		self.lines = nil
	}

	return nil
}
