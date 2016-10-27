package procwatch

import (
	"fmt"
	"github.com/go-ini/ini"
	"github.com/mattn/go-shellwords"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const MaxProcessKillWaitTime = (5 * time.Second)

type ProgramState int

const (
	ProgramStopped  ProgramState = 0
	ProgramStarting              = 10
	ProgramRunning               = 20
	ProgramBackoff               = 30
	ProgramStopping              = 40
	ProgramExited                = 100
	ProgramFatal                 = 200
	ProgramUnknown               = 1000
)

type ProgramSignal string

const (
	SIGKILL ProgramSignal = `KILL`
	SIGINT                = `INT`
	SIGTERM               = `TERM`
	SIGHUP                = `HUP`
	SIGQUIT               = `QUIT`
	SIGUSR1               = `USR1`
	SIGUSR2               = `USR2`
)

func (self ProgramSignal) Signal() os.Signal {
	switch self {
	case SIGINT:
		return os.Interrupt
	case SIGTERM:
		return syscall.SIGTERM
	case SIGHUP:
		return syscall.SIGHUP
	case SIGQUIT:
		return syscall.SIGQUIT
	case SIGUSR1:
		return syscall.SIGUSR1
	case SIGUSR2:
		return syscall.SIGUSR2
	default:
		return os.Kill
	}
}

func (self ProgramState) String() string {
	switch self {
	case ProgramStopped:
		return `STOPPED`
	case ProgramStarting:
		return `STARTING`
	case ProgramRunning:
		return `RUNNING`
	case ProgramBackoff:
		return `BACKOFF`
	case ProgramStopping:
		return `STOPPING`
	case ProgramExited:
		return `EXITED`
	case ProgramFatal:
		return `FATAL`
	default:
		return `UNKNOWN`
	}
}

type Program struct {
	Name                  string        `ini:"-"`
	LoadIndex             int           `ini:"-"`
	State                 ProgramState  `ini:"-"`
	Command               string        `ini:"command"`
	ProcessName           string        `ini:"process_name,omitempty"`
	NumProcs              int           `ini:"numprocs,omitempty"`
	Directory             string        `ini:"directory,omitempty"`
	UMask                 int           `ini:"umask,omitempty"`
	Priority              int           `ini:"priority,omitempty"`
	AutoStart             bool          `ini:"autostart,omitempty"`
	AutoRestart           string        `ini:"autorestart,omitempty"`
	StartSeconds          int           `ini:"startsecs,omitempty"`
	StartRetries          int           `ini:"startretries,omitempty"`
	ExitCodes             []int         `ini:"exitcodes,omitempty" delim:","`
	StopSignal            ProgramSignal `ini:"stopsignal,omitempty"` // any of TERM, HUP, INT, QUIT, KILL, USR1, or USR2
	StopWaitSeconds       int           `ini:"stopwaitsecs,omitempty"`
	StopAsGroup           bool          `ini:"stopasgroup,omitempty"`
	KillAsGroup           bool          `ini:"killasgroup,omitempty"`
	User                  string        `ini:"user,omitempty"`
	RedirectStderr        bool          `ini:"redirect_stderr,omitempty"`
	StdoutLogfile         string        `ini:"stdout_logfile,omitempty"`
	StdoutLogfileMaxBytes string        `ini:"stdout_logfile_maxbytes,omitempty"`
	StdoutLogfileBackups  int           `ini:"stdout_logfile_backups,omitempty"`
	StdoutCaptureMaxBytes string        `ini:"stdout_capture_maxbytes,omitempty"`
	StdoutEventsEnabled   bool          `ini:"stdout_events_enabled,omitempty"`
	StderrLogfile         string        `ini:"stderr_logfile,omitempty"`
	StderrLogfileMaxBytes string        `ini:"stderr_logfile_maxbytes,omitempty"`
	StderrLogfileBackups  int           `ini:"stderr_logfile_backups,omitempty"`
	StderrCaptureMaxBytes string        `ini:"stderr_capture_maxbytes,omitempty"`
	StderrEventsEnabled   bool          `ini:"stderr_events_enabled,omitempty"`
	Environment           []string      `ini:"environment,omitempty" delim:","`
	ServerUrl             string        `ini:"serverurl,omitempty"`
	processRetryCount     int
	manager               *Manager
	command               *exec.Cmd
	hasEverBeenStarted    bool
	lastExitStatus        int
	lastStartedAt         time.Time
	commandIsRunning      bool
}

func LoadProgramsFromConfig(data []byte, manager *Manager) (map[string]*Program, error) {
	programs := make(map[string]*Program)

	if iniFile, err := ini.Load(data); err == nil {
		loadedPrograms := 0

		for _, section := range iniFile.Sections() {
			if strings.HasPrefix(section.Name(), `program:`) {
				parts := strings.SplitN(section.Name(), `:`, 2)

				program := NewProgram(parts[1], manager)

				if err := section.MapTo(program); err == nil {
					program.LoadIndex = loadedPrograms
					programs[program.Name] = program
					loadedPrograms += 1
				} else {
					return nil, err
				}
			}
		}
	} else {
		return nil, err
	}

	return programs, nil
}

func NewProgram(name string, manager *Manager) *Program {
	return &Program{
		Name:                  name,
		State:                 ProgramStopped,
		ProcessName:           `%(program_name)s`,
		NumProcs:              1,
		Priority:              999,
		AutoStart:             true,
		StartSeconds:          1,
		StartRetries:          3,
		ExitCodes:             []int{0, 2},
		StopSignal:            `TERM`,
		StopWaitSeconds:       10,
		StdoutLogfile:         `AUTO`,
		StdoutLogfileMaxBytes: `50MB`,
		StdoutLogfileBackups:  10,
		StderrLogfile:         `AUTO`,
		StderrLogfileMaxBytes: `50MB`,
		StderrLogfileBackups:  10,
		Environment:           make([]string, 0),
		ServerUrl:             `AUTO`,
		manager:               manager,
		lastExitStatus:        -1,
		processRetryCount:     0,
	}
}

func (self *Program) HasEverBeenStarted() bool {
	return self.hasEverBeenStarted
}

func (self *Program) ShouldAutoRestart() bool {
	switch self.State {
	case ProgramFatal, ProgramStopped:
		return false
	}

	autorestart := strings.ToLower(self.AutoRestart)

	switch autorestart {
	case `unexpected`:
		if self.IsExpectedStatus(self.lastExitStatus) {
			return false
		}

		fallthrough
	case `true`:
		if self.processRetryCount < self.StartRetries {
			return true
		}
	}

	return false
}

func (self *Program) IsExpectedStatus(code int) bool {
	for _, validStatus := range self.ExitCodes {
		if code == validStatus {
			return true
		}
	}

	return false
}

func (self *Program) Start() {
	self.transitionTo(ProgramStarting)

	// if process started successfully and stayed running for self.StartSeconds
	if err := self.startProcess(); err == nil {
		self.transitionTo(ProgramRunning)
	} else {
		log.Debugf("[%s] Start failed", self.Name)
		self.killProcess(false)

		if self.ShouldAutoRestart() {
			self.transitionTo(ProgramBackoff)
		} else {
			self.StopFatal()
		}
	}
}

func (self *Program) Stop() {
	self.transitionTo(ProgramStopping)
	self.processRetryCount = 0

	if err := self.killProcess(false); err == nil {
		self.transitionTo(ProgramStopped)
	} else {
		self.transitionTo(ProgramFatal)
	}
}

func (self *Program) StopFatal() {
	self.Stop()
	self.transitionTo(ProgramFatal)
}

func (self *Program) transitionTo(state ProgramState) {
	if self.State != state {
		switch state {
		case ProgramBackoff:
			self.processRetryCount += 1
		}

		self.State = state
		self.manager.pushProcessStateEvent(state, self, nil)
	}
}

func (self *Program) startProcess() error {
	if self.command != nil {
		if err := self.killProcess(false); err != nil {
			self.command = nil
			self.transitionTo(ProgramFatal)
			return err
		}
	}

	self.commandIsRunning = false

	shwords := shellwords.NewParser()
	shwords.ParseEnv = true
	shwords.ParseBacktick = false

	if words, err := shwords.Parse(self.Command); err == nil {
		self.command = exec.Command(words[0], words[1:]...)

		// setup environment, piping, etc...

		if err := self.command.Start(); err == nil {
			self.lastStartedAt = time.Now()
			go self.monitorProcessState()

			if self.StartSeconds > 0 {
				startDuration := time.Duration(self.StartSeconds) * time.Second

				select {
				case <-time.After(startDuration):
					if self.commandIsRunning {
						return nil
					} else {
						return fmt.Errorf("Command did not stay running for %s", startDuration)
					}
				}
			} else {
				return nil
			}
		} else {
			return err
		}
	} else {
		return err
	}
}

func (self *Program) monitorProcessState() {
	if self.command != nil {
		self.commandIsRunning = true
		self.command.Wait()

		// TODO: IsExpectedStatus check
		self.transitionTo(ProgramExited)
	}

	self.commandIsRunning = false

	// handle process exit state; transitions -> [EXITED, BACKOFF, FATAL]
}

func (self *Program) killProcess(force bool) error {
	// wait up to self.StopWaitSeconds for process to disappear, otherwise
	// kill-9 it.  wait for MaxProcessKillWaitTime, then FATAL it

	self.commandIsRunning = false
	return nil
}
