package procwatch

import (
	"io"
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
	if self.IsErrorStream {
		log.Errorf("[%s] LOG: %s", self.Program.Name, string(p[:]))
	} else {
		log.Infof("[%s] LOG: %s", self.Program.Name, string(p[:]))
	}

	return len(p), nil
}
