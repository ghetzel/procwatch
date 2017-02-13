package procwatch

import (
	"fmt"
	"github.com/ghetzel/ini"
	"github.com/mattn/go-shellwords"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const MaxProcessKillWaitTime = (5 * time.Second)
const ProcessStateSettleInterval = (250 * time.Millisecond)

type ProgramState string

const (
	ProgramStopped  ProgramState = `STOPPED`
	ProgramStarting              = `STARTING`
	ProgramRunning               = `RUNNING`
	ProgramBackoff               = `BACKOFF`
	ProgramStopping              = `STOPPING`
	ProgramExited                = `EXITED`
	ProgramFatal                 = `FATAL`
	ProgramUnknown               = `UNKNOWN`
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

type Program struct {
	Name                  string        `ini:"-"`
	LoadIndex             int           `ini:"-"`
	State                 ProgramState  `ini:"-"`
	ProcessID             int           `ini:"-"`
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
	commandRunLock        sync.RWMutex
	stateLock             sync.RWMutex
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

func (self *Program) GetState() ProgramState {
	self.stateLock.RLock()
	defer self.stateLock.RUnlock()
	return self.State
}

func (self *Program) GetCommand() *exec.Cmd {
	self.commandRunLock.RLock()
	defer self.commandRunLock.RUnlock()
	return self.command
}

func (self *Program) HasEverBeenStarted() bool {
	return self.hasEverBeenStarted
}

func (self *Program) ShouldAutoRestart() bool {
	switch self.GetState() {
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

func (self *Program) Start() int {
	if self.InState(
		ProgramStopped,
		ProgramExited,
		ProgramFatal,
		ProgramBackoff,
	) {
		self.hasEverBeenStarted = true
		self.transitionTo(ProgramStarting)

		// if process started successfully and stayed running for self.StartSeconds
		if err := self.startProcess(); err == nil {
			self.transitionTo(ProgramRunning)
		} else {
			log.Warningf("[%s] Failed to start: %v", self.Name, err)

			self.killProcess(false)

			if self.ShouldAutoRestart() {
				self.transitionTo(ProgramBackoff)
			} else {
				self.transitionTo(ProgramFatal)
			}
		}
	}

	return self.PID()
}

func (self *Program) Stop() {
	if self.InState(
		ProgramStarting,
		ProgramRunning,
	) {
		self.transitionTo(ProgramStopping)
		self.processRetryCount = 0

		if err := self.killProcess(false); err == nil {
			self.transitionTo(ProgramStopped)
		} else {
			self.transitionTo(ProgramFatal)
		}
	}
}

func (self *Program) StopFatal() {
	self.Stop()
	self.transitionTo(ProgramFatal)
}

func (self *Program) PID() int {
	if command := self.GetCommand(); command != nil {
		if command.Process != nil {
			self.ProcessID = command.Process.Pid
			return self.ProcessID
		}
	}

	return self.ProcessID
}

func (self *Program) InState(states ...ProgramState) bool {
	currentState := self.GetState()

	for _, state := range states {
		if currentState == state {
			return true
		}
	}

	return false
}

func (self *Program) InTerminalState() bool {
	return self.InState(
		ProgramStopped,
		ProgramExited,
		ProgramFatal,
	)
}

func (self *Program) transitionTo(state ProgramState) {
	if self.GetState() != state {
		switch state {
		case ProgramBackoff:
			self.processRetryCount += 1
		}

		if state != ProgramRunning {
			self.ProcessID = 0
		}

		self.stateLock.Lock()
		self.State = state
		self.stateLock.Unlock()

		self.manager.pushProcessStateEvent(state, self, nil)
	}
}

func (self *Program) startProcess() error {
	if !self.InState(ProgramStarting) {
		return fmt.Errorf("Program in wrong state (wanted: STARTING, got: %s)", self.GetState())
	}

	shwords := shellwords.NewParser()
	shwords.ParseEnv = true
	shwords.ParseBacktick = false

	if words, err := shwords.Parse(self.Command); err == nil {
		self.commandRunLock.Lock()
		self.command = exec.Command(words[0], words[1:]...)
		self.commandRunLock.Unlock()

		// setup environment, piping, etc...
		command := self.GetCommand()

		if err := command.Start(); err == nil {
			self.lastStartedAt = time.Now()
			go self.monitorProcessGetState(command)

			if self.StartSeconds > 0 {
				startDuration := time.Duration(self.StartSeconds) * time.Second

				select {
				case <-time.After(startDuration):
					if pid := self.PID(); pid >= 0 {
						log.Debugf("Program stayed running for %s, PID=%d", startDuration, pid)
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

func (self *Program) monitorProcessGetState(command *exec.Cmd) {
	if command != nil {
		// setup output interception
		command.Stdout = NewLogIntercept(self, false)
		command.Stderr = NewLogIntercept(self, true)

		// block until process exits and yields an exit status
		if code, err := self.startProcessAndWaitForStatus(command); err == nil {
			// update the last known exit status
			self.lastExitStatus = code

			if self.IsExpectedStatus(code) {
				// if the code is an expected one, EXITED
				self.transitionTo(ProgramExited)
			} else if self.ShouldAutoRestart() {
				// if not expected, but we should restart: BACKOFF
				self.transitionTo(ProgramBackoff)
			} else {
				// unexpected status that shouldn't restart: FATAL
				self.transitionTo(ProgramFatal)
			}
		} else {
			self.transitionTo(ProgramStopped)
		}
	}
}

func (self *Program) startProcessAndWaitForStatus(command *exec.Cmd) (int, error) {
	err := command.Wait()

	if err == nil {
		return 0, nil
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		psI := exitErr.Sys()

		switch psI.(type) {
		case syscall.WaitStatus:
			ps := psI.(syscall.WaitStatus)
			return ps.ExitStatus(), nil

		default:
			return -1, fmt.Errorf("Unsupported process state structure %T", psI)
		}
	} else {
		return -1, err
	}
}

func (self *Program) killProcess(force bool) error {
	if self.InState(ProgramStarting, ProgramRunning, ProgramStopping) {
		if command := self.GetCommand(); command != nil {
			if command.Process != nil {
				var signal os.Signal

				if force {
					signal = os.Kill
				} else {
					signal = self.StopSignal.Signal()
				}

				log.Debugf("[%s] Killing PID %d with signal %v", self.Name, self.PID(), signal)

				// send the requested signal to the process
				if err := command.Process.Signal(signal); err == nil {
					processExited := make(chan bool)

					// wait for the signal to be dealt with
					go func() {
						command.Process.Wait()
						processExited <- true
					}()

					// wait for signal acknowledgment or timeout
					select {
					case <-processExited:
						break
					case <-time.After(time.Duration(self.StopWaitSeconds) * time.Second):
						if !force {
							log.Warningf("[%s] Signal not handled in time, sending SIGKILL", self.Name)
							return self.killProcess(true)
						}
					}
				}
			}
		}
	}

	return nil
}
