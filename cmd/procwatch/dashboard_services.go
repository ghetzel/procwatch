package main

import (
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/ghetzel/procwatch"
	"github.com/rivo/tview"
)

type ServicesDashboardPage struct {
	dash     *Dashboard
	id       string
	table    *tview.Table
	selected int
}

func NewServicesDashboardPage(id string, dash *Dashboard) *ServicesDashboardPage {
	return &ServicesDashboardPage{
		id:       id,
		dash:     dash,
		selected: 0,
	}
}

func (self *ServicesDashboardPage) HandleKeyEvent(event *tcell.EventKey) *tcell.EventKey {
	// switch event.Rune() {
	// case 'l':
	// 	// log view
	// }

	return event
}

func (self *ServicesDashboardPage) String() string {
	return self.id
}

func (self *ServicesDashboardPage) Update() error {
	self.RootElement()
	// self.table.SetBorder(true)
	// self.table.SetBorderColor(tcell.ColorRed)
	self.table.SetSelectable(true, false)
	self.table.Clear()

	var maxNameLen int

	for _, program := range self.dash.manager.Programs() {
		if l := len(program.Name); l > maxNameLen {
			maxNameLen = l
		}
	}

	for row, program := range self.dash.manager.Programs() {
		var cells = make([]*tview.TableCell, 4)
		//
		// ---------------------------------------------------------------------
		cells[0] = tview.NewTableCell(program.Name)
		cells[0].SetMaxWidth(maxNameLen + 2)
		//
		// ---------------------------------------------------------------------
		cells[1] = tview.NewTableCell(self.taggedProgramState(program.State))
		//
		// ---------------------------------------------------------------------
		var nextstr string
		if next := program.NextScheduledAt; !next.IsZero() {
			nextstr = next.Format(time.RFC3339)
		} else {
			nextstr = `-`
		}
		cells[2] = tview.NewTableCell(fmt.Sprintf("%- 28s", nextstr))
		//
		// ---------------------------------------------------------------------
		cells[3] = tview.NewTableCell(program.String())
		cells[3].SetExpansion(1)

		for col, cell := range cells {
			self.table.SetCell(row, col, cell)
		}
	}

	return nil
}

func (self *ServicesDashboardPage) RootElement() tview.Primitive {
	if self.table == nil {
		self.table = tview.NewTable()
	}

	var flex = tview.NewFlex()

	flex.AddItem(self.table, 0, 1, true)

	return flex
}

func (self *ServicesDashboardPage) taggedProgramState(state procwatch.ProgramState) string {
	switch state {
	case procwatch.ProgramRunning:
		return fmt.Sprintf("[green::b]%-10s", state)
	case procwatch.ProgramStarting:
		return fmt.Sprintf("[blue::b]%-10s", state)
	case procwatch.ProgramStopped, procwatch.ProgramExited:
		return fmt.Sprintf("[white::b]%-10s", state)
	default:
		return fmt.Sprintf("[red::b]%-10s", state)
	}
}
