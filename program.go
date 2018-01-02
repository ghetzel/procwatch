package procwatch

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ghetzel/go-stockutil/stringutil"
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
	cmd                   *exec.Cmd
	processLock           sync.Mutex
	hasEverBeenStarted    bool
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
		return fmt.Sprintf("exited at %v", self.LastExitedAt.Format(time.RFC3339))
	case ProgramFatal, ProgramBackoff:
		return fmt.Sprintf("crashed with status %d at %v", self.LastExitStatus, self.LastExitedAt)
	}

	return ``
}

func (self *Program) GetState() ProgramState {
	return self.State
}

func (self *Program) getProcess() *os.Process {
	self.processLock.Lock()
	defer self.processLock.Unlock()

	if self.cmd != nil && self.cmd.Process != nil {
		return self.cmd.Process
	}

	return nil
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
	if len(self.ExitCodes) == 0 {
		self.ExitCodes = []int{0, 2}
	}

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
		go self.monitorProcess()

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
		self.killProcess(false)
	}
}

func (self *Program) ForceStop() {
	self.transitionTo(ProgramStopping)
	self.killProcess(true)
}

func (self *Program) StopFatal() {
	self.Stop()
	self.transitionTo(ProgramFatal)
}

func (self *Program) PID() int {
	if !self.InState(ProgramStarting, ProgramRunning, ProgramStopping) {
		return -1
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

		self.State = state
		self.manager.pushProcessStateEvent(state, self, nil)
	}
}

func (self *Program) startProcess() error {
	if !self.InState(ProgramStarting) {
		return fmt.Errorf("Program in wrong state (wanted: STARTING, got: %s)", self.GetState())
	}

	if self.getProcess() != nil {
		return fmt.Errorf("Program already running or holding onto stale process handle")
	}

	shwords := shellwords.NewParser()
	shwords.ParseEnv = true
	shwords.ParseBacktick = false

	if words, err := shwords.Parse(self.Command); err == nil {
		cmd := exec.Command(words[0], words[1:]...)

		cmd.Env = self.getEnvironment()
		cmd.Dir = self.Directory
		cmd.Stdout = NewLogIntercept(self, false)
		cmd.Stderr = NewLogIntercept(self, true)

		if err := cmd.Start(); err == nil {
			// ---------------------------------------------------------------------
			self.processLock.Lock()

			self.cmd = cmd
			self.ProcessID = cmd.Process.Pid
			self.LastStartedAt = time.Now()

			self.processLock.Unlock()
			// ---------------------------------------------------------------------

			log.Debugf("[%s] Program started: pid=%d command=%v %+v", self.Name, self.ProcessID, words[0], stringutil.WrapEach(words[1:], `'`, `'`))

			go self.monitorProcess()

			if self.StartSeconds > 0 {
				startDuration := time.Duration(self.StartSeconds) * time.Second

				select {
				case <-time.After(startDuration):
					if self.InState(ProgramStarting) {
						log.Debugf("[%s] Program stayed running for %s, PID=%d", self.Name, startDuration, self.ProcessID)
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

func (self *Program) monitorProcess() {
	if process := self.getProcess(); process != nil {
		var code int

		state, err := process.Wait()

		// ---------------------------------------------------------------------
		self.processLock.Lock()

		if err == nil {
			if unix, ok := state.Sys().(syscall.WaitStatus); ok {
				code = unix.ExitStatus()
			} else if state.Success() {
				code = 0
			} else {
				code = 127
			}

			log.Debugf("[%s] PID %d exited with code %d", self.Name, state.Pid(), code)
		} else {
			log.Warningf("[%s] Error getting process state: %v", self.Name, err)
			code = 254
		}

		// update the last known exit status
		self.LastExitStatus = code

		if self.IsExpectedStatus(code) {
			// if the code is an expected one, EXITED
			self.LastExitedAt = time.Now()
			self.transitionTo(ProgramExited)

		} else if self.ShouldAutoRestart() {
			// if not expected, but we should restart: BACKOFF
			self.transitionTo(ProgramBackoff)
		} else {
			// unexpected status that shouldn't restart: FATAL
			self.transitionTo(ProgramFatal)
		}

		self.cmd = nil
		self.ProcessID = 0

		self.processLock.Unlock()
		// ---------------------------------------------------------------------
	}
}

func (self *Program) killProcess(force bool) error {
	if self.InState(ProgramStarting, ProgramRunning, ProgramStopping) {
		var signal os.Signal

		if force {
			signal = os.Kill
		} else {
			signal = self.StopSignal.Signal()
		}

		if process := self.getProcess(); process != nil {
			var err error

			// if propagating to the whole process group, the signal is negative
			if self.StopAsGroup || (force && self.KillAsGroup) {
				if pgrp, perr := syscall.Getpgid(self.PID()); perr == nil {
					log.Debugf("[%s] Killing Process Group %d with signal %v", self.Name, pgrp, signal)

					if sig, ok := signal.(syscall.Signal); ok {
						err = syscall.Kill(-1*pgrp, sig)
					} else {
						err = fmt.Errorf("wrong signal type; expected syscall.Signal, got %T", signal)
					}
				} else {
					err = fmt.Errorf("failed to retrieve process group ID: %v", perr)
				}

			} else {
				log.Debugf("[%s] Killing PID %d with signal %v", self.Name, self.PID(), signal)
				err = process.Signal(signal)
			}

			if err == nil {
				processExited := make(chan bool)

				// wait for the signal to be dealt with
				go func() {
					for {
						if self.cmd == nil {
							processExited <- true
							return
						} else {
							time.Sleep(67 * time.Millisecond)
						}
					}
				}()

				// wait for signal acknowledgment or timeout
				select {
				case <-processExited:
					if force {
						self.transitionTo(ProgramFatal)
					} else {
						self.transitionTo(ProgramStopped)
					}

					return nil
				case <-time.After(time.Duration(self.StopWaitSeconds) * time.Second):
					if !force {
						log.Warningf("[%s] Signal not handled in time, sending SIGKILL", self.Name)
						return self.killProcess(true)
					} else {
						return fmt.Errorf("[%s] SIGKILL not handled", self.Name)
					}
				}
			} else {
				log.Errorf("[%s] Failed to send signal: %v", self.Name, err)
			}
		}
	}

	return nil
}

func (self *Program) getEnvironment() []string {
	return append(os.Environ(), self.Environment...)
}
