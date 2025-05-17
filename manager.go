package procwatch

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/ghetzel/go-stockutil/convutil"
	"github.com/ghetzel/go-stockutil/fileutil"
	"github.com/ghetzel/go-stockutil/log"
	"github.com/ghetzel/go-stockutil/mathutil"
	"github.com/ghetzel/go-stockutil/sliceutil"
	"github.com/ghetzel/go-stockutil/structutil"
	"github.com/go-ini/ini"
	"github.com/natefinch/lumberjack"
	"github.com/nxadm/tail"
)

var DefaultLogFileMaxBytes = 50 * convutil.Megabyte

type EventHandler func(*Event)

type LogLine struct {
	Timestamp  time.Time
	Text       string
	Filename   string
	LineNumber int
	Program    *Program
}

func (line LogLine) String() string {
	return line.Text
}

func (line LogLine) IsEmpty() bool {
	return len(line.Text) == 0
}

type Manager struct {
	ConfigFile            string
	Version               string      `json:"version"                 ini:"-"`
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
	DefaultStdoutLogfile  string      `json:"stdout_logfile"          ini:"stdout_logfile"`
	DefaultStderrLogfile  string      `json:"stderr_logfile"          ini:"stderr_logfile"`
	Server                *Server     `json:"server"                  ini:"server"`
	Events                chan *Event `json:"-"`
	includes              []string
	loadedConfigs         []string
	eventHandlers         []EventHandler
	programs              []*Program
	stopping              bool
	doneStopping          chan error
	externalWaiters       chan bool
	intercept             string
	rollingLogger         *lumberjack.Logger
	logFileMaxBytes       uint64
}

