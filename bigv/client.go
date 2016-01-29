package bigv

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"
)

const bigvURI = "https://uk0.bigv.io"
const bigvTimeout = 20

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
		Timeout: time.Second * bigvTimeout,
	}

	// We're going to potentially do this again, so we need to copy the body
	body, _ := ioutil.ReadAll(req.Body)

	for i := 0; i < 3; i++ {
		req.Body = ioutil.NopCloser(bytes.NewReader(body))
		resp, err := client.Do(req)
		if err != nil {
			if resp != nil {
				l.Printf("Error HTTP Status: %d", resp.StatusCode)
				for k, v := range resp.Header {
					l.Printf("Header %s: %s", k, v)
				}
			}
			return resp, err
		}

		if resp.StatusCode == 401 {
			return resp, err
			l.Printf("HTTP 401. Retrying. Bigv tends to do this lots")
			time.Sleep(1 * time.Second)
			continue
		}

		if resp.StatusCode == 500 {
			body, _ := ioutil.ReadAll(resp.Body)
			return resp, fmt.Errorf("Bigv returned 500 internal error: %s", body)
		}
	}
	return nil, errors.New("Bigv replied 401 unauthorized too many times. Maybe your credentials really are wrong?")
}
