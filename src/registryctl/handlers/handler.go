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

package handlers

import (
	"net/http"
	"os"

	"github.com/goharbor/harbor/src/common/utils/log"
	"github.com/goharbor/harbor/src/registryctl/auth"
	gorilla_handlers "github.com/gorilla/handlers"
)

// NewHandlerChain returns a gorilla router which is wrapped by  authenticate handler
// and logging handler
func NewHandlerChain() http.Handler {
	// 创建路由处理方法
	h := newRouter()
	secrets := map[string]string{
		"jobSecret": os.Getenv("JOBSERVICE_SECRET"),
	}
	insecureAPIs := map[string]bool{
		"/api/health": true,
	}
	// 创建经过授权的请求，NewSecretHandler 函数将基础的授权信息存入到请求中
	h = newAuthHandler(auth.NewSecretHandler(secrets), h, insecureAPIs)
	// 将上述 handler 和日志绑定在一起
	h = gorilla_handlers.LoggingHandler(os.Stdout, h)
	return h
}

// 实际传送的请求
type authHandler struct {
	authenticator auth.AuthenticationHandler
	handler       http.Handler
	insecureAPIs  map[string]bool
}

func newAuthHandler(authenticator auth.AuthenticationHandler, handler http.Handler, insecureAPIs map[string]bool) http.Handler {
	return &authHandler{
		authenticator: authenticator,
		handler:       handler,
		insecureAPIs:  insecureAPIs,
	}
}

func (a *authHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if a.authenticator == nil {
		log.Errorf("No authenticator found in registry controller.")
		http.Error(w, http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError)
		return
	}

	// 请求已经经过授权并带有insecureAPIs信息，直接提供服务
	if a.insecureAPIs != nil && a.insecureAPIs[r.URL.Path] {
		if a.handler != nil {
			a.handler.ServeHTTP(w, r)
		}
		return
	}

	// 对请求进行授权，大致思路就是获取请求的Authorization字段，判断其 value 是否存在于secretHandler的secrets字段中。
	err := a.authenticator.AuthorizeRequest(r)
	if err != nil {
		log.Errorf("failed to authenticate request: %v", err)
		http.Error(w, http.StatusText(http.StatusUnauthorized),
			http.StatusUnauthorized)
		return
	}

	if a.handler != nil {
		a.handler.ServeHTTP(w, r)
	}
	return
}
