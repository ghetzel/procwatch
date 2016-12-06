package procwatch

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func newManager(config string) (*Manager, error) {
	manager := NewManager(path.Join(`./tests`, config+`.ini`))
	return manager, manager.Initialize()
}

func TestSuccessfulProgramLifecycle(t *testing.T) {
	assert := require.New(t)

	manager, err := newManager(`one-success`)
	assert.Nil(err)

	program, ok := manager.Program(`program1`)
	assert.True(ok)
	assert.NotNil(program)

}
