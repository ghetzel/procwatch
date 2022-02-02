package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/gdamore/tcell/v2"
	"github.com/ghetzel/go-stockutil/convutil"
	"github.com/ghetzel/go-stockutil/log"
	"github.com/ghetzel/go-stockutil/maputil"
	"github.com/ghetzel/procwatch"
	"github.com/ghetzel/sysfact"
	logging "github.com/op/go-logging"
	"github.com/rivo/tview"
)

var spinner = []rune{'○', '◔', '◑', '◕', '●'}

// var spinner = []rune{'◐', '◑'}

type toggle struct {
	Label    string
	Shortcut rune
	On       bool
}

type dashboardPage interface {
	String() string
	Update() error
	GetToggleStates() []toggle
	Color() tcell.Color
	RootElement() tview.Primitive
	HandleKeyEvent(event *tcell.EventKey) *tcell.EventKey
}

type Dashboard struct {
	gui               *tview.Application
	manager           *procwatch.Manager
	paused            bool
	logHeight         int
	lines             []string
	lineno            int
	level             logging.Level
	widestStatusYet   int
	lineBuffer        *bytes.Buffer
	guictx            context.Context
	targetFPS         int
	curpage           string
	curpageindex      int
	lastpage          string
	framelock         sync.Mutex
	frame             int64
	hideHeader        bool
	pagelist          []dashboardPage
	rootLayout        *tview.Flex
	header            *tview.Frame
	pages             *tview.Pages
	navbar            *tview.Table
	sysinfo           *sysfact.Reporter
	sysinfoInterval   time.Duration
	sysinfoLastReport time.Time
	sysinfoData       *maputil.Map
}

func NewDashboard(manager *procwatch.Manager) *Dashboard {
	return &Dashboard{
		manager:         manager,
		lines:           make([]string, 0),
		lineBuffer:      bytes.NewBuffer(nil),
		guictx:          context.Background(),
		targetFPS:       8,
		sysinfo:         sysfact.NewReporter(),
		sysinfoInterval: time.Second,
	}
}

func (self *Dashboard) Run() error {
	self.gui = tview.NewApplication()
	go self.startDrawUpdates()
	go self.scanAndEmitLogBuffer()
	self.init()

	if err := self.setupKeybindings(); err != nil {
		return err
	}

	return self.gui.Run()
}

func (self *Dashboard) Stop() {
	if manager := self.manager; manager != nil {
		manager.Stop(false)
	}

	if gui := self.gui; gui != nil {
		gui.Stop()
	}
}

func (self *Dashboard) init() {
	self.navbar = tview.NewTable()
	self.navbar.SetBorderPadding(0, 0, 1, 1)

	self.pages = tview.NewPages()
	self.curpageindex = -1

	self.pages.SetBorderPadding(0, 0, 1, 1)

	for i, page := range []dashboardPage{
		NewServicesDashboardPage(self),
		NewLogsDashboardPage(self),
	} {
		var id = page.String()

		self.pages.AddPage(id, page.RootElement(), true, false)
		self.pagelist = append(self.pagelist, page)

		var navitem = tview.NewTableCell(strings.ToUpper(id))
		navitem.SetExpansion(1)
		navitem.SetReference(page)
		navitem.SetAlign(tview.AlignCenter)
		self.navbar.SetCell(0, i, navitem)

		if self.curpage == `` {
			self.curpage = id
			self.navpage(true)
		}
	}

	var maxPageNameLen int

	for _, page := range self.pagelist {
		if l := len(page.String()); l > maxPageNameLen {
			maxPageNameLen = l
		}
	}

	for i := 0; i < self.navbar.GetColumnCount(); i++ {
		self.navbar.GetCell(0, i).SetMaxWidth(maxPageNameLen + 2)
	}

	self.pages.SetBorder(true)
	self.rootLayout = tview.NewFlex()
	self.rootLayout.SetDirection(tview.FlexRow)
	self.rootLayout.AddItem(self.pages, 0, 1, true)
	self.rootLayout.AddItem(self.navbar, 1, 0, false)

	self.header = tview.NewFrame(self.rootLayout)
	self.gui.SetRoot(self.header, true)
}

func (self *Dashboard) redraw() {
	self.framelock.Lock()
	self.frame += 1
	defer self.framelock.Unlock()

	self.gui.QueueUpdateDraw(func() {
		self.refreshSystemInfo()
		self.updateHeaderDetails()

		var page, changed = self.currentPage()
		if err := page.Update(); err != nil {
			log.Errorf("update failed: %v", err)
		}
		if changed {
			self.pages.SwitchToPage(page.String())
		}
	})
}

func (self *Dashboard) startDrawUpdates() {
	for {
		self.redraw()
		time.Sleep((1000 * time.Millisecond) / time.Duration(self.targetFPS))
	}
}

func (self *Dashboard) refreshSystemInfo() {
	if self.sysinfoData == nil || time.Since(self.sysinfoLastReport) >= self.sysinfoInterval {
		if report, err := self.sysinfo.Report(); err == nil {
			self.sysinfoData = maputil.M(report)
			self.sysinfoLastReport = time.Now()
		} else {
			log.Errorf("sysinfo: %v", err)
		}
	}
}

