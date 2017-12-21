package procwatch

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-ini/ini"
	"github.com/mattn/go-shellwords"
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
	Command               string        `json:"command"                           ini:"command"`
	ProcessName           string        `json:"process_name,omitempty"            ini:"process_name,omitempty"`
	NumProcs              int           `json:"numprocs,omitempty"                ini:"numprocs,omitempty"`
	Directory             string        `json:"directory,omitempty"               ini:"directory,omitempty"`
	UMask                 int           `json:"umask,omitempty"                   ini:"umask,omitempty"`
	Priority              int           `json:"priority,omitempty"                ini:"priority,omitempty"`
	AutoStart             bool          `json:"autostart,omitempty"               ini:"autostart,omitempty"`
	AutoRestart           string        `json:"autorestart,omitempty"             ini:"autorestart,omitempty"`
	StartSeconds          int           `json:"startsecs,omitempty"               ini:"startsecs,omitempty"`
	StartRetries          int           `json:"startretries,omitempty"            ini:"startretries,omitempty"`
	ExitCodes             []int         `json:"exitcodes,omitempty"               delim:"," ini:"exitcodes,omitempty" delim:","`
	StopSignal            ProgramSignal `json:"stopsignal,omitempty"              ini:"stopsignal,omitempty"`
	StopWaitSeconds       int           `json:"stopwaitsecs,omitempty"            ini:"stopwaitsecs,omitempty"`
	StopAsGroup           bool          `json:"stopasgroup,omitempty"             ini:"stopasgroup,omitempty"`
	KillAsGroup           bool          `json:"killasgroup,omitempty"             ini:"killasgroup,omitempty"`
	User                  string        `json:"user,omitempty"                    ini:"user,omitempty"`
	RedirectStderr        bool          `json:"redirect_stderr,omitempty"         ini:"redirect_stderr,omitempty"`
	StdoutLogfile         string        `json:"stdout_logfile,omitempty"          ini:"stdout_logfile,omitempty"`
	StdoutLogfileMaxBytes string        `json:"stdout_logfile_maxbytes,omitempty" ini:"stdout_logfile_maxbytes,omitempty"`
	StdoutLogfileBackups  int           `json:"stdout_logfile_backups,omitempty"  ini:"stdout_logfile_backups,omitempty"`
	StdoutCaptureMaxBytes string        `json:"stdout_capture_maxbytes,omitempty" ini:"stdout_capture_maxbytes,omitempty"`
	StdoutEventsEnabled   bool          `json:"stdout_events_enabled,omitempty"   ini:"stdout_events_enabled,omitempty"`
	StderrLogfile         string        `json:"stderr_logfile,omitempty"          ini:"stderr_logfile,omitempty"`
	StderrLogfileMaxBytes string        `json:"stderr_logfile_maxbytes,omitempty" ini:"stderr_logfile_maxbytes,omitempty"`
	StderrLogfileBackups  int           `json:"stderr_logfile_backups,omitempty"  ini:"stderr_logfile_backups,omitempty"`
	StderrCaptureMaxBytes string        `json:"stderr_capture_maxbytes,omitempty" ini:"stderr_capture_maxbytes,omitempty"`
	StderrEventsEnabled   bool          `json:"stderr_events_enabled,omitempty"   ini:"stderr_events_enabled,omitempty"`
	Environment           []string      `json:"environment,omitempty"             delim:"," ini:"environment,omitempty" delim:","`
	ServerUrl             string        `json:"serverurl,omitempty"               ini:"serverurl,omitempty"`
	LastExitStatus        int
	LastStartedAt         time.Time
	LastExitedAt          time.Time
	processRetryCount     int
	manager               *Manager
	command               *exec.Cmd
	hasEverBeenStarted    bool
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
		LastExitStatus:        -1,
		processRetryCount:     0,
	}
}

func (self *Program) String() string {
	switch self.GetState() {
	case ProgramStopped:
		return `Not started`
	case ProgramRunning:
		if self.LastStartedAt.IsZero() {
			return fmt.Sprintf("pid %d", self.PID())
		} else {
			return fmt.Sprintf("pid %d, uptime %v", self.PID(), time.Since(self.LastStartedAt).Round(time.Second))
		}
	case ProgramExited:
		return fmt.Sprintf("exited at %v", self.LastExitedAt)
	case ProgramFatal, ProgramBackoff:
		return fmt.Sprintf("crashed with status %d at %v", self.LastExitStatus, self.LastExitedAt)
	}

	return ``
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
		if self.IsExpectedStatus(self.LastExitStatus) {
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

			self.LastExitedAt = time.Now()
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

func (self *Program) ForceStop() {
	self.transitionTo(ProgramStopping)

	if err := self.killProcess(true); err == nil {
		self.transitionTo(ProgramStopped)
	} else {
		self.transitionTo(ProgramFatal)
	}
}

func (self *Program) StopFatal() {
	self.Stop()
	self.transitionTo(ProgramFatal)
}

func (self *Program) PID() int {
	if !self.InState(ProgramStarting, ProgramRunning, ProgramStopping) {
		return -1
	}

	if command := self.GetCommand(); command != nil {
		if command.Process != nil {
			self.stateLock.Lock()
			defer self.stateLock.Unlock()

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

		self.stateLock.Lock()

		if state != ProgramRunning {
			self.ProcessID = 0
		}

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

		// setup output interception
		command.Stdout = NewLogIntercept(self, false)
		command.Stderr = NewLogIntercept(self, true)

		// set exec directory
		command.Dir = self.Directory

		// set environment
		command.Env = self.getEnvironment()

		if err := command.Start(); err == nil {
			self.LastStartedAt = time.Now()
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
		// block until process exits and yields an exit status
		if code, err := self.startProcessAndWaitForStatus(command); err == nil {
			// update the last known exit status
			self.LastExitStatus = code

			if self.IsExpectedStatus(code) {
				// if the code is an expected one, EXITED
				self.transitionTo(ProgramExited)
				self.LastExitedAt = time.Now()

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

func (self *Program) getEnvironment() []string {
	return append(os.Environ(), self.Environment...)
}
