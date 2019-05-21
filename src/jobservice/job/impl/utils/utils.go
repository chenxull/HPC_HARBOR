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

package utils

import (
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/docker/distribution/registry/auth/token"
	httpauth "github.com/goharbor/harbor/src/common/http/modifier/auth"
	"github.com/goharbor/harbor/src/common/utils/registry"
	"github.com/goharbor/harbor/src/common/utils/registry/auth"
)

// 用来访问 core 中数据的client
var coreClient *http.Client
var mutex = &sync.Mutex{}

// NewRepositoryClient creates a repository client with standard token authorizer
// 没有使用
func NewRepositoryClient(endpoint string, insecure bool, credential auth.Credential,
	tokenServiceEndpoint, repository string) (*registry.Repository, error) {

	transport := registry.GetHTTPTransport(insecure)

	authorizer := auth.NewStandardTokenAuthorizer(&http.Client{
		Transport: transport,
	}, credential, tokenServiceEndpoint)

	uam := &UserAgentModifier{
		UserAgent: "harbor-registry-client",
	}

	return registry.NewRepository(repository, endpoint, &http.Client{
		Transport: registry.NewTransport(transport, authorizer, uam),
	})
}

// NewRepositoryClientForJobservice creates a repository client that can only be used to
// access the internal registry
// 创建访问存储库的client
func NewRepositoryClientForJobservice(repository, internalRegistryURL, secret, internalTokenServiceURL string) (*registry.Repository, error) {
	// 默认的传输通道
	transport := registry.GetHTTPTransport()
	// clair 的授权信息，凭证
	credential := httpauth.NewSecretAuthorizer(secret)
	// 使用刚刚构造的默认 http 传输通道以及授权信息。以及内部的 token 服务服务器 来构造授权器。
	authorizer := auth.NewStandardTokenAuthorizer(&http.Client{
		Transport: transport,
	}, credential, internalTokenServiceURL)

	uam := &UserAgentModifier{
		UserAgent: "harbor-registry-client",
	}

	// 使用上述信息构造 内部registry 的访问客户端。
	return registry.NewRepository(repository, internalRegistryURL, &http.Client{
		Transport: registry.NewTransport(transport, authorizer, uam),
	})
}

// UserAgentModifier adds the "User-Agent" header to the request
type UserAgentModifier struct {
	UserAgent string
}

// Modify adds user-agent header to the request
func (u *UserAgentModifier) Modify(req *http.Request) error {
	req.Header.Set(http.CanonicalHeaderKey("User-Agent"), u.UserAgent)
	return nil
}

// BuildBlobURL ...
// 构建访问 Blob的访问的地址
func BuildBlobURL(endpoint, repository, digest string) string {
	return fmt.Sprintf("%s/v2/%s/blobs/%s", endpoint, repository, digest)
}

// GetTokenForRepo is used for job handler to get a token for clair.
// 为 clair 访问 registry 提供 token信息。
func GetTokenForRepo(repository, secret, internalTokenServiceURL string) (string, error) {
	// 创建凭证
	credential := httpauth.NewSecretAuthorizer(secret)

	t, err := auth.GetToken(internalTokenServiceURL, false, credential,
		[]*token.ResourceActions{{  // scopes 信息，就是对 registry 中的资源访问权限
			Type:    "repository",
			Name:    repository,
			Actions: []string{"pull"},
		}})
	if err != nil {
		return "", err
	}

	return t.Token, nil
}

// GetClient returns the HTTP client that will attach jobService secret to the request, which can be used for
// accessing Harbor's Core Service.
// This function returns error if the secret of Job service is not set.
// 创建HTTP client其可以给发往 Core Service 的请求附带上 jobService secret的信息，主要是用来访问内部的 registry 的。
// 访问 registry 的 client
func GetClient() (*http.Client, error) {
	mutex.Lock()
	defer mutex.Unlock()
	if coreClient == nil {
		// 从系统环境变量中获取 JOBSERVICE_SECRET 信息
		secret := os.Getenv("JOBSERVICE_SECRET")
		if len(secret) == 0 {
			return nil, fmt.Errorf("unable to load secret for job service")
		}
		//Secret Authorizer 请求修改器
		modifier := httpauth.NewSecretAuthorizer(secret)
		// 给访问 core中的 registry 的 client 创建传输通道以及请求修改器
		coreClient = &http.Client{Transport: registry.NewTransport(&http.Transport{}, modifier)}
	}
	return coreClient, nil
}
