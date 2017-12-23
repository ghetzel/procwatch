package procwatch

import (
	"io"
	"strings"

	logging "github.com/op/go-logging"
)

type LogIntercept struct {
	io.Writer
	Program       *Program
	Manager       *Manager
	IsErrorStream bool
}

func NewLogIntercept(program *Program, isErrorStream bool) *LogIntercept {
	return &LogIntercept{
		Program:       program,
		Manager:       program.manager,
		IsErrorStream: isErrorStream,
	}
}

func (self *LogIntercept) Write(p []byte) (int, error) {
	for _, line := range strings.Split(string(p), "\n") {
		if self.IsErrorStream {
			log.Errorf("[%s] LOG: %s", self.Program.Name, line)
		} else {
			log.Infof("[%s] LOG: %s", self.Program.Name, line)
		}
	}

	return len(p), nil
}

type NullBackend struct {
	level logging.Level
}

func NewNullBackend() *NullBackend {
	return &NullBackend{}
}

func (self *NullBackend) GetLevel(module string) logging.Level {
	return self.level
}

func (self *NullBackend) SetLevel(lvl logging.Level, module string) {
	self.level = lvl
}

func (self *NullBackend) IsEnabledFor(lvl logging.Level, module string) bool {
	return true
}

func (self *NullBackend) Log(lvl logging.Level, depth int, record *logging.Record) error {
	return nil
}
