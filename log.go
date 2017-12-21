package procwatch

import (
	"io"
	"strings"
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
