package procwatch

import (
	"fmt"
	"github.com/go-ini/ini"
	"github.com/sasha-s/go-deadlock"
	"io/ioutil"
	"sync"
	"time"
)

type EventHandler func(*Event)

type Manager struct {
	ConfigFile          string
	Events              chan *Event `json:"-"`
	Server              *Server
	eventHandlers       []EventHandler
	programs            []*Program
	stopping            bool
	doneStopping        chan error
	lastError           error
	eventHandlerRunning bool
	stopLock            deadlock.Mutex
}

func NewManager(configFile string) *Manager {
	return &Manager{
		ConfigFile:    configFile,
		Events:        make(chan *Event),
		programs:      make([]*Program, 0),
		eventHandlers: make([]EventHandler, 0),
		doneStopping:  make(chan error),
	}
}

func (self *Manager) Initialize() error {
	if data, err := ioutil.ReadFile(self.ConfigFile); err == nil {
		if err := LoadGlobalConfig(data, self); err != nil {
			return err
		}

		if loaded, err := LoadProgramsFromConfig(data, self); err == nil {
			for _, program := range loaded {
				self.programs = append(self.programs, program)
			}
		}

		if self.Server != nil {
			if err := self.Server.Initialize(self); err == nil {
				go self.Server.Start()
			} else {
				return err
			}
		}
	} else {
		return err
	}

	return nil
}

func (self *Manager) Run() {
	self.stopLock.Lock()
	self.stopping = false
	self.stopLock.Unlock()

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
		self.stopLock.Lock()
		isStopping := self.stopping
		self.stopLock.Unlock()

		if isStopping {
			shouldBreak := true

			for _, p := range self.programs {
				if !p.InTerminalState() {
					shouldBreak = false
					break
				}
			}

			// break out of the mainloop, which will send the terminate signal
			if shouldBreak {
				break
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	log.Infof("All programs stopped, stopping manager...")
	self.doneStopping <- nil
}

func (self *Manager) Stop() error {
	for _, program := range self.programs {
		if !program.InTerminalState() {
			program.Stop()
		}
	}

	self.stopLock.Lock()
	self.stopping = true
	self.stopLock.Unlock()

	select {
	case err := <-self.doneStopping:
		return err
	}

	return fmt.Errorf("Manager stopped prematurely")
}

func (self *Manager) AddEventHandler(handler EventHandler) {
	self.eventHandlers = append(self.eventHandlers, handler)
}

// Process Management States
//
// STOPPED -> STARTING
//          |- up for startsecs? -> RUNNING
//          |                       |- manually stopped? -> STOPPING
//          |                       |                       |- stopped in time? -> [STOPPED]
//          |                       |                       \- no?              -> [FATAL]
//          |                       \- process exited?   -> EXITED -> STARTING...
//          |
//          |- no?
//          |  \- should restart? -> BACKOFF -> STARTING...
//          |                     -> [FATAL]
//          |
//          \- manually stopped?  -> STOPPING
//                                   |- stopped in time? -> [STOPPED]
//                                   \- no?              -> [FATAL]
//
func (self *Manager) checkProgramState(program *Program, checkLock *sync.WaitGroup) {
	switch program.GetState() {
	case ProgramStopped:
		// first-time start for autostart programs
		if program.AutoStart && !program.HasEverBeenStarted() {
			log.Debugf("[%s] Starting program for the first time", program.Name)
			program.Start()
		}

	case ProgramExited:
		// automatic restart of cleanly-exited programs
		if program.ShouldAutoRestart() {
			log.Debugf("[%s] Automatically restarting cleanly-exited program", program.Name)
			program.Start()
		}

	case ProgramBackoff:
		if program.ShouldAutoRestart() {
			log.Debugf("[%s] Automatically restarting program after backoff (retry %d/%d)",
				program.Name,
				program.processRetryCount,
				program.StartRetries)
			program.Start()
		} else {
			log.Debugf("[%s] Marking program fatal after %d/%d retries",
				program.Name,
				program.processRetryCount,
				program.StartRetries)
			program.StopFatal()
		}
	}

	checkLock.Done()
}

func (self *Manager) Programs() []*Program {
	return self.programs
}

func (self *Manager) Program(name string) (*Program, bool) {
	for _, program := range self.programs {
		if program.Name == name {
			return program, true
		}
	}

	return nil, false
}

func (self *Manager) GetProgramsByState(states ...ProgramState) []*Program {
	programs := make([]*Program, 0)

	for _, program := range self.programs {
		currentState := program.GetState()

		for _, state := range states {
			if currentState == state {
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
		fmt.Sprintf("PROCESS_STATE_%v", state),
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
		self.stopLock.Lock()
		isStopping := self.stopping
		self.stopLock.Unlock()

		if isStopping {
			self.eventHandlerRunning = false
			break
		}

		select {
		case event := <-self.Events:
			log.Debug(event.String())

			if event.Error != nil {
				log.Error(event.Error)
			}

			// dispatch event to all registered handlers
			for _, handler := range self.eventHandlers {
				handler(event)
			}
		}
	}
}

func LoadGlobalConfig(data []byte, manager *Manager) error {
	if iniFile, err := ini.Load(data); err == nil {
		for _, section := range iniFile.Sections() {
			switch section.Name() {
			case `server`:
				if key := section.Key(`enabled`); key != nil && key.MustBool(false) {
					manager.Server = new(Server)

					if err := section.MapTo(manager.Server); err != nil {
						return err
					}
				}
			}
		}
	} else {
		return err
	}

	return nil
}
