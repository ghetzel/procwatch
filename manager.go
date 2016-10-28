package procwatch

import (
	"fmt"
	"github.com/op/go-logging"
	"io/ioutil"
	"sync"
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
		var checkLock sync.WaitGroup

		for _, program := range self.Programs {
			checkLock.Add(1)
			go self.checkProgramState(program, &checkLock)
		}

		// wait for all program checks to be complete for this iteration
		checkLock.Wait()

		time.Sleep(500 * time.Millisecond)
	}
}

// Process Management States
//
// STOPPED -> STARTING
//          |- up for startsecs? -> RUNNING
//          |                       |- manually stopped? -> STOPPING
//          |                                               |- stopped in time? -> [STOPPED]
//          |                                               \- no?              -> [FATAL]
//          |                       \- process exited?   -> EXITED -> STARTING...
//          |
//          |- no?
//          |   \- should restart? -> BACKOFF -> STARTING...
//          |                      -> [FATAL]
//          |
//          \- manually stopped?   -> STOPPING
//                                    |- stopped in time? -> [STOPPED]
//                                    \- no?              -> [FATAL]
//
func (self *Manager) checkProgramState(program *Program, checkLock *sync.WaitGroup) {
	switch program.State {
	case ProgramStopped:
		// first-time start for autostart programs
		if program.AutoStart && !program.HasEverBeenStarted() {
			program.Start()
		}

	case ProgramExited:
		// automatic restart of cleanly-exited programs
		if program.ShouldAutoRestart() {
			program.Start()
		}

	case ProgramBackoff:
		if program.ShouldAutoRestart() {
			program.Start()
		} else {
			program.StopFatal()
		}
	}

	checkLock.Done()
}

func (self *Manager) GetProgramsByState(states ...ProgramState) []*Program {
	programs := make([]*Program, 0)

	for _, program := range self.Programs {
		for _, state := range states {
			if program.State == state {
				programs = append(programs, program)
			}
		}
	}

	return programs
}

func (self *Manager) pushEvent(names []string, sourceType EventSource, source interface{}, args ...string) {
	self.Events <- NewEvent(names, `MANAGER`, sourceType, source, args...)
}

func (self *Manager) pushProcessStateEvent(state ProgramState, source *Program, err error, args ...string) {
	event := NewEvent([]string{
		`PROCESS_STATE`,
		fmt.Sprintf("PROCESS_STATE_%s", state.String()),
	}, source.Name, ProgramSource, source, args...)

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
