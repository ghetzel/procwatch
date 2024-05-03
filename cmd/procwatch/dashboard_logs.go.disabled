package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type LogsDashboardPage struct {
	dash                  *Dashboard
	logs                  *tview.List
	ctx                   context.Context
	cancel                context.CancelFunc
	init                  bool
	initlock              sync.Mutex
	listlock              sync.Mutex
	autoscroll            bool
	status                *tview.Table
	layout                *tview.Flex
	lastLogItemReceivedAt time.Time
}

func NewLogsDashboardPage(dash *Dashboard) *LogsDashboardPage {
	var ctx, cancel = context.WithCancel(context.Background())

	return &LogsDashboardPage{
		dash:       dash,
		layout:     tview.NewFlex(),
		logs:       tview.NewList(),
		status:     tview.NewTable(),
		ctx:        ctx,
		cancel:     cancel,
		autoscroll: true,
	}
}

func (self *LogsDashboardPage) GetToggleStates() []toggle {
	return []toggle{
		{
			Label:    `Autoscroll`,
			Shortcut: 's',
			On:       self.autoscroll,
		},
	}
}

func (self *LogsDashboardPage) Color() tcell.Color {
	return tcell.ColorOrange
}

func (self *LogsDashboardPage) String() string {
	return `logs`
}

func (self *LogsDashboardPage) Update() error {
	if !self.init {
		self.initlock.Lock()
		self.logs.SetHighlightFullLine(true)
		self.logs.ShowSecondaryText(false)
		self.logs.SetSelectedTextColor(tcell.ColorWhite)
		self.logs.SetSelectedBackgroundColor(tcell.Color238)

		go func() {
			var lastfile string

			for line := range self.dash.manager.Tail(self.ctx) {
				self.listlock.Lock()

				if lastfile != line.Filename {
					lastfile = line.Filename
					self.logs.AddItem(fmt.Sprintf("==> %s <==", lastfile), ``, 0, nil)
				}

				self.logs.AddItem(line.Text, ``, 0, nil)
				self.lastLogItemReceivedAt = time.Now()
				self.listlock.Unlock()

				if self.autoscroll {
					self.autoscrollToEnd()
				}
			}
		}()

		self.layout.AddItem(self.logs, 0, 1, true)
		self.layout.AddItem(self.status, 1, 0, false)
		self.layout.SetDirection(tview.FlexRow)
		self.init = true
		self.initlock.Unlock()
	}

	self.updateStatus()

	return nil
}

func (self *LogsDashboardPage) updateStatus() {
	var since = time.Since(self.lastLogItemReceivedAt).Round(time.Second)
	self.status.SetCell(0, 1, tview.NewTableCell("").SetExpansion(1))

	if since >= 5*time.Second {
		self.status.SetCellSimple(0, 2, fmt.Sprintf("[#999999]last record: [#dddddd]%v[-]", since))
	} else {
		self.status.SetCellSimple(0, 2, ``)
	}
}

func (self *LogsDashboardPage) autoscrollToEnd() {
	self.listlock.Lock()
	self.logs.SetCurrentItem(-1)
	self.listlock.Unlock()
}

func (self *LogsDashboardPage) lastItemSelected() bool {
	self.listlock.Lock()
	defer self.listlock.Unlock()
	return self.logs.GetCurrentItem() == (self.logs.GetItemCount() - 1)
}

func (self *LogsDashboardPage) RootElement() tview.Primitive {
	return self.layout
}

func (self *LogsDashboardPage) HandleKeyEvent(event *tcell.EventKey) *tcell.EventKey {
	switch event.Rune() {
	case 's':
		self.autoscroll = !self.autoscroll
		self.autoscrollToEnd()
		return nil
	}

	switch event.Key() {
	case tcell.KeyPgUp, tcell.KeyPgDn, tcell.KeyUp, tcell.KeyDown:
		self.autoscroll = false
	}

	return event
}