func (self *Dashboard) updateHeaderDetails() {
	if self.header == nil {
		return
	}

	var page, _ = self.currentPage()

	self.header.Clear()
	self.header.SetBorderPadding(0, 0, 1, 1)
	self.header.AddText(fmt.Sprintf("[#aaaaaa]proc[green::b]watch[-] [#444444]v%s[-]", procwatch.Version), true, tview.AlignLeft, tcell.ColorReset)

	if states := page.GetToggleStates(); len(states) > 0 {
		self.header.AddText("    [orange]Shortcuts[-]", true, tview.AlignLeft, tcell.ColorReset)

		for _, state := range states {
			var scol = `red`
			var sval = `OFF`

			if state.On {
				scol = `green`
				sval = `ON`
			}

			var txt = fmt.Sprintf("[#999999]([-][#dddddd]%c[-][#999999])[-] %- 12s [%s]%s[-]", state.Shortcut, state.Label, scol, sval)
			self.header.AddText(txt, true, tview.AlignLeft, tcell.ColorReset)
		}
	}

	if self.sysinfoData != nil {
		var memsz, memunit = convutil.Bytes(self.sysinfoData.Int(`memory.total`)).Auto()
		var memPctInt = int(math.RoundToEven(self.sysinfoData.Float(`memory.percent_used`) / 10.0))
		var cpuU = int(math.RoundToEven(self.sysinfoData.Float(`cpu.usage.user`) / 10.0))
		var cpuS = int(math.RoundToEven(self.sysinfoData.Float(`cpu.usage.system`) / 10.0))

		var info = []string{
			fmt.Sprintf(
				"[white]LOAD[-] [#bbbbbb]%.2f [#999999]%.2f [#777777]%.2f[-:-:-]",
				self.sysinfoData.Float(`system.load.last_1m`),
				self.sysinfoData.Float(`system.load.last_5m`),
				self.sysinfoData.Float(`system.load.last_15m`),
			),
			fmt.Sprintf(
				"[white]CPU[#999999]x%d[-] [blue:-:-]%s[blue::b]%s[#444444::d]%s[-:-:-]",
				self.sysinfoData.NInt(`cpu.count`),
				strings.Repeat("\u25a0", cpuU),
				strings.Repeat("\u25a0", cpuS),
				strings.Repeat("\u25a0", 10-cpuU-cpuS),
			),
			fmt.Sprintf(
				"[white]MEM[#999999] %d%s[-] [green::b]%s[#444444::d]%s[-:-:-]",
				int(memsz),
				string(memunit[0]),
				strings.Repeat("\u25a0", memPctInt),
				strings.Repeat("\u25a0", 10-memPctInt),
			),
		}

		self.header.AddText(strings.Join(info, `  `), true, tview.AlignRight, tcell.ColorReset)
	}
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
		defer self.updateHeaderDetails()

		switch event.Key() {
		case tcell.KeyTAB:
			self.navpage(true)
			return nil
		}

		switch event.Rune() {
		case 'q', 'Q':
			self.confirmExit()
			// self.Stop()
			return nil
		case 'h':
			self.hideHeader = !self.hideHeader
			return nil
		}

		var board, _ = self.currentPage()
		return board.HandleKeyEvent(event)
	})

	return nil
}

func (self *Dashboard) currentPage() (page dashboardPage, changed bool) {
	if i := self.curpageindex; i < len(self.pagelist) {
		page = self.pagelist[i]
		self.curpage = page.String()

		if self.curpage != self.lastpage {
			changed = true
			self.lastpage = self.curpage
		}
	}

	return
}

func (self *Dashboard) navpage(forward bool) error {
	var ni int

	if forward {
		ni = (self.curpageindex + 1)
	} else {
		ni = (self.curpageindex - 1)
	}

	ni = ni % len(self.pagelist)

	if ni < len(self.pagelist) {
		var page = self.pagelist[ni]

		self.pages.SwitchToPage(page.String())
		self.pages.SetBorderColor(page.Color())
		self.curpageindex = ni

		for i := 0; i < self.navbar.GetColumnCount(); i++ {
			var cell = self.navbar.GetCell(0, i)

			if i == ni {
				cell.SetTextColor(tcell.ColorBlack)
				cell.SetBackgroundColor(page.Color())
			} else {
				cell.SetTextColor(tcell.ColorReset)
				cell.SetBackgroundColor(tcell.ColorReset)
			}
		}
	}

	return nil
}

func (self *Dashboard) renderLog(w io.Writer) {
	for _, line := range self.lines {
		fmt.Fprintln(w, line)
	}
}

func (self *Dashboard) scanAndEmitLogBuffer() {
	for {
		var scanner = bufio.NewScanner(self.lineBuffer)

		for scanner.Scan() {
			if line := strings.TrimSpace(scanner.Text()); line != `` {
				self.lines = append(self.lines, line)
			}
		}

		time.Sleep(125 * time.Millisecond)
	}
}

func (self *Dashboard) Write(p []byte) (int, error) {
	return self.lineBuffer.Write(p)
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
