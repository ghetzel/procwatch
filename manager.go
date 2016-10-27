package procwatch

import (
	"fmt"
	"github.com/op/go-logging"
	"io/ioutil"
	"time"
)

var log = logging.MustGetLogger(`procwatch`)

type Manager struct {
	ConfigFile string
	Programs   map[string]*Program
	Events     chan *Event
}

func NewManager(configFile string) *Manager {
	return &Manager{
		ConfigFile: configFile,
		Programs:   make(map[string]*Program),
		Events:     make(chan *Event),
	}
}

func (self *Manager) Initialize() error {
	if data, err := ioutil.ReadFile(self.ConfigFile); err == nil {
		if loaded, err := LoadProgramsFromConfig(data, self); err == nil {
			for name, program := range loaded {
				if _, ok := self.Programs[name]; ok {
					return fmt.Errorf("Cannot load program %d from file %s: a program named '%s' was already loaded.",
						program.LoadIndex, self.ConfigFile, name)
				}

				self.Programs[name] = program
			}
		}
	} else {
		return err
	}

	return nil
}

func (self *Manager) Run() error {
	go self.startEventLogger()

	for {
		// start processes in the STOPPED state
		if err := self.performAutomaticStarts(); err != nil {
			return err
		}

		time.Sleep(500 * time.Millisecond)
	}
}

func (self *Manager) performAutomaticStarts() error {
	for _, program := range self.GetProgramsByState(ProgramStopped) {
		if program.AutoStart {
			program.Start(false)
		}
	}

	return nil
}

func (self *Manager) GetProgramsByState(state ProgramState) []*Program {
	programs := make([]*Program, 0)

	for _, program := range self.Programs {
		if program.State == state {
			programs = append(programs, program)
		}
	}

	return programs
}

func (self *Manager) pushEvent(names []string, sourceType EventSource, source interface{}, args ...string) {
	self.Events <- NewEvent(names, sourceType, source, args...)
}

func (self *Manager) pushProcessStateEvent(state ProgramState, source interface{}, err error, args ...string) {
	event := NewEvent([]string{
		`PROCESS_STATE`,
		fmt.Sprintf("PROCESS_STATE_%s", state.String()),
	}, ProgramSource, source, args...)

	event.Error = err
	self.Events <- event
}

func (self *Manager) startEventLogger() {
	for {
		select {
		case event := <-self.Events:
			log.Debug(event.String())

			if event.Error != nil {
				log.Error(event.Error)
			}
		}
	}
}
