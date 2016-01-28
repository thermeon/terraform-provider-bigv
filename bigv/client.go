package bigv

import (
	"fmt"
	"net/http"
)

const bigvURI = "https://uk0.bigv.io"

type client struct {
	account  string
	group    string
	user     string
	password string
}

func (c *client) uri() string {
	return fmt.Sprintf("%s/accounts/%s/groups/%s",
		bigvURI,
		c.account,
		c.group,
	)
}

func (c *client) do(req *http.Request) (*http.Response, error) {

	client := &http.Client{}

	if resp, err := client.Do(req); err != nil {
		return nil, err
	} else {
		return resp, nil
	}
}
