package client

import "github.com/ghetzel/procwatch"

type Program struct {
	*procwatch.Program
	client *Client
}

func (self *Program) Start() int {
	self.client.DoProgramAction(self.Name, `start`)
	return 0
}

func (self *Program) Stop() {
	self.client.DoProgramAction(self.Name, `stop`)
}

func (self *Program) Restart() {
	self.client.DoProgramAction(self.Name, `restart`)
}
