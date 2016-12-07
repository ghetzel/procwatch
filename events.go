package procwatch

import (
	"fmt"
	"strings"
	"github.com/ghetzel/go-stockutil/sliceutil"
	"time"
)

type EventSource int

const (
	ProgramSource EventSource = iota
)

func (self EventSource) String() string {
	switch self {
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
	Source     interface{}
}

func NewEvent(names []string, label string, sourceType EventSource, source interface{}, args ...string) *Event {
	return &Event{
		Names:      names,
		Label:      label,
		Timestamp:  time.Now(),
		Arguments:  args,
		SourceType: sourceType,
		Source:     source,
	}
}

func (self *Event) String() string {
	return fmt.Sprintf("[%s] %s",
		self.Label,
		strings.Join(self.Names, `,`))
}

func (self *Event) HasName(name string) bool {
	return sliceutil.ContainsString(self.Names, name)
}

func (self *Event) sourceDetail() string {
	var detail string

	if self.Source != nil {
		detail = fmt.Sprintf(": %T", self.Source)

		if len(self.Arguments) > 0 {
			detail = fmt.Sprintf("%s<%s>",
				detail,
				strings.Join(self.Arguments, `, `))
		}
	}

	return detail
}
