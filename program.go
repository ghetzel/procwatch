package procwatch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/ghetzel/go-stockutil/executil"
	"github.com/ghetzel/go-stockutil/fileutil"
	"github.com/ghetzel/go-stockutil/log"
	"github.com/ghetzel/go-stockutil/mathutil"
	"github.com/ghetzel/go-stockutil/rxutil"
	"github.com/ghetzel/go-stockutil/sliceutil"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/ghetzel/go-stockutil/typeutil"
	"github.com/go-cmd/cmd"
	"github.com/go-ini/ini"
	"github.com/mattn/go-shellwords"
	"github.com/natefinch/lumberjack"
	"github.com/robfig/cron"
)

const MaxProcessKillWaitTime = (5 * time.Second)
const ProcessStateSettleInterval = (250 * time.Millisecond)

type ProgramState string

const (
	ProgramStopped  ProgramState = `STOPPED`
	ProgramStarting ProgramState = `STARTING`
	ProgramRunning  ProgramState = `RUNNING`
	ProgramBackoff  ProgramState = `BACKOFF`
	ProgramStopping ProgramState = `STOPPING`
	ProgramExited   ProgramState = `EXITED`
	ProgramFatal    ProgramState = `FATAL`
	ProgramUnknown  ProgramState = `UNKNOWN`
)

type ProgramSignal string

const (
	SIGKILL ProgramSignal = `KILL`
	SIGINT  ProgramSignal = `INT`
	SIGTERM ProgramSignal = `TERM`
	SIGHUP  ProgramSignal = `HUP`
	SIGQUIT ProgramSignal = `QUIT`
	SIGUSR1 ProgramSignal = `USR1`
	SIGUSR2 ProgramSignal = `USR2`
)

