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

package client

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	common_http "github.com/goharbor/harbor/src/common/http"
	"github.com/goharbor/harbor/src/common/http/modifier/auth"
	"github.com/goharbor/harbor/src/common/utils"
	"github.com/goharbor/harbor/src/common/utils/log"
	"github.com/goharbor/harbor/src/registryctl/api"
)

// Client defines methods that an Registry client should implement
type Client interface {
	// Health tests the connection with registry server
	Health() error
	// StartGC enable the gc of registry server
	StartGC() (*api.GCResult, error)
}

type client struct {
	baseURL string
	client  *common_http.Client
}

// Config contains configurations needed for client
type Config struct {
	Secret string
}

// NewClient return an instance of Registry client
func NewClient(baseURL string, cfg *Config) Client {
	baseURL = strings.TrimRight(baseURL, "/")
	if !strings.Contains(baseURL, "://") {
		baseURL = "http://" + baseURL
	}
	client := &client{
		baseURL: baseURL,
	}
	if cfg != nil {
		// 为内部创建的 docker client 授权，这里的 cfg.Secret 为环境中 JOBSERVICE_SECRET 的值
		authorizer := auth.NewSecretAuthorizer(cfg.Secret)
		// 具体的 client 的创建需要调用 common 包中的NewClient 函数。经过这样的处理的 client 发出去的请求都会被 modifiers 修改，进行验证授权。
		client.client = common_http.NewClient(nil, authorizer)
	}
	return client
}

// Health ...
func (c *client) Health() error {
	addr := strings.Split(c.baseURL, "://")[1]
	if !strings.Contains(addr, ":") {
		addr = addr + ":80"
	}
	return utils.TestTCPConn(addr, 60, 2)
}

// StartGC ...
func (c *client) StartGC() (*api.GCResult, error) {
	// 构建请求地址，发送给 api 接口。
	url := c.baseURL + "/api/registry/gc"
	// 垃圾回收的结果
	gcr := &api.GCResult{}

	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		log.Errorf("Failed to start gc: %d", resp.StatusCode)
		return nil, fmt.Errorf("Failed to start GC: %d", resp.StatusCode)
	}
	// 格式化结果
	if err := json.Unmarshal(data, gcr); err != nil {
		return nil, err
	}

	return gcr, nil
}
