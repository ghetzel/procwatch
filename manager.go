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
	ConfigFile          string
	Events              chan *Event
	programs            map[string]*Program
	stopping            bool
	lastError           error
	eventHandlerRunning bool
}

func NewManager(configFile string) *Manager {
	return &Manager{
		ConfigFile: configFile,
		programs:   make(map[string]*Program),
		Events:     make(chan *Event),
	}
}

func (self *Manager) Initialize() error {
	if data, err := ioutil.ReadFile(self.ConfigFile); err == nil {
		if loaded, err := LoadProgramsFromConfig(data, self); err == nil {
			for name, program := range loaded {
				if _, ok := self.programs[name]; ok {
					return fmt.Errorf("Cannot load program %d from file %s: a program named '%s' was already loaded.",
						program.LoadIndex, self.ConfigFile, name)
				}

				self.programs[name] = program
			}
		}
	} else {
		return err
	}

	return nil
}

func (self *Manager) Run() {
	self.stopping = false
	go self.startEventLogger()

	for {
		var checkLock sync.WaitGroup

		for _, program := range self.programs {
			checkLock.Add(1)
			go self.checkProgramState(program, &checkLock)
		}

		// wait for all program checks to be complete for this iteration
		checkLock.Wait()

		// if we're stopping the manager, and if all the programs are in a terminal state, quit the loop
		if self.stopping {
			if len(self.GetProgramsByState(ProgramStopped, ProgramExited, ProgramFatal)) == len(self.programs) {
				return
			}
		}

		time.Sleep(500 * time.Millisecond)
	}
}

func (self *Manager) Stop() {
	for _, program := range self.programs {
		program.Stop()
	}

	self.stopping = true
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

func (self *Manager) Program(name string) (*Program, bool) {
	program, ok := self.programs[name]
	return program, ok
}

func (self *Manager) GetProgramsByState(states ...ProgramState) []*Program {
	programs := make([]*Program, 0)

	for _, program := range self.programs {
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
	if self.eventHandlerRunning {
		return
	}

	self.eventHandlerRunning = true

	for {
		if self.stopping {
			self.eventHandlerRunning = false
			break
		}

		select {
		case event := <-self.Events:
			log.Debug(event.String())

			if event.Error != nil {
				log.Error(event.Error)
			}
		}
	}
}
