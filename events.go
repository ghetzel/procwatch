package procwatch

import (
	"fmt"
	"strings"
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
	Timestamp  time.Time
	Error      error
	Arguments  []string
	SourceType EventSource
	Source     interface{}
}

func NewEvent(names []string, sourceType EventSource, source interface{}, args ...string) *Event {
	return &Event{
		Names:      names,
		Timestamp:  time.Now(),
		Arguments:  args,
		SourceType: sourceType,
		Source:     source,
	}
}

func (self *Event) String() string {
	return fmt.Sprintf("[%s] %s%s",
		self.SourceType.String(),
		strings.Join(self.Names, `,`),
		self.sourceDetail())
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
