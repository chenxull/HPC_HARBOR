// Copyright Project Harbor Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package http

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"

	"github.com/goharbor/harbor/src/common/http/modifier"
)

// Client is a util for common HTTP operations, such Get, Head, Post, Put and Delete.
// Use Do instead if  those methods can not meet your requirement
type Client struct {
	modifiers []modifier.Modifier
	client    *http.Client
}

// NewClient creates an instance of Client.
// Use net/http.Client as the default value if c is nil.
// Modifiers modify the request before sending it.
func NewClient(c *http.Client, modifiers ...modifier.Modifier) *Client {
	client := &Client{
		client: c,
	}
	if client.client == nil {
		client.client = &http.Client{}
	}
	if len(modifiers) > 0 {
		client.modifiers = modifiers
	}
	return client
}

// Do ...
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	for _, modifier := range c.modifiers {
		// 在发送数据前，根据 client 中的修改器对 req 进行修改。具体来说就是对 req 进行验证授权管理
		if err := modifier.Modify(req); err != nil {
			return nil, err
		}
	}

	return c.client.Do(req)
}

// Get ...
func (c *Client) Get(url string, v ...interface{}) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	// 一个 GET 的实际执行，需要调用 DO 来对请求进行修改。
	data, err := c.do(req)
	if err != nil {
		return err
	}

	if len(v) == 0 {
		return nil
	}

	return json.Unmarshal(data, v[0])
}

// Head ...
func (c *Client) Head(url string) error {
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return err
	}
	_, err = c.do(req)
	return err
}

// Post ...
func (c *Client) Post(url string, v ...interface{}) error {
	var reader io.Reader
	if len(v) > 0 {
		data, err := json.Marshal(v[0])
		if err != nil {
			return err
		}

		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(http.MethodPost, url, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	_, err = c.do(req)
	return err
}

// Put ...
func (c *Client) Put(url string, v ...interface{}) error {
	var reader io.Reader
	if len(v) > 0 {
		data := []byte{}
		data, err := json.Marshal(v[0])
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(http.MethodPut, url, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	_, err = c.do(req)
	return err
}

// Delete ...
func (c *Client) Delete(url string) error {
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	_, err = c.do(req)
	return err
}

func (c *Client) do(req *http.Request) ([]byte, error) {
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &Error{
			Code:    resp.StatusCode,
			Message: string(data),
		}
	}

	return data, nil
}
