package bigv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

const bigvUri = "https://uk0.bigv.io"
const bigvAuthUri = "https://auth.bytemark.co.uk/session"
const bigvTimeout = 20

type client struct {
	account  string
	group    string
	zone     string
	user     string
	password string
	http     *http.Client
	session  string
}

var sessions sync.Mutex

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (c *client) newSession() error {
	l := log.New(os.Stderr, "", 0)

	cr := credentials{
		Username: c.user,
		Password: c.password,
	}

	body, err := json.Marshal(cr)
	if err != nil {
		l.Printf("Error creating json: %s", err)
		return err
	}

	l.Printf("Requesting new session at: %s", bigvAuthUri)
	req, _ := http.NewRequest("POST", bigvAuthUri, bytes.NewBuffer(body))
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Accept", "text/plain")

	if resp, err := c.http.Do(req); err != nil {
		return err
	} else {

		body, _ := ioutil.ReadAll(resp.Body)
		defer resp.Body.Close()

		c.session = string(body)
		l.Printf("Got back session Id: %s", c.session)
	}

	return nil
}

func (c *client) do(req *http.Request) (*http.Response, error) {
	l := log.New(os.Stderr, "", 0)

	if c.http == nil {
		// Initialization
		c.http = &http.Client{
			Timeout: time.Second * bigvTimeout,
		}
	}

	if c.session == "" {
		sessions.Lock()
		// Check again, in case it's been fixed by something we were blocking on
		if c.session == "" {
			if err := c.newSession(); err != nil {
				return nil, err
			}
		}
		sessions.Unlock()
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.session))
	l.Printf("Using Session Id: %s", c.session)

	// We're going to potentially do this again, so we need to copy the body
	var body []byte
	if req.Body != nil {
		body, _ = ioutil.ReadAll(req.Body)
	}

	for i := 0; i < 3; i++ {
		if len(body) > 0 {
			req.Body = ioutil.NopCloser(bytes.NewReader(body))
		}

		resp, err := c.http.Do(req)

		// Either a full error, or a good response
		if err != nil || (resp.StatusCode >= 200 && resp.StatusCode < 500) {
			return resp, err
		}

		// Otherwise we need to massage and deal with auth retries

		if resp.StatusCode == 401 && i == 0 {
			return resp, err
			l.Printf("HTTP 401. Retrying with a new session id")
			time.Sleep(1 * time.Second)
			c.newSession()
			continue
		}

		// Any other http error. Try to get more about it
		body, _ := ioutil.ReadAll(resp.Body)
		defer resp.Body.Close()
		return resp, fmt.Errorf("Bigv returned HTTP Status %d: %s", resp.StatusCode, body)
	}

	return nil, errors.New("Unexpected error in HTTP client, this should not happen")
}