func NewManager() *Manager {
	var manager = &Manager{
		Version:               Version,
		LogFileMaxBytes:       `50MB`,
		StdoutLogfileMaxBytes: `50MB`,
		StderrLogfileMaxBytes: `50MB`,
		StderrLogfileBackups:  10,
		StdoutLogfileBackups:  10,
		Events:                make(chan *Event),
		Server: &Server{
			Address: DefaultAddress,
		},
		programs:        make([]*Program, 0),
		eventHandlers:   make([]EventHandler, 0),
		doneStopping:    make(chan error),
		includes:        make([]string, 0),
		loadedConfigs:   make([]string, 0),
		externalWaiters: make(chan bool),
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

func (manager *Manager) Initialize() error {
	if manager.Server == nil {
		manager.Server = &Server{
			Address: DefaultAddress,
		}
	}

	if manager.Events == nil {
		manager.Events = make(chan *Event)
	}

	// load main config
	if manager.ConfigFile != `` {
		if err := manager.loadConfigFromFile(manager.ConfigFile); err != nil {
			return err
		}

		// load included configs (if any were specified in the main config)
		for _, includeGlob := range manager.includes {
			if include, err := fileutil.ExpandUser(includeGlob); err == nil {
				if matches, err := filepath.Glob(include); err == nil {
					for _, includedConfig := range matches {
						if sliceutil.ContainsString(manager.loadedConfigs, includedConfig) {
							return fmt.Errorf("already loaded configuration at %s", includedConfig)
						}

						if err := manager.loadConfigFromFile(includedConfig); err == nil {
							manager.loadedConfigs = append(manager.loadedConfigs, includedConfig)
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

	if manager.ChildLogDir == `` {
		if u, err := user.Current(); err == nil && u.Uid == `0` {
			manager.ChildLogDir = `/var/log/procwatch`
		} else {
			manager.ChildLogDir, _ = fileutil.ExpandUser(`~/.cache/procwatch`)
		}
	}

	if manager.LogFile == `` {
		manager.LogFile = filepath.Join(manager.ChildLogDir, `procwatch.log`)
	}

	if manager.LogFileMaxBytes != `` {
		if b, err := humanize.ParseBytes(manager.LogFileMaxBytes); err == nil {
			manager.logFileMaxBytes = b
		} else {
			return fmt.Errorf("logfile_maxbytes: %v", err)
		}
	} else {
		manager.LogFileMaxBytes = DefaultLogFileMaxBytes.To(convutil.Megabyte)
		manager.logFileMaxBytes = uint64(DefaultLogFileMaxBytes)
	}

	if manager.Server != nil {
		if err := manager.Server.Initialize(manager); err == nil {
			go manager.Server.Start()
		} else {
			return err
		}
	}

	return nil
}

func (manager *Manager) AddProgram(program *Program) error {
	newprogram := NewProgram(program.Name, manager)

	if err := structutil.CopyNonZero(newprogram, program); err == nil {
		newprogram.LoadIndex = len(manager.programs)
		manager.programs = append(manager.programs, newprogram)
		return nil
	} else {
		return err
	}
}

func (manager *Manager) loadConfigFromFile(filename string) error {
	filename = fileutil.MustExpandUser(filename)
	log.Infof("Loading configuration file: %s", filename)

	if data, err := os.ReadFile(filename); err == nil {
		if err := LoadGlobalConfig(data, manager); err != nil {
			return err
		}

		if loaded, err := LoadProgramsFromConfig(data, manager); err == nil {
			for _, program := range loaded {
				manager.AddProgram(program)
			}
		} else {
			return err
		}
	} else {
		return err
	}

	return nil
}

func (manager *Manager) Run() {
	manager.stopping = false

	go manager.startEventLogger()

	for {
		var checkLock sync.WaitGroup

		for _, program := range manager.programs {
			checkLock.Add(1)
			go manager.checkProgramState(program, &checkLock)
		}

		// wait for all program checks to be complete for this iteration
		checkLock.Wait()

		// if we're stopping the manager, and if all the programs are in a terminal state, quit the loop
		if manager.stopping {
			manager.externalWaiters <- true
			break
		}

		time.Sleep(500 * time.Millisecond)
	}
}

func (manager *Manager) Wait() {
	<-manager.externalWaiters
	log.Debugf("Mainloop exited")
}

func (manager *Manager) Stop(force bool) {
	manager.stopping = true

	for _, program := range manager.programs {
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

func (manager *Manager) AddEventHandler(handler EventHandler) {
	manager.eventHandlers = append(manager.eventHandlers, handler)
}

// Process Management States
//
// STOPPED -> STARTING
//
//	|- up for startsecs? -> RUNNING
//	|                       |- manually stopped? -> STOPPING
//	|                       |                       |- stopped in time? -> [STOPPED]
//	|                       |                       \- no?              -> [FATAL]
//	|                       \- process exited?   -> EXITED -> STARTING...
//	|
//	|- no?
//	|  \- should restart? -> BACKOFF -> STARTING...
//	|                     -> [FATAL]
//	|
//	\- manually stopped?  -> STOPPING
//	                         |- stopped in time? -> [STOPPED]
//	                         \- no?              -> [FATAL]
func (manager *Manager) checkProgramState(program *Program, checkLock *sync.WaitGroup) {
	var isStopping = manager.stopping
	defer checkLock.Done()

	if isStopping {
		return
	}

	switch program.GetState() {
	case ProgramStopped:
		// first-time start for autostart programs
		if program.AutoStart && !program.HasEverBeenStarted() {
			log.Debugf("[%s] Starting program for the first time", program.Name)
			program.ShouldAutoRestart() // do this here to "seed" the scheduler with the first schedule time
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

func (manager *Manager) Programs() []*Program {
	return manager.programs
}

func (manager *Manager) Program(name string) (*Program, bool) {
	for _, program := range manager.programs {
		if program.Name == name {
			return program, true
		}
	}

	return nil, false
}

func (manager *Manager) GetProgramsByState(states ...ProgramState) []*Program {
	programs := make([]*Program, 0)

	for _, program := range manager.programs {
		currentState := program.GetState()

		for _, state := range states {
			if currentState == state {
				programs = append(programs, program)
			}
		}
	}

	return programs
}

func (manager *Manager) pushProcessStateEvent(state ProgramState, source *Program, err error, args ...string) {
	event := NewEvent([]string{
		`PROCESS_STATE`,
		fmt.Sprintf("PROCESS_STATE_%v", state),
	}, source.Name, ProgramSource, source, args...)

	event.Error = err
	manager.Events <- event
}

func (manager *Manager) startEventLogger() {
	for event := range manager.Events {
		if event.Error != nil {
			log.Error(event.Error)
		} else {
			log.Debug(event.String())
		}

		// dispatch event to all registered handlers
		for _, handler := range manager.eventHandlers {
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

func (manager *Manager) Log(level log.Level, line string, stack log.StackItems) {
	if manager.LogFile == `` {
		return
	} else if level < log.GetLevel(manager.LogLevel) {
		return
	}

	if manager.rollingLogger == nil {
		manager.rollingLogger = &lumberjack.Logger{
			Filename:   manager.LogFile,
			MaxSize:    int(mathutil.ClampLower(float64(manager.logFileMaxBytes/1048576), 1)),
			MaxBackups: manager.LogFileBackups,
			Compress:   true,
		}
	}

	fmt.Fprintf(
		manager.rollingLogger,
		"%s %v: %s\n",
		time.Now().Format(`2006-01-02 15:04:05,999`),
		level,
		line,
	)
}

func (manager *Manager) Tail(ctx context.Context) <-chan LogLine {
	var tailchan = make(chan LogLine)

	go func() {
		defer close(tailchan)

		for {
			if tailer, err := tail.TailFile(manager.rollingLogger.Filename, tail.Config{
				Poll:   true,
				Follow: true,
				ReOpen: true,
			}); err == nil {
				for line := range tailer.Lines {
					select {
					case <-ctx.Done():
						return
					case tailchan <- LogLine{
						Timestamp:  line.Time,
						Filename:   tailer.Filename,
						Text:       line.Text,
						LineNumber: line.Num,
						Program:    nil,
					}:
						continue
					}
				}
			} else {
				return
			}
		}
	}()

	return tailchan
}
