package proxy

import (
	"github.com/goharbor/harbor/src/core/config"

	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
)

// Proxy is the instance of the reverse proxy in this package.
var Proxy *httputil.ReverseProxy

var handlers handlerChain

type handlerChain struct {
	head http.Handler
}

// Init initialize the Proxy instance and handler chain.
func Init(urls ...string) error {
	var err error
	var registryURL string
	if len(urls) > 1 {
		return fmt.Errorf("the parm, urls should have only 0 or 1 elements")
	}
	if len(urls) == 0 {
		// 获取镜像仓库注册中心的地址
		registryURL, err = config.RegistryURL()
		if err != nil {
			return err
		}
	} else {
		registryURL = urls[0]
	}
	targetURL, err := url.Parse(registryURL)
	if err != nil {
		return err
	}
	// 对指定 URL 设置反向代理，重定向路由
	Proxy = httputil.NewSingleHostReverseProxy(targetURL)
	// 将多个 handler 层次调用在一起
	handlers = handlerChain{head: readonlyHandler{next: urlHandler{next: listReposHandler{next: contentTrustHandler{next: vulnerableHandler{next: Proxy}}}}}}
	return nil
}

// Handle handles the request.
// 开启代理,对请求进行修改
func Handle(rw http.ResponseWriter, req *http.Request) {
	handlers.head.ServeHTTP(rw, req)
}
