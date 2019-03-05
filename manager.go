package procwatch

import (
	"fmt"
	"io/ioutil"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/ghetzel/go-stockutil/convutil"
	"github.com/ghetzel/go-stockutil/log"
	"github.com/ghetzel/go-stockutil/mathutil"
	"github.com/ghetzel/go-stockutil/pathutil"
	"github.com/ghetzel/go-stockutil/sliceutil"
	"github.com/ghetzel/go-stockutil/structutil"
	"github.com/go-ini/ini"
	"github.com/natefinch/lumberjack"
)

var DefaultLogFileMaxBytes = 50 * convutil.Megabyte

type EventHandler func(*Event)

type Manager struct {
	ConfigFile            string
	LogFile               string      `json:"logfile"                 ini:"logfile"`
	LogFileMaxBytes       string      `json:"logfile_maxbytes"        ini:"logfile_maxbytes"`
	LogFileBackups        int         `json:"logfile_backups"         ini:"logfile_backups"`
	LogLevel              string      `json:"loglevel"                ini:"loglevel"`
	ChildLogDir           string      `json:"childlogdir"             ini:"childlogdir"`
	RedirectStderr        bool        `json:"redirect_stderr"         ini:"redirect_stderr,omitempty"`
	StdoutLogfileMaxBytes string      `json:"stdout_logfile_maxbytes" ini:"stdout_logfile_maxbytes"`
	StderrLogfileMaxBytes string      `json:"stderr_logfile_maxbytes" ini:"stderr_logfile_maxbytes"`
	StderrLogfileBackups  int         `json:"stderr_logfile_backups"  ini:"stderr_logfile_backups"`
	StdoutLogfileBackups  int         `json:"stdout_logfile_backups"  ini:"stdout_logfile_backups"`
	Events                chan *Event `json:"-"`
	Server                *Server
	includes              []string
	loadedConfigs         []string
	eventHandlers         []EventHandler
	programs              []*Program
	stopping              bool
	doneStopping          chan error
	lastError             error
	eventHandlerRunning   bool
	externalWaiters       chan bool
	intercept             string
	rollingLogger         *lumberjack.Logger
	logFileMaxBytes       uint64
}

func NewManager() *Manager {
	manager := &Manager{
		LogFileMaxBytes:       `50MB`,
		StdoutLogfileMaxBytes: `50MB`,
		StderrLogfileMaxBytes: `50MB`,
		StderrLogfileBackups:  10,
		StdoutLogfileBackups:  10,
		Events:                make(chan *Event),
		programs:              make([]*Program, 0),
		eventHandlers:         make([]EventHandler, 0),
		doneStopping:          make(chan error),
		includes:              make([]string, 0),
		loadedConfigs:         make([]string, 0),
		externalWaiters:       make(chan bool),
		Server: &Server{
			Address: DefaultAddress,
		},
	}

	// register the log intercept function for this manager
	manager.intercept = log.AddLogIntercept(manager.Log)

	return manager
}

func NewManagerFromConfig(configFile string) *Manager {
	manager := NewManager()
	manager.ConfigFile = configFile

	return manager
}

func (self *Manager) Initialize() error {
	// load main config
	if self.ConfigFile != `` {
		if err := self.loadConfigFromFile(self.ConfigFile); err != nil {
			return err
		}

		// load included configs (if any were specified in the main config)
		for _, includeGlob := range self.includes {
			if include, err := pathutil.ExpandUser(includeGlob); err == nil {
				if matches, err := filepath.Glob(include); err == nil {
					for _, includedConfig := range matches {
						if sliceutil.ContainsString(self.loadedConfigs, includedConfig) {
							return fmt.Errorf("Already loaded configuration at %s", includedConfig)
						}

						if err := self.loadConfigFromFile(includedConfig); err == nil {
							self.loadedConfigs = append(self.loadedConfigs, includedConfig)
						} else {
							return err
						}
					}
				} else {
					return err
				}
			} else {
				return err
			}
		}
	}

	if self.ChildLogDir == `` {
		if u, err := user.Current(); err == nil && u.Uid == `0` {
			self.ChildLogDir = `/var/log/procwatch`
		} else {
			self.ChildLogDir, _ = pathutil.ExpandUser(`~/.cache/procwatch`)
		}
	}

	if self.LogFile == `` {
		self.LogFile = filepath.Join(self.ChildLogDir, `procwatch.log`)
	}

	if self.LogFileMaxBytes != `` {
		if b, err := humanize.ParseBytes(self.LogFileMaxBytes); err == nil {
			self.logFileMaxBytes = b
		} else {
			return fmt.Errorf("logfile_maxbytes: %v", err)
		}
	} else {
		self.LogFileMaxBytes = DefaultLogFileMaxBytes.To(convutil.Megabyte)
		self.logFileMaxBytes = uint64(DefaultLogFileMaxBytes)
	}

	if self.Server != nil {
		if err := self.Server.Initialize(self); err == nil {
			go self.Server.Start()
		} else {
			return err
		}
	}

	return nil
}

