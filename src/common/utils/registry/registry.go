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

package registry

import (
	"crypto/tls"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	// "time"

	"github.com/goharbor/harbor/src/common/utils"
	registry_error "github.com/goharbor/harbor/src/common/utils/error"
)

// Registry holds information of a registry entity
type Registry struct {
	Endpoint *url.URL
	client   *http.Client
}

var defaultHTTPTransport, secureHTTPTransport, insecureHTTPTransport *http.Transport

// 初始化不同类型传输通道
func init() {
	defaultHTTPTransport = &http.Transport{}

	secureHTTPTransport = &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
	}
	insecureHTTPTransport = &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}
}

// GetHTTPTransport returns HttpTransport based on insecure configuration
func GetHTTPTransport(insecure ...bool) *http.Transport {
	if len(insecure) == 0 {
		return defaultHTTPTransport
	}
	if insecure[0] {
		return insecureHTTPTransport
	}
	return secureHTTPTransport
}

// NewRegistry returns an instance of registry
func NewRegistry(endpoint string, client *http.Client) (*Registry, error) {
	u, err := utils.ParseEndpoint(endpoint)
	if err != nil {
		return nil, err
	}

	registry := &Registry{
		Endpoint: u,
		client:   client,
	}

	return registry, nil
}

// Catalog ...
// 显示仓库的大概信息
func (r *Registry) Catalog() ([]string, error) {
	repos := []string{}
	suffix := "/v2/_catalog?n=1000"
	var url string

	for len(suffix) > 0 {
		url = r.Endpoint.String() + suffix

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return repos, err
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return nil, parseError(err)
		}

		defer resp.Body.Close()
		b, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return repos, err
		}

		if resp.StatusCode == http.StatusOK {
			catalogResp := struct {
				Repositories []string `json:"repositories"`
			}{}

			if err := json.Unmarshal(b, &catalogResp); err != nil {
				return repos, err
			}

			repos = append(repos, catalogResp.Repositories...)
			// Link: </v2/_catalog?last=library%2Fhello-world-25&n=100>; rel="next"
			link := resp.Header.Get("Link")
			if strings.HasSuffix(link, `rel="next"`) && strings.Index(link, "<") >= 0 && strings.Index(link, ">") >= 0 {
				suffix = link[strings.Index(link, "<")+1 : strings.Index(link, ">")]
			} else {
				suffix = ""
			}
		} else {
			return repos, &registry_error.HTTPError{
				StatusCode: resp.StatusCode,
				Detail:     string(b),
			}
		}
	}
	return repos, nil
}

// Ping ...
// 测试 registry 服务是否可用
func (r *Registry) Ping() error {
	req, err := http.NewRequest(http.MethodHead, buildPingURL(r.Endpoint.String()), nil)
	if err != nil {
		return err
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return parseError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	return &registry_error.HTTPError{
		StatusCode: resp.StatusCode,
		Detail:     string(b),
	}
}
