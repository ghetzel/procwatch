package main

import (
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/ghetzel/go-stockutil/typeutil"
	"github.com/ghetzel/procwatch"
	"github.com/rivo/tview"
)

type ServicesDashboardPage struct {
	dash  *Dashboard
	id    string
	table *tview.Table
}

func NewServicesDashboardPage(id string, dash *Dashboard) *ServicesDashboardPage {
	return &ServicesDashboardPage{
		id:   id,
		dash: dash,
	}
}

func (self *ServicesDashboardPage) getSelectedProgram() (*procwatch.Program, bool) {
	var row, _ = self.table.GetSelection()

	if namecell := self.table.GetCell(row, 0); namecell != nil {
		return self.dash.manager.Program(namecell.Text)
	} else {
		return nil, false
	}
}

func (self *ServicesDashboardPage) HandleKeyEvent(event *tcell.EventKey) *tcell.EventKey {
	switch event.Rune() {
	// case 'l':
	// log view
	case 'R':
		if program, ok := self.getSelectedProgram(); ok {
			go program.Restart()
		}
	}

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
	self.table.SetWrapSelection(true, false)
	self.table.SetFixed(1, 0)
	self.table.Clear()

	var rpad int = 2
	var maxNameLen int = 12
	var maxScheduleLen int = 9

	for _, program := range self.dash.manager.Programs() {
		if l := len(program.Name); l > maxNameLen {
			maxNameLen = l
		}
		if l := len(program.Schedule); l > maxScheduleLen {
			maxScheduleLen = l
		}
	}

	var fmtName = "%- " + typeutil.String(maxNameLen+rpad) + "s"
	var fmtSchd = "%- " + typeutil.String(maxScheduleLen+rpad) + "s"

	for i, label := range []string{
		`PROGRAM NAME`,
		`STATE`,
		`SCHEDULE`,
		`NEXT RUN`,
		`LAST OUTPUT`,
	} {
		var cell = tview.NewTableCell(label)
		cell.SetSelectable(false)
		cell.SetTextColor(tcell.ColorBlue)
		self.table.SetCell(0, i, cell)
	}

	for row, program := range self.dash.manager.Programs() {
		var cells = make([]*tview.TableCell, 5)
		//
		// ---------------------------------------------------------------------
		cells[0] = tview.NewTableCell(fmt.Sprintf(fmtName, program.Name))
		cells[0].SetMaxWidth(maxNameLen + rpad)
		//
		// ---------------------------------------------------------------------
		cells[1] = tview.NewTableCell(self.taggedProgramState(program.State))
		cells[1].SetMaxWidth(10)
		//
		// ---------------------------------------------------------------------
		cells[2] = tview.NewTableCell(fmt.Sprintf(fmtSchd, program.Schedule))
		cells[2].SetMaxWidth(maxScheduleLen + rpad)
		var nextstr string
		if next := program.NextScheduledAt; !next.IsZero() {
			var until = next.Sub(time.Now()).Round(time.Second)

			if until < 0 {
				until = 0
			}

			nextstr = until.String()
		} else {
			nextstr = `-`
		}
		cells[3] = tview.NewTableCell(fmt.Sprintf("%- 10s", nextstr))
		cells[3].SetMaxWidth(10)
		//
		// ---------------------------------------------------------------------
		cells[4] = tview.NewTableCell(program.String())
		cells[4].SetExpansion(1)

		for col, cell := range cells {
			self.table.SetCell(row+1, col, cell)
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
