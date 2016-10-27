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
	processStartCount     int
	manager               *Manager
	command               *exec.Cmd
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

					shwords := shellwords.NewParser()
					shwords.ParseBacktick = false
					shwords.ParseEnv = true

					if words, err := shwords.Parse(program.Command); err == nil {
						program.command = exec.Command(words[0], words[1:]...)

						programs[program.Name] = program
						loadedPrograms += 1
					} else {
						return nil, err
					}

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
	}
}

func (self *Program) Start(manual bool) {
	switch self.State {
	case ProgramRunning, ProgramStarting:
		return
	case ProgramBackoff:
		if self.processStartCount < self.StartRetries {
			self.runProcess()
			return
		}

	case ProgramExited:
		self.runProcess()
		return

	case ProgramStopped:
		if !self.hasEverBeenStarted {
			self.runProcess()
			return
		}
	}

	// process falls back to fatal
	self.StopFatal()
}

func (self *Program) StopFatal() {
	self.processStartCount = 0
	self.setState(ProgramFatal)
}

func (self *Program) setState(state ProgramState) {
	self.State = state
	self.manager.pushProcessStateEvent(state, self, nil)
}

func (self *Program) setErrorState(err error) {
	if self.processStartCount < self.StartRetries {
		self.State = ProgramBackoff
	} else {
		self.State = ProgramFatal
	}

	self.manager.pushProcessStateEvent(self.State, self, err)
}

func (self *Program) runProcess() {
	self.hasEverBeenStarted = true
	self.processStartCount += 1

	if err := self.command.Start(); err != nil {
		self.setErrorState(err)
		return
	}

	done := make(chan bool)
	go self.waitForRunningProcess(done)

	select {
	case <-done:
		return
	case <-time.After(time.Duration(self.StartSeconds) * time.Second):
		self.killProcess()
	}
}

func (self *Program) waitForRunningProcess(done chan bool) {
	var exit interface{}
	exit = self.command.Wait()

	log.Debugf("POSTWAIT: %v (%T)", exit, exit)

	switch exit.(type) {
	case *exec.ExitError:
		exitStatus := exit.(*exec.ExitError)
		pStateI := exitStatus.Sys()

		switch pStateI.(type) {
		case syscall.WaitStatus:
			pState := pStateI.(syscall.WaitStatus)
			exitCode := pState.ExitStatus()
			var statusOk bool
			exitWaitStart := time.Now()

			for {
				if time.Now().Sub(exitWaitStart) > (time.Duration(self.StopWaitSeconds) * time.Second) {
					self.setErrorState(fmt.Errorf("Timed out waiting for %q to stop, killing process...", self.Name))
					self.killProcess()
					done <- true
					return
				}

				if pState.Exited() {
					break
				} else {
					time.Sleep(100 * time.Millisecond)
				}
			}

			for _, validStatus := range self.ExitCodes {
				if exitCode == validStatus {
					statusOk = true
					break
				}
			}

			if statusOk {
				self.setState(ProgramExited)
			} else {
				self.setErrorState(fmt.Errorf("Program %q exited with status %d",
					self.Name,
					exitCode))
			}
		}
	default:
		self.setErrorState(fmt.Errorf("Execution error: %v", exit))
	}

	done <- true
}

func (self *Program) killProcess() {
	if self.command.Process != nil {
		var err error

		if signal := self.StopSignal.Signal(); signal == os.Kill {
			err = self.command.Process.Kill()
		} else {
			err = self.command.Process.Signal(signal)
		}

		if err == nil {
			self.setState(ProgramStopping)
		} else {
			self.setState(ProgramFatal)
		}
	}
}
