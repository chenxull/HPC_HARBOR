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

package auth

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/url"

	"github.com/docker/distribution/registry/auth/token"
	"github.com/goharbor/harbor/src/common/models"
	registry_error "github.com/goharbor/harbor/src/common/utils/error"
	"github.com/goharbor/harbor/src/common/utils/registry"
)

const (
	service = "harbor-registry"
)

// GetToken requests a token against the endpoint using credetial provided
func GetToken(endpoint string, insecure bool, credential Credential,
	scopes []*token.ResourceActions) (*models.Token, error) {
	client := &http.Client{
		Transport: registry.GetHTTPTransport(insecure),
	}

	return getToken(client, credential, endpoint, service, scopes)
}

func getToken(client *http.Client, credential Credential, realm, service string,
	scopes []*token.ResourceActions) (*models.Token, error) {
	//	 u 是 token service 的访问地址
	u, err := url.Parse(realm)
	if err != nil {
		return nil, err
	}
	query := u.Query()
	// 加上访问 token service 的参数，这些参数不能出错
	query.Add("service", service)
	for _, scope := range scopes {
		query.Add("scope", scopeString(scope))
	}
	u.RawQuery = query.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	// 如果设置有凭证，给 request 加 Authorization 信息
	if credential != nil {
		credential.Modify(req)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 获取 token 数据
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &registry_error.HTTPError{
			StatusCode: resp.StatusCode,
			Detail:     string(data),
		}
	}

	token := &models.Token{}
	if err = json.Unmarshal(data, token); err != nil {
		return nil, err
	}

	return token, nil
}
