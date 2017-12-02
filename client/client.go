package client

import (
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

func (self *Client) GetPrograms() ([]*procwatch.Program, error) {
	if response, err := self.Get(`/api/programs`, nil, nil); err == nil {
		programs := make([]*procwatch.Program, 0)

		if err := self.Decode(response.Body, &programs); err == nil {
			return programs, nil
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}
