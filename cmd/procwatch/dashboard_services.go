package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/ghetzel/go-stockutil/sliceutil"
	"github.com/ghetzel/go-stockutil/timeutil"
	"github.com/ghetzel/go-stockutil/typeutil"
	"github.com/ghetzel/procwatch"
	"github.com/rivo/tview"
)

type ServicesDashboardPage struct {
	dash  *Dashboard
	table *tview.Table
}

func NewServicesDashboardPage(dash *Dashboard) *ServicesDashboardPage {
	return &ServicesDashboardPage{
		dash:  dash,
		table: tview.NewTable(),
	}
}

func (self *ServicesDashboardPage) GetToggleStates() []toggle {
	return nil
}

func (self *ServicesDashboardPage) Color() tcell.Color {
	return tcell.ColorBlue
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
	if program, ok := self.getSelectedProgram(); ok {
		switch event.Rune() {
		// case 'l':
		// log view
		case 'R':
			go program.Restart()
		case 'K':
			go program.Stop()
		}
	}

	return event
}

func (self *ServicesDashboardPage) String() string {
	return `services`
}

func (self *ServicesDashboardPage) Update() error {
	self.table.SetSelectable(true, false)
	self.table.SetWrapSelection(true, false)
	self.table.SetFixed(1, 0)
	self.table.Clear()

	var selected, _ = self.table.GetSelection()
	selected -= 1
	var rpad int = 2
	var maxNameLen int = 12
	var maxScheduleLen int = 9
	var maxRemainLen int = 12

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
		var hilite = self.colorForProgramState(program.State)
		var nextstr string
		var cells = make([]*tview.TableCell, 5)
		//
		// ---------------------------------------------------------------------
		cells[0] = tview.NewTableCell(fmt.Sprintf(fmtName, program.Name))
		cells[0].SetMaxWidth(maxNameLen + rpad)
		//
		// ---------------------------------------------------------------------
		var statefmt = "[%s::]%- 10s"
		cells[1] = tview.NewTableCell(fmt.Sprintf(statefmt, hilite, program.State))
		cells[1].SetMaxWidth(10)
		//
		// ---------------------------------------------------------------------
		cells[2] = tview.NewTableCell(
			fmt.Sprintf(fmtSchd, typeutil.OrString(program.Schedule, `-`)),
		)
		cells[2].SetMaxWidth(maxScheduleLen + rpad)
		if next := program.NextScheduledAt; !next.IsZero() {
			var until = next.Sub(time.Now()).Round(time.Second)

			if until < 0 {
				until = 0
			}

			if until < time.Second {
				nextstr = `starting...`
			} else if until <= (24 * time.Hour) {
				nextstr = timeutil.FormatTimerf("%dh %dm %ds", until)

				var parts = strings.Split(nextstr, ` `)
				parts = sliceutil.MapString(parts, func(i int, value string) string {
					if len(value) == 2 && strings.HasPrefix(value, `0`) {
						return ``
					} else {
						return value
					}
				})

				nextstr = strings.Join(parts, ` `)
				nextstr = strings.TrimSpace(nextstr)
			} else if next.Second() > 0 {
				nextstr = next.Format("2-Jan-2006 15:04:05 -0700 MST")
			} else {
				nextstr = next.Format("2-Jan-2006 15:04 -0700 MST")
			}
		} else {
			nextstr = `-`
		}

		if l := len(nextstr); l > maxRemainLen {
			maxRemainLen = l
		}

		var fmtNext = "%- " + typeutil.String(maxRemainLen+rpad) + "s"

		cells[3] = tview.NewTableCell(fmt.Sprintf(fmtNext, nextstr))
		cells[3].SetMaxWidth(maxRemainLen + rpad)
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
	return self.table
}

func (self *ServicesDashboardPage) colorForProgramState(state procwatch.ProgramState) string {
	switch state {
	case procwatch.ProgramRunning:
		return "green"
	case procwatch.ProgramStarting:
		return "blue"
	case procwatch.ProgramStopped, procwatch.ProgramExited:
		return "white"
	default:
		return "red"
	}
}
