package controllers

import (
	"github.com/astaxie/beego"
	"github.com/goharbor/harbor/src/core/proxy"
)

// RegistryProxy is the endpoint on UI for a reverse proxy pointing to registry
type RegistryProxy struct {
	beego.Controller
}

// Handle is the only entrypoint for incoming requests, all requests must go through this func.
// 所有访问 docker registry 的请求的总入口
func (p *RegistryProxy) Handle() {
	// 将请求从 ctx 中提取出来
	req := p.Ctx.Request
	rw := p.Ctx.ResponseWriter
	// 使用代理进行转发
	proxy.Handle(rw, req)
}

// Render ...
func (p *RegistryProxy) Render() error {
	return nil
}