func (self *Manager) AddProgram(program *Program) error {
	newprogram := NewProgram(program.Name, self)

	if err := structutil.CopyNonZero(newprogram, program); err == nil {
		newprogram.LoadIndex = len(self.programs)
		self.programs = append(self.programs, newprogram)
		return nil
	} else {
		return err
	}
}

func (self *Manager) loadConfigFromFile(filename string) error {
	log.Infof("Loading configuration file: %s", filename)

	if data, err := ioutil.ReadFile(filename); err == nil {
		if err := LoadGlobalConfig(data, self); err != nil {
			return err
		}

		if loaded, err := LoadProgramsFromConfig(data, self); err == nil {
			for _, program := range loaded {
				self.AddProgram(program)
			}
		} else {
			return err
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
		isStopping := self.stopping

		if isStopping {
			self.externalWaiters <- true
			break
		}

		time.Sleep(500 * time.Millisecond)
	}
}

func (self *Manager) Wait() {
	<-self.externalWaiters
	log.Debugf("Mainloop exited")
}

func (self *Manager) Stop(force bool) {
	self.stopping = true

	for _, program := range self.programs {
		if force {
			log.Warningf("Force stopping program %s", program.Name)
			program.ForceStop()
		} else {
			log.Infof("Stopping program %s", program.Name)
			program.Stop()
		}
	}

	log.Infof("All programs stopped, stopping manager...")
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
	isStopping := self.stopping
	defer checkLock.Done()

	if isStopping {
		return
	}

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
	for event := range self.Events {
		if event.Error != nil {
			log.Error(event.Error)
		} else {
			log.Debug(event.String())
		}

		// dispatch event to all registered handlers
		for _, handler := range self.eventHandlers {
			handler(event)
		}
	}
}

func LoadGlobalConfig(data []byte, manager *Manager) error {
	if iniFile, err := ini.Load(data); err == nil {
		for _, section := range iniFile.Sections() {
			switch section.Name() {
			case `procwatch`, `supervisord`:
				if err := section.MapTo(manager); err != nil {
					return err
				}
			case `server`:
				if key := section.Key(`enabled`); key != nil && key.MustBool(false) {
					if err := section.MapTo(manager.Server); err != nil {
						return err
					}
				}

			case `include`:
				if key := section.Key(`files`); key != nil {
					if value := key.MustString(``); value != `` {
						fileGlobs := strings.Split(value, `,`)
						manager.includes = append(manager.includes, fileGlobs...)
					}
				}
			}
		}
	} else {
		return err
	}

	return nil
}

func (self *Manager) Log(level log.Level, line string, stack log.StackItems) {
	if self.LogFile == `` {
		return
	} else if level < log.GetLevel(self.LogLevel) {
		return
	}

	if self.rollingLogger == nil {
		self.rollingLogger = &lumberjack.Logger{
			Filename:   self.LogFile,
			MaxSize:    int(mathutil.ClampLower(float64(self.logFileMaxBytes/1048576), 1)),
			MaxBackups: self.LogFileBackups,
			Compress:   true,
		}
	}

	fmt.Fprintf(
		self.rollingLogger,
		"%s %v: %s\n",
		time.Now().Format(`2006-01-02 15:04:05,999`),
		level,
		line,
	)
}
