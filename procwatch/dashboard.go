package main

import (
	"fmt"
	"io"
	"time"

	tabwriter "github.com/NonerKao/color-aware-tabwriter"
	"github.com/fatih/color"
	"github.com/ghetzel/procwatch"
	"github.com/jroimartin/gocui"
)

type Dashboard struct {
	gui     *gocui.Gui
	manager *procwatch.Manager
	paused  bool
}

func NewDashboard(manager *procwatch.Manager) *Dashboard {
	return &Dashboard{
		manager: manager,
	}
}

func (self *Dashboard) Run() error {
	if g, err := gocui.NewGui(gocui.OutputNormal); err == nil {
		self.gui = g
		defer self.gui.Close()
		go self.startDrawUpdates()
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

	if _, err := g.SetView(`status`, 0, 0, maxX-1, maxY-1); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
	}

	return nil
}

func (self *Dashboard) quit(_ *gocui.Gui, _ *gocui.View) error {
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
		return table.Flush()
	} else {
		return err
	}

	return nil
}

func (self *Dashboard) renderStatus(w io.Writer) {
	fmt.Fprintf(w, "PROGRAM\tSTATE\tSTATUS\n")

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

		stateStr := c.Sprintf("%v", state)

		fmt.Fprintf(w, "%s\t%v\t%v\n", program.Name, stateStr, program)
	}
}
