package procwatch

import (
	"fmt"
	"strings"
	"time"

	"github.com/ghetzel/go-stockutil/sliceutil"
)

type EventSource int

const (
	ProgramSource EventSource = iota
)

func (src EventSource) String() string {
	switch src {
	case ProgramSource:
		return `Program`
	default:
		return `Anonymous`
	}
}

type Event struct {
	Names      []string
	Label      string
	Timestamp  time.Time
	Error      error
	Arguments  []string
	SourceType EventSource
	Source     any
}

func NewEvent(names []string, label string, sourceType EventSource, source any, args ...string) *Event {
	return &Event{
		Names:      names,
		Label:      label,
		Timestamp:  time.Now(),
		Arguments:  args,
		SourceType: sourceType,
		Source:     source,
	}
}

func (event *Event) String() string {
	return fmt.Sprintf("[%s] %s",
		event.Label,
		strings.Join(event.Names, `,`))
}

func (event *Event) HasName(name string) bool {
	return sliceutil.ContainsString(event.Names, name)
}
