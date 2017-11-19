package procwatch

import (
	"path"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var actualStates = make([]ProgramState, 0)

func newManager(config string) (*Manager, error) {
	log.Debugf("Creating new manager...")
	manager := NewManager(path.Join(`./tests`, config+`.ini`))
	actualStates = nil

	if err := manager.Initialize(); err == nil {
		manager.AddEventHandler(func(event *Event) {
			if event.HasName(`PROCESS_STATE_STOPPED`) {
				actualStates = append(actualStates, ProgramStopped)
			} else if event.HasName(`PROCESS_STATE_STARTING`) {
				actualStates = append(actualStates, ProgramStarting)
			} else if event.HasName(`PROCESS_STATE_RUNNING`) {
				actualStates = append(actualStates, ProgramRunning)
			} else if event.HasName(`PROCESS_STATE_BACKOFF`) {
				actualStates = append(actualStates, ProgramBackoff)
			} else if event.HasName(`PROCESS_STATE_STOPPING`) {
				actualStates = append(actualStates, ProgramStopping)
			} else if event.HasName(`PROCESS_STATE_EXITED`) {
				actualStates = append(actualStates, ProgramExited)
			} else if event.HasName(`PROCESS_STATE_FATAL`) {
				actualStates = append(actualStates, ProgramFatal)
			} else {
				actualStates = append(actualStates, ProgramUnknown)
			}
		})

		return manager, nil
	} else {
		return manager, err
	}
}

func stopAndVerifyManager(manager *Manager, assert *require.Assertions) {
	manager.Stop(false)

	for _, program := range manager.programs {
		assert.True(program.InTerminalState())
	}
}

func TestSuccessfulProgramLifecycle(t *testing.T) {
	assert := require.New(t)

	manager, err := newManager(`one-success`)
	assert.Nil(err)
	go manager.Run()

	program, ok := manager.Program(`one-success`)
	assert.True(ok)
	assert.NotNil(program)

	time.Sleep(3 * time.Second)
	stopAndVerifyManager(manager, assert)

	assert.Equal([]ProgramState{
		ProgramStarting,
		ProgramRunning,
		ProgramExited,
	}, actualStates)
}

func TestSuccessfulNonDefaultExitCodeProgramLifecycle(t *testing.T) {
	assert := require.New(t)

	manager, err := newManager(`one-success-nondefault-exit-code`)
	assert.Nil(err)
	go manager.Run()

	program, ok := manager.Program(`one-success-exitcode`)
	assert.True(ok)
	assert.NotNil(program)

	time.Sleep(3 * time.Second)
	stopAndVerifyManager(manager, assert)

	assert.Equal([]ProgramState{
		ProgramStarting,
		ProgramRunning,
		ProgramExited,
	}, actualStates)
}

func TestFatalProgramLifecycle(t *testing.T) {
	assert := require.New(t)

	manager, err := newManager(`one-failure`)
	assert.Nil(err)
	go manager.Run()

	program, ok := manager.Program(`one-failure`)
	assert.True(ok)
	assert.NotNil(program)

	time.Sleep(3 * time.Second)
	stopAndVerifyManager(manager, assert)

	assert.Equal([]ProgramState{
		ProgramStarting,
		ProgramRunning,
		ProgramFatal,
	}, actualStates)
}

func TestFatalAutorestartProgramLifecycle(t *testing.T) {
	assert := require.New(t)

	manager, err := newManager(`one-failure-ar`)
	assert.Nil(err)
	go manager.Run()

	program, ok := manager.Program(`one-failure-ar`)
	assert.True(ok)
	assert.NotNil(program)

	time.Sleep(5 * time.Second)
	stopAndVerifyManager(manager, assert)

	assert.Equal([]ProgramState{
		ProgramStarting,
		ProgramBackoff,
		ProgramStarting,
		ProgramBackoff,
		ProgramStarting,
		ProgramBackoff,
		ProgramFatal,
	}, actualStates)
}
