package main

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/gdamore/tcell/v2"
	"github.com/ghetzel/go-stockutil/log"
	"github.com/ghetzel/procwatch"
	logging "github.com/op/go-logging"
	"github.com/rivo/tview"
)

type dashboardPage interface {
	String() string
	Update() error
	RootElement() tview.Primitive
	HandleKeyEvent(event *tcell.EventKey) *tcell.EventKey
}

type Dashboard struct {
	gui             *tview.Application
	manager         *procwatch.Manager
	paused          bool
	logHeight       int
	lines           []string
	lineno          int
	level           logging.Level
	widestStatusYet int
	lineBuffer      *bytes.Buffer
	pages           *tview.Pages
	boards          map[string]dashboardPage
	guictx          context.Context
	targetFPS       int
	curpage         string
	header          *tview.Frame
	rootLayout      *tview.Flex
	lastpage        string
	framelock       sync.Mutex
	frame           int64
}

func NewDashboard(manager *procwatch.Manager) *Dashboard {
	return &Dashboard{
		manager:    manager,
		lines:      make([]string, 0),
		lineBuffer: bytes.NewBuffer(nil),
		boards:     make(map[string]dashboardPage),
		guictx:     context.Background(),
		targetFPS:  8,
	}
}

func (self *Dashboard) Run() error {
	self.gui = tview.NewApplication()
	go self.startDrawUpdates()
	self.guiSetup()

	if err := self.setupKeybindings(); err != nil {
		return err
	}

	return self.gui.Run()
}

func (self *Dashboard) currentPage() (board dashboardPage, changed bool) {
	if self.curpage != self.lastpage {
		changed = true
		self.lastpage = self.curpage
	}

	board = self.boards[self.curpage]
	return
}

func (self *Dashboard) guiSetup() {
	self.pages = tview.NewPages()
	self.pages.SetBorderColor(tcell.ColorBlue)
	self.pages.SetBorderPadding(0, 0, 1, 1)
	self.curpage = `services`

	self.boards[`services`] = NewServicesDashboardPage(`services`, self)

	for id, page := range self.boards {
		self.pages.AddPage(id, page.RootElement(), true, false)
	}

	self.pages.SetBorder(true)

	self.rootLayout = tview.NewFlex()
	self.rootLayout.SetDirection(tview.FlexRow)
	self.rootLayout.AddItem(self.pages, 0, 1, true)

	self.header = tview.NewFrame(self.rootLayout)
	self.gui.SetRoot(self.header, true)
}

func (self *Dashboard) redraw() {
	self.framelock.Lock()
	self.frame += 1
	defer self.framelock.Unlock()
	self.updateHeaderDetails()

	self.gui.QueueUpdateDraw(func() {
		var board, changed = self.currentPage()

		if err := board.Update(); err != nil {
			log.Errorf("update failed: %v", err)
		} else {
			// defer self.gui.Sync()
		}

		if changed {
			self.pages.SwitchToPage(board.String())
		}
	})
}

func (self *Dashboard) startDrawUpdates() {
	for {
		self.redraw()
		time.Sleep((1000 * time.Millisecond) / time.Duration(self.targetFPS))
	}
}

func (self *Dashboard) updateHeaderDetails() {
	if self.header == nil {
		return
	}

	self.header.Clear()
	self.header.AddText("LEFT 1", true, tview.AlignLeft, tcell.ColorOrange)
	self.header.AddText("LEFT 2", true, tview.AlignLeft, tcell.ColorOrange)
	self.header.AddText("LEFT 3", true, tview.AlignLeft, tcell.ColorOrange)
	self.header.AddText("LEFT 4", true, tview.AlignLeft, tcell.ColorOrange)

	self.header.AddText("CENTER 1", true, tview.AlignCenter, tcell.ColorOrange)
	self.header.AddText("CENTER 2", true, tview.AlignCenter, tcell.ColorOrange)
	self.header.AddText("CENTER 3", true, tview.AlignCenter, tcell.ColorOrange)
	self.header.AddText("CENTER 4", true, tview.AlignCenter, tcell.ColorOrange)

	self.header.AddText(fmt.Sprintf("[blue::d]\u25a0\u25a0\u25a0\u25a0\u25a0[blue::b]\u25a0\u25a0\u25a0\u25a0\u25a0[-]  [white]CPU x%- 5d", 8), true, tview.AlignRight, tcell.ColorOrange)
	self.header.AddText(fmt.Sprintf("[grey::d]\u25a0\u25a0\u25a0\u25a0\u25a0[red::b]\u25a0[green::b]\u25a0\u25a0\u25a0\u25a0[-]  [white]MEM @ 128G"), true, tview.AlignRight, tcell.ColorOrange)
	self.header.AddText("24.32 15.32 7.09 [white]LOAD 1-5-15", true, tview.AlignRight, tcell.ColorOrange)
	self.header.AddText(fmt.Sprintf("frame=%d", self.frame), true, tview.AlignRight, tcell.ColorOrange)
}

func (self *Dashboard) confirmExit() {
	var modal = tview.NewModal()

	modal.SetTitle("Confirm exit...")
	modal.SetText("Are you sure you want to stop all services and exit the program?")
	modal.SetBackgroundColor(tcell.ColorRed)
	modal.SetTextColor(tcell.ColorYellow)
	modal.SetButtonBackgroundColor(tcell.ColorRed)
	modal.SetButtonTextColor(tcell.ColorYellow)

	modal.AddButtons([]string{
		`Cancel`,
		`Stop & Exit`,
	})

	modal.SetDoneFunc(func(index int, label string) {
		self.gui.SetRoot(self.rootLayout, true)

		if index > 0 {
			self.Stop()
		}

	})

	self.gui.SetRoot(modal, true)
}

func (self *Dashboard) setupKeybindings() error {
	self.gui.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Rune() {
		case 'q', 'Q':
			self.confirmExit()
			return nil
		}

		var board, _ = self.currentPage()
		return board.HandleKeyEvent(event)
	})

	return nil
}

func (self *Dashboard) Stop() {
	if manager := self.manager; manager != nil {
		manager.Stop(false)
	}

	if gui := self.gui; gui != nil {
		gui.Stop()
	}
}

// func (self *Dashboard) renderLog(w io.Writer) {
// 	for _, line := range self.lines {
// 		fmt.Fprintln(w, line)
// 	}
// }

// func (self *Dashboard) scanAndEmitLogBuffer() {
// 	for {
// 		var scanner = bufio.NewScanner(self.lineBuffer)

// 		for scanner.Scan() {
// 			if line := strings.TrimSpace(scanner.Text()); line != `` {
// 				self.lines = append(self.lines, line)
// 			}
// 		}

// 		time.Sleep(125 * time.Millisecond)
// 	}
// }

// func (self *Dashboard) Write(p []byte) (int, error) {
// 	return self.lineBuffer.Write(p)
// }

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