func (signal ProgramSignal) Signal() os.Signal {
	switch signal {
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
	Name                  string        `json:"name"                              ini:"-"`
	LoadIndex             int           `json:"index"                             ini:"-"`
	State                 ProgramState  `json:"state"                             ini:"-"`
	ProcessID             int           `json:"pid"                               ini:"-"`
	Command               any           `json:"command"                           ini:"-"`
	ProcessName           string        `json:"process_name,omitempty"            ini:"process_name,omitempty"`
	NumProcs              int           `json:"numprocs,omitempty"                ini:"numprocs,omitempty"`
	Directory             string        `json:"directory,omitempty"               ini:"directory,omitempty"`
	UMask                 int           `json:"umask,omitempty"                   ini:"umask,omitempty"`
	Priority              int           `json:"priority,omitempty"                ini:"priority,omitempty"`
	AutoStart             bool          `json:"autostart,omitempty"               ini:"autostart,omitempty"`
	AutoRestart           string        `json:"autorestart,omitempty"             ini:"autorestart,omitempty"`
	StartSeconds          int           `json:"startsecs,omitempty"               ini:"startsecs,omitempty"`
	StartRetries          int           `json:"startretries,omitempty"            ini:"startretries,omitempty"`
	ExitCodes             []int         `json:"exitcodes,omitempty"               delim:"," ini:"exitcodes,omitempty"`
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
	Environment           []string      `json:"environment,omitempty"             delim:"," ini:"environment,omitempty"`
	ServerUrl             string        `json:"serverurl,omitempty"               ini:"serverurl,omitempty"`
	Schedule              string        `json:"schedule,omitempty"                ini:"schedule,omitempty"`
	CommandString         string        `json:"-"                                 ini:"command"`
	LastExitStatus        int           `json:"last_exit_status,omitempty"        ini:"-"`
	LastStartedAt         time.Time     `json:"last_started_at,omitempty"         ini:"-"`
	LastExitedAt          time.Time     `json:"last_exited_at,omitempty"          ini:"-"`
	LastTriggeredAt       time.Time     `json:"last_triggered_at,omitempty"       ini:"-"`
	NextScheduledAt       time.Time     `json:"next_scheduled_at,omitempty"       ini:"-"`
	processRetryCount     int
	manager               *Manager
	cmd                   *cmd.Cmd
	hasEverBeenStarted    bool
	processLock           sync.Mutex
	rollingLogger         *lumberjack.Logger
}

func LoadProgramsFromConfig(data []byte, manager *Manager) (map[string]*Program, error) {
	var programs = make(map[string]*Program)

	if iniFile, err := ini.Load(data); err == nil {
		for _, section := range iniFile.Sections() {
			if strings.HasPrefix(section.Name(), `program:`) {
				var _, name = stringutil.SplitPair(section.Name(), `:`)
				var program = new(Program)

				if err := section.MapTo(program); err == nil {
					program.Name = name
					program.Command = program.CommandString

					if err := manager.AddProgram(program); err != nil {
						return nil, fmt.Errorf("program:%v: %v", name, err)
					}
				} else {
					return nil, fmt.Errorf("program:%v: %v", name, err)
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
		StartSeconds:          0,
		StartRetries:          3,
		ExitCodes:             []int{0, 2},
		StopSignal:            `TERM`,
		StopWaitSeconds:       10,
		StdoutLogfile:         typeutil.OrString(manager.DefaultStdoutLogfile, `AUTO`),
		RedirectStderr:        manager.RedirectStderr,
		StdoutLogfileMaxBytes: manager.StdoutLogfileMaxBytes,
		StdoutLogfileBackups:  manager.StdoutLogfileBackups,
		StderrLogfile:         typeutil.OrString(manager.DefaultStderrLogfile, `AUTO`),
		StderrLogfileMaxBytes: manager.StderrLogfileMaxBytes,
		StderrLogfileBackups:  manager.StderrLogfileBackups,
		Environment:           make([]string, 0),
		ServerUrl:             `AUTO`,
		manager:               manager,
		LastExitStatus:        -1,
		processRetryCount:     0,
	}
}

func (program *Program) String() string {
	switch program.GetState() {
	case ProgramStopped:
		return `Not started`
	case ProgramRunning:
		if program.LastStartedAt.IsZero() {
			return fmt.Sprintf("pid %d", program.PID())
		} else {
			return fmt.Sprintf("pid %d, uptime %v", program.PID(), time.Since(program.LastStartedAt).Round(time.Second))
		}
	case ProgramExited:
		return fmt.Sprintf("exited at %v", program.LastExitedAt.Format(time.RFC3339))
	case ProgramFatal, ProgramBackoff:
		return fmt.Sprintf("crashed with status %d at %v", program.LastExitStatus, program.LastExitedAt)
	}

	return ``
}

func (program *Program) detectLevel(line string) log.Level {
	var iline = strings.ToLower(line)

	if rxutil.IsMatchString(`(error|critical|fail)`, iline) {
		return log.ERROR
	} else if rxutil.IsMatchString(`warn`, iline) {
		return log.WARNING
	} else if rxutil.IsMatchString(`debug`, iline) {
		return log.DEBUG
	} else {
		return log.INFO
	}
}

func (program *Program) Log(line string, stdout bool) {
	log.Logf(program.detectLevel(line), "[%s] \u25b8  %s", program.Name, line)

	var logfile string
	var suffix string

	if stdout || program.RedirectStderr || program.manager.RedirectStderr {
		logfile = program.StdoutLogfile

		if program.RedirectStderr {
			suffix = `.log`
		} else {
			suffix = `_out.log`
		}
	} else {
		logfile = program.StderrLogfile
		suffix = `_err.log`
	}

	if logfile == `AUTO` {
		// TODO: this should be ProcessName, but has to wait until pattern interpolation is built
		logfile = filepath.Join(program.manager.ChildLogDir, fmt.Sprintf("%s%s", program.Name, suffix))
	}

	logfile = fileutil.MustExpandUser(logfile)

	switch strings.ToLower(logfile) {
	case `none`:
		return
	case `stdout`:
		fmt.Fprint(os.Stdout, strings.TrimSuffix(line, "\n")+"\n")
	case `stderr`:
		fmt.Fprint(os.Stderr, strings.TrimSuffix(line, "\n")+"\n")
	default:
		if program.rollingLogger == nil {
			var maxsize int
			var backups int

			if stdout || program.RedirectStderr {
				backups = program.StdoutLogfileBackups

				if b, err := humanize.ParseBytes(program.StdoutLogfileMaxBytes); err == nil {
					maxsize = int(b)
				} else {
					maxsize = int(DefaultLogFileMaxBytes)
				}
			} else {
				backups = program.StderrLogfileBackups

				if b, err := humanize.ParseBytes(program.StderrLogfileMaxBytes); err == nil {
					maxsize = int(b)
				} else {
					maxsize = int(DefaultLogFileMaxBytes)
				}
			}

			program.rollingLogger = &lumberjack.Logger{
				Filename:   logfile,
				MaxSize:    int(mathutil.ClampLower(float64(maxsize/1048576), 1)),
				MaxBackups: backups,
				Compress:   true,
			}

			if parent := filepath.Dir(logfile); !fileutil.DirExists(parent) {
				os.MkdirAll(parent, 0700)
			}
		}

		fmt.Fprintf(
			program.rollingLogger,
			"%s %s\n",
			time.Now().Format(`2006-01-02 15:04:05,999`),
			line,
		)
	}
}

func (program *Program) GetState() ProgramState {
	return program.State
}

func (program *Program) HasEverBeenStarted() bool {
	return program.hasEverBeenStarted
}

func (program *Program) ShouldAutoRestart() bool {
	var now = time.Now()

	if cronstring := strings.TrimSpace(program.Schedule); cronstring != `` {
		var schedule cron.Schedule

		if s, err := cron.ParseStandard(cronstring); err == nil {
			schedule = s
		} else if s, err := cron.Parse(cronstring); err == nil {
			schedule = s
		} else {
			log.Warningf("[%s] invalid schedule string %q - %v", program.Name, cronstring, err)
			return false
		}

		if next := schedule.Next(now); !program.NextScheduledAt.Equal(next) {
			program.NextScheduledAt = next
			program.LastTriggeredAt = now
			log.Debugf("[%s] Scheduled start, next scheduled to start at %v", program.Name, program.NextScheduledAt)
			return true
		} else {
			return false
		}
	}

	switch program.GetState() {
	case ProgramFatal, ProgramStopped:
		return false
	}

	var autorestart = strings.ToLower(program.AutoRestart)

	switch autorestart {
	case `unexpected`:
		if program.IsExpectedStatus(program.LastExitStatus) {
			return false
		}

		fallthrough
	case `true`:
		if program.processRetryCount < program.StartRetries {
			return true
		}
	}

	return false
}

func (program *Program) IsExpectedStatus(code int) bool {
	if len(program.ExitCodes) == 0 {
		program.ExitCodes = []int{0, 2}
	}

	for _, validStatus := range program.ExitCodes {
		if code == validStatus {
			return true
		}
	}

	return false
}

func (program *Program) Start() int {
	if program.InState(
		ProgramStopped,
		ProgramExited,
		ProgramFatal,
		ProgramBackoff,
	) {
		program.hasEverBeenStarted = true
		go program.monitorProcess()

		program.transitionTo(ProgramStarting)

		// if process started successfully and stayed running for program.StartSeconds
		if err := program.startProcess(); err == nil {
			program.transitionTo(ProgramRunning)
		} else {
			log.Warningf("[%s] Failed to start: %v", program.Name, err)

			program.killProcess(false)

			if program.ShouldAutoRestart() {
				program.transitionTo(ProgramBackoff)
			} else {
				program.transitionTo(ProgramFatal)
			}

			program.LastExitedAt = time.Now()
		}
	}

	return program.PID()
}

func (program *Program) Stop() {
	if program.InState(
		ProgramStarting,
		ProgramRunning,
	) {
		program.transitionTo(ProgramStopping)
		program.processRetryCount = 0
		program.killProcess(false)
	}
}

func (program *Program) ForceStop() {
	program.transitionTo(ProgramStopping)
	program.killProcess(true)
}

func (program *Program) StopFatal() {
	program.Stop()
	program.transitionTo(ProgramFatal)
}

func (program *Program) Restart() {
	program.Stop()
	program.Start()
}

func (program *Program) PID() int {
	if !program.InState(ProgramStarting, ProgramRunning, ProgramStopping) {
		return -1
	}

	return program.ProcessID
}

func (program *Program) InState(states ...ProgramState) bool {
	var currentState = program.GetState()

	for _, state := range states {
		if currentState == state {
			return true
		}
	}

	return false
}

func (program *Program) InTerminalState() bool {
	return program.InState(
		ProgramStopped,
		ProgramExited,
		ProgramFatal,
	)
}

func (program *Program) transitionTo(state ProgramState) {
	if program.GetState() != state {
		switch state {
		case ProgramBackoff:
			program.processRetryCount += 1
		}

		program.State = state
		program.manager.pushProcessStateEvent(state, program, nil)
	}
}

func (program *Program) isRunning() bool {
	program.processLock.Lock()
	var process = program.cmd
	program.processLock.Unlock()

	if process != nil {
		if status := process.Status(); !status.Complete {
			return true
		}
	}

	return false
}

func (program *Program) startProcess() error {
	if program.isRunning() {
		return fmt.Errorf("Program already running or holding onto stale process handle")
	} else if !program.InState(ProgramStarting) {
		return fmt.Errorf("Program in wrong state (wanted: STARTING, got: %s)", program.GetState())
	}

	var words []string

	if typeutil.IsArray(program.Command) {
		words = sliceutil.Stringify(program.Command)
	} else {
		var shwords = shellwords.NewParser()
		shwords.ParseEnv = true
		shwords.ParseBacktick = false

		if w, err := shwords.Parse(typeutil.String(program.Command)); err == nil {
			words = w
		} else {
			return err
		}
	}

	if len(words) > 0 {
		for i, word := range words {
			// expand all tildes into the current user's home directory
			words[i], _ = fileutil.ExpandUser(word)

			// expand environment variables
			words[i] = os.ExpandEnv(words[i])
		}

		var cmd = cmd.NewCmdOptions(cmd.Options{
			Streaming: true,
		}, words[0], words[1:]...)

		cmd.Env = program.getEnvironment()
		cmd.Dir = fileutil.MustExpandUser(program.Directory)

		go func() {
			for line := range cmd.Stdout {
				for _, ln := range strings.Split(line, "\n") {
					program.Log(ln, true)
				}
			}
		}()

		go func() {
			for line := range cmd.Stderr {
				for _, ln := range strings.Split(line, "\n") {
					program.Log(ln, false)
				}
			}
		}()

		log.Debugf("[%s] command: %s", program.Name, executil.Join(words))
		cmd.Start()

		if status := cmd.Status(); status.Error == nil {
			// ---------------------------------------------------------------------
			program.processLock.Lock()

			program.cmd = cmd
			program.ProcessID = status.PID
			program.LastStartedAt = time.Now()

			program.processLock.Unlock()
			// ---------------------------------------------------------------------

			if program.ProcessID > 0 {
				log.Debugf("[%s] Program started: pid=%d", program.Name, program.ProcessID)
			}

			go program.monitorProcess()

			if program.StartSeconds > 0 {
				var startDuration = time.Duration(program.StartSeconds) * time.Second

				time.Sleep(startDuration)

				if program.isRunning() {
					log.Debugf("[%s] program stayed running for %s, PID=%d", program.Name, startDuration, program.ProcessID)
					return nil
				} else {
					return fmt.Errorf("command did not stay running for %s", startDuration)
				}
			} else {
				return nil
			}
		} else {
			return status.Error
		}
	} else {
		return fmt.Errorf("empty command specified")
	}
}

func (program *Program) monitorProcess() {
	program.processLock.Lock()
	var process = program.cmd
	program.processLock.Unlock()

	if process != nil {
		<-process.Done()
		var status = process.Status()

		if status.Error == nil {
			log.Debugf("[%s] PID %d exited with status %d", program.Name, status.PID, status.Exit)
		} else {
			log.Warningf("[%s] PID %d exited with status %d: %v", program.Name, status.PID, status.Exit, status.Error)
		}

		// update the last known exit status
		program.LastExitStatus = status.Exit

		if program.IsExpectedStatus(program.LastExitStatus) {
			// if the code is an expected one, EXITED
			program.LastExitedAt = time.Now()
			program.transitionTo(ProgramExited)

		} else if program.ShouldAutoRestart() {
			// if not expected, but we should restart: BACKOFF
			program.transitionTo(ProgramBackoff)
		} else {
			// unexpected status that shouldn't restart: FATAL
			program.transitionTo(ProgramFatal)
		}

		program.processLock.Lock()
		program.cmd = nil
		program.ProcessID = 0
		program.processLock.Unlock()
	}
}

func (program *Program) killProcess(force bool) error {
	if program.InState(ProgramStarting, ProgramRunning, ProgramStopping) {
		program.processLock.Lock()
		var process = program.cmd
		program.processLock.Unlock()

		if process != nil {
			var status = process.Status()
			log.Debugf("[%s] Stopping PID %d with", program.Name, status.PID)

			if err := process.Stop(); err == nil {
				select {
				case <-process.Done():
					if wait := process.Status(); wait.Error != nil {
						if force {
							program.transitionTo(ProgramFatal)
						} else {
							program.transitionTo(ProgramStopped)
						}

						return err
					}

				case <-time.After(time.Duration(program.StopWaitSeconds) * time.Second):
					if !force {
						log.Warningf("[%s] Signal not handled in time, sending SIGKILL", program.Name)
						return program.killProcess(true)
					} else {
						return fmt.Errorf("[%s] SIGKILL not handled", program.Name)
					}
				}
			} else {
				return err
			}
		}
	}

	return nil
}

func (program *Program) getEnvironment() []string {
	return append(os.Environ(), program.Environment...)
}
