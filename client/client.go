package client

import (
	"fmt"
	"io/ioutil"

	"github.com/ghetzel/go-stockutil/httputil"
	"github.com/ghetzel/procwatch"
)

var DefaultClientAddress string = `http://localhost:9001`

type Client struct {
	*httputil.Client
}

func NewClient(address string) (*Client, error) {
	if address == `` {
		address = DefaultClientAddress
	}

	if client, err := httputil.NewClient(address); err == nil {
		return &Client{
			Client: client,
		}, nil
	} else {
		return nil, err
	}
}

func (self *Client) ManagerInfo() (*procwatch.Manager, error) {
	if response, err := self.Get(`/api/manager`, nil, nil); err == nil {
		var mgr procwatch.Manager

		if err := self.Decode(response.Body, &mgr); err == nil {
			return &mgr, nil
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}

func (self *Client) GetPrograms() ([]*Program, error) {
	if response, err := self.Get(`/api/programs`, nil, nil); err == nil {
		programs := make([]*procwatch.Program, 0)

		if err := self.Decode(response.Body, &programs); err == nil {
			var rv = make([]*Program, len(programs))

			for i := 0; i < len(rv); i++ {
				rv[i] = &Program{
					Program: programs[i],
				}
			}

			return rv, nil
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}

func (self *Client) GetProgram(name string) (*Program, error) {
	if response, err := self.Get(`/api/programs/`+name, nil, nil); err == nil {
		var program procwatch.Program

		if err := self.Decode(response.Body, &program); err == nil {
			return &Program{
				Program: &program,
			}, nil
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}

func (self *Client) DoProgramAction(name string, action string) error {
	var endpoint = fmt.Sprintf("/api/programs/%v/action/%v", name, action)
	if response, err := self.Put(endpoint, nil, nil, nil); err == nil {
		if response != nil {
			go ioutil.ReadAll(response.Body)
		}
		return nil
	} else {
		return err
	}
}
