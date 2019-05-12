// Copyright 2018 Project Harbor Authors
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

package token

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/docker/distribution/registry/auth/token"
	"github.com/goharbor/harbor/src/common/models"
	"github.com/goharbor/harbor/src/common/security"
	"github.com/goharbor/harbor/src/common/utils/log"
	"github.com/goharbor/harbor/src/core/config"
	"github.com/goharbor/harbor/src/core/filter"
	"github.com/goharbor/harbor/src/core/promgr"
)

var creatorMap map[string]Creator
var registryFilterMap map[string]accessFilter
var notaryFilterMap map[string]accessFilter

const (
	// Notary service
	Notary = "harbor-notary"
	// Registry service
	Registry = "harbor-registry"
)

// InitCreators initialize the token creators for different services
func InitCreators() {
	// creatorMap
	creatorMap = make(map[string]Creator)
	registryFilterMap = map[string]accessFilter{
		"repository": &repositoryFilter{ // 存储库 用来存储镜像实体的
			parser: &basicParser{},
		},
		"registry": &registryFilter{}, // 注册表 用来记录镜像信息的
	}
	// 获取外部的URL: host:port。 这个外部 url 就是 notary 的服务器
	ext, err := config.ExtURL()
	if err != nil {
		log.Warningf("Failed to get ext url, err: %v, the token service will not be functional with notary requests", err)
	} else {
		notaryFilterMap = map[string]accessFilter{
			"repository": &repositoryFilter{
				parser: &endpointParser{
					endpoint: ext,
				},
			},
		}
		creatorMap[Notary] = &generalCreator{
			service:   Notary,
			filterMap: notaryFilterMap,
		}
	}

	// 将 registry 服务的 token 数据存储在creatorMap 中
	creatorMap[Registry] = &generalCreator{
		service:   Registry,
		filterMap: registryFilterMap,
	}
}

// Creator creates a token ready to be served based on the http request.
// 为 http 请求创建 token 服务
type Creator interface {
	// 为 docker pull/push 创建 token 信息
	Create(r *http.Request) (*models.Token, error)
}

// 解析请求中关于镜像的信息。
type imageParser interface {
	parse(s string) (*image, error)
}

// 镜像的命名空间，存储仓库，标签
type image struct {
	namespace string
	repo      string
	tag       string
}

//
type basicParser struct{}

func (b basicParser) parse(s string) (*image, error) {
	return parseImg(s)
}

type endpointParser struct {
	endpoint string
}

func (e endpointParser) parse(s string) (*image, error) {
	repo := strings.SplitN(s, "/", 2)
	if len(repo) < 2 {
		return nil, fmt.Errorf("Unable to parse image from string: %s", s)
	}
	// 检查 harbor 上是否有对应的命名空间
	if repo[0] != e.endpoint {
		return nil, fmt.Errorf("Mismatch endpoint from string: %s, expected endpoint: %s", s, e.endpoint)
	}
	// 构建出镜像结构体基本信息
	return parseImg(repo[1])
}

// build Image accepts a string like library/ubuntu:14.04 and build a image struct
// 从接受到构建镜像的内容中，构建出镜像的数据结构
func parseImg(s string) (*image, error) {
	repo := strings.SplitN(s, "/", 2)
	if len(repo) < 2 {
		return nil, fmt.Errorf("Unable to parse image from string: %s", s)
	}
	//ubuntu:14.04
	i := strings.SplitN(repo[1], ":", 2)
	res := &image{
		namespace: repo[0],
		repo:      i[0],
	}
	if len(i) == 2 {
		res.tag = i[1]
	}
	return res, nil
}

// An accessFilter will filter access based on userinfo
//当使用 pull/push 指令时，根据用户权限对访问进行过滤
type accessFilter interface {
	filter(ctx security.Context, pm promgr.ProjectManager, a *token.ResourceActions) error
}

type registryFilter struct {
}

func (reg registryFilter) filter(ctx security.Context, pm promgr.ProjectManager,
	a *token.ResourceActions) error {
	// Do not filter if the request is to access registry catalog
	// 访问 catelog 是不需要过滤
	if a.Name != "catalog" {
		return fmt.Errorf("Unable to handle, type: %s, name: %s", a.Type, a.Name)
	}
	if !ctx.IsSysAdmin() {
		// Set the actions to empty is the user is not admin
		a.Actions = []string{}
	}
	return nil
}

// repositoryFilter filters the access based on Harbor's permission model
// 不同用户对项目具有不同的权限。
type repositoryFilter struct {
	parser imageParser
}

func (rep repositoryFilter) filter(ctx security.Context, pm promgr.ProjectManager,
	a *token.ResourceActions) error {
	// clear action list to assign to new acess element after perm check.
	img, err := rep.parser.parse(a.Name)
	if err != nil {
		return err
	}
	// project 的 namespace 就是项目名。通过项目名的方式，对镜像资源进行隔离。
	project := img.namespace
	permission := ""

	exist, err := pm.Exists(project)
	if err != nil {
		return err
	}
	if !exist {
		log.Debugf("project %s does not exist, set empty permission", project)
		a.Actions = []string{}
		return nil
	}

	// 检查这个用户对这个项目具有什么样的权限。因为在之前的处理中，ctx 中包含了用户和项目管理器的信息。
	if ctx.HasAllPerm(project) {
		permission = "RWM"
	} else if ctx.HasWritePerm(project) {
		permission = "RW"
	} else if ctx.HasReadPerm(project) {
		permission = "R"
	}

	// 将 pull/push 权限 授权给用户,根据上述的 RWM 给用户分配 pull/push 权限。
	a.Actions = permToActions(permission)
	return nil
}

type generalCreator struct {
	service   string
	filterMap map[string]accessFilter
}

type unauthorizedError struct{}

func (e *unauthorizedError) Error() string {
	return "Unauthorized"
}

func (g generalCreator) Create(r *http.Request) (*models.Token, error) {
	var err error
	// 获取请求中的 scopes；scope="repository:samalba/my-app:pull,push"
	scopes := parseScopes(r.URL)
	log.Debugf("scopes: %v", scopes)

	// 获取 securitycontext
	ctx, err := filter.GetSecurityContext(r)
	if err != nil {
		return nil, fmt.Errorf("failed to  get security context from request")
	}
	// 获取 pm
	pm, err := filter.GetProjectManager(r)
	if err != nil {
		return nil, fmt.Errorf("failed to  get project manager from request")
	}

	// for docker login
	if !ctx.IsAuthenticated() {
		if len(scopes) == 0 {
			return nil, &unauthorizedError{}
		}
	}
	// 通过 scopes 来判断用户对镜像仓库的资源有何种访问权限；pull,push
	access := GetResourceActions(scopes)
	// 迭代资源操作列表，并尝试使用与资源类型匹配的筛选器来筛选操作。执行的结果信息都保存在 ctx 中。
	err = filterAccess(access, ctx, pm, g.filterMap)
	if err != nil {
		return nil, err
	}
	// 在 token 服务器上获取对资源的操作权限后，开始生成 token。生成完成之后返回给 docker client
	return MakeToken(ctx.GetUsername(), g.service, access)
}

func parseScopes(u *url.URL) []string {
	var sector string
	var result []string
	for _, sector = range u.Query()["scope"] {
		result = append(result, strings.Split(sector, " ")...)
	}
	return result
}
