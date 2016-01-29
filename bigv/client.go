package bigv

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
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
	l := log.New(os.Stderr, "", 0)

	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.user, c.password)

	client := &http.Client{
		Timeout: time.Second * 20,
	}

	for i := 0; i < 3; i++ {
		if resp, err := client.Do(req); err != nil {
			return nil, err
		} else {
			// BigV has a bad habbit of doing 401s all the time
			if resp.StatusCode == 401 {
				l.Printf("HTTP 401. Retrying. Bigv tends to do this lots")
				time.Sleep(1 * time.Second)
			} else {
				return resp, nil
			}
		}
	}
	return nil, errors.New("Bigv replied 401 unauthorized too many times. Maybe your credentials really are wrong?")
}
