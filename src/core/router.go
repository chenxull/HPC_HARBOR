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

package main

import (
	"github.com/goharbor/harbor/src/core/api"
	"github.com/goharbor/harbor/src/core/config"
	"github.com/goharbor/harbor/src/core/controllers"
	"github.com/goharbor/harbor/src/core/service/notifications/admin"
	"github.com/goharbor/harbor/src/core/service/notifications/clair"
	"github.com/goharbor/harbor/src/core/service/notifications/jobs"
	"github.com/goharbor/harbor/src/core/service/notifications/registry"
	"github.com/goharbor/harbor/src/core/service/token"

	"github.com/astaxie/beego"
)

func initRouters() {

	// standalone
	if !config.WithAdmiral() {
		// Controller API:
		// 登录
		beego.Router("/c/login", &controllers.CommonController{}, "post:Login")
		// 登出
		beego.Router("/c/log_out", &controllers.CommonController{}, "get:LogOut")
		// 重置密码
		beego.Router("/c/reset", &controllers.CommonController{}, "post:ResetPassword")
		// 检查用户是否存储，在注册用户的时候使用
		beego.Router("/c/userExists", &controllers.CommonController{}, "post:UserExists")
		// 用来发送邮件的 api
		beego.Router("/c/sendEmail", &controllers.CommonController{}, "get:SendResetEmail")

		// API:
		// 给具体某个项目增加成员，对于项目的控制很有用。
		beego.Router("/api/projects/:pid([0-9]+)/members/?:pmid([0-9]+)", &api.ProjectMemberAPI{})
		// Head 检查 project 是否存在，用在创建 project 时候
		beego.Router("/api/projects/", &api.ProjectAPI{}, "head:Head")
		//  用来修改更新单个project 配置的 API
		beego.Router("/api/projects/:id([0-9]+)", &api.ProjectAPI{})

		beego.Router("/api/users/:id", &api.UserAPI{}, "get:Get;delete:Delete;put:Put")
		beego.Router("/api/users", &api.UserAPI{}, "get:List;post:Post")
		beego.Router("/api/users/:id([0-9]+)/password", &api.UserAPI{}, "put:ChangePassword")
		beego.Router("/api/users/:id/sysadmin", &api.UserAPI{}, "put:ToggleUserAdminRole")
		beego.Router("/api/usergroups/?:ugid([0-9]+)", &api.UserGroupAPI{})
		beego.Router("/api/ldap/ping", &api.LdapAPI{}, "post:Ping")
		beego.Router("/api/ldap/users/search", &api.LdapAPI{}, "get:Search")
		beego.Router("/api/ldap/groups/search", &api.LdapAPI{}, "get:SearchGroup")
		beego.Router("/api/ldap/users/import", &api.LdapAPI{}, "post:ImportUser")
		beego.Router("/api/email/ping", &api.EmailAPI{}, "post:Ping")
	}

	// API
	beego.Router("/api/ping", &api.SystemInfoAPI{}, "get:Ping")
	beego.Router("/api/search", &api.SearchAPI{})
	// 获取 ProjectAPI 中定义了对 project 的各种操作。
	beego.Router("/api/projects/", &api.ProjectAPI{}, "get:List;post:Post")
	// 用来获取具体项目的日志信息
	beego.Router("/api/projects/:id([0-9]+)/logs", &api.ProjectAPI{}, "get:Logs")
	beego.Router("/api/projects/:id([0-9]+)/_deletable", &api.ProjectAPI{}, "get:Deletable")
	beego.Router("/api/projects/:id([0-9]+)/metadatas/?:name", &api.MetadataAPI{}, "get:Get")
	beego.Router("/api/projects/:id([0-9]+)/metadatas/", &api.MetadataAPI{}, "post:Post")
	beego.Router("/api/projects/:id([0-9]+)/metadatas/:name", &api.MetadataAPI{}, "put:Put;delete:Delete")

	// 所有对 repository 的操作都是通过RepositoryAPI来进行的。
	// todo 这些 api 都需要弄懂
	beego.Router("/api/repositories", &api.RepositoryAPI{}, "get:Get")
	beego.Router("/api/repositories/scanAll", &api.RepositoryAPI{}, "post:ScanAll")
	beego.Router("/api/repositories/*", &api.RepositoryAPI{}, "delete:Delete;put:Put")
	beego.Router("/api/repositories/*/labels", &api.RepositoryLabelAPI{}, "get:GetOfRepository;post:AddToRepository")
	beego.Router("/api/repositories/*/labels/:id([0-9]+)", &api.RepositoryLabelAPI{}, "delete:RemoveFromRepository")
	beego.Router("/api/repositories/*/tags/:tag", &api.RepositoryAPI{}, "delete:Delete;get:GetTag")
	beego.Router("/api/repositories/*/tags/:tag/labels", &api.RepositoryLabelAPI{}, "get:GetOfImage;post:AddToImage")
	beego.Router("/api/repositories/*/tags/:tag/labels/:id([0-9]+)", &api.RepositoryLabelAPI{}, "delete:RemoveFromImage")
	beego.Router("/api/repositories/*/tags", &api.RepositoryAPI{}, "get:GetTags;post:Retag")
	// 启动镜像扫描任务，后面的 ScanImage 才是正在执行的函数，POST 是请求的类型
	beego.Router("/api/repositories/*/tags/:tag/scan", &api.RepositoryAPI{}, "post:ScanImage")
	// 从 clair 中获取镜像的漏洞信息
	beego.Router("/api/repositories/*/tags/:tag/vulnerability/details", &api.RepositoryAPI{}, "Get:VulnerabilityDetails")
	// 获取镜像的 manifest 数据
	beego.Router("/api/repositories/*/tags/:tag/manifest", &api.RepositoryAPI{}, "get:GetManifests")
	beego.Router("/api/repositories/*/signatures", &api.RepositoryAPI{}, "get:GetSignatures")
	beego.Router("/api/repositories/top", &api.RepositoryAPI{}, "get:GetTopRepos")
	beego.Router("/api/jobs/replication/", &api.RepJobAPI{}, "get:List;put:StopJobs")
	beego.Router("/api/jobs/replication/:id([0-9]+)", &api.RepJobAPI{})
	beego.Router("/api/jobs/replication/:id([0-9]+)/log", &api.RepJobAPI{}, "get:GetLog")
	beego.Router("/api/jobs/scan/:id([0-9]+)/log", &api.ScanJobAPI{}, "get:GetLog")

	beego.Router("/api/system/gc", &api.GCAPI{}, "get:List")
	beego.Router("/api/system/gc/:id", &api.GCAPI{}, "get:GetGC")
	beego.Router("/api/system/gc/:id([0-9]+)/log", &api.GCAPI{}, "get:GetLog")
	beego.Router("/api/system/gc/schedule", &api.GCAPI{}, "get:Get;put:Put;post:Post")

	beego.Router("/api/policies/replication/:id([0-9]+)", &api.RepPolicyAPI{})
	beego.Router("/api/policies/replication", &api.RepPolicyAPI{}, "get:List")
	beego.Router("/api/policies/replication", &api.RepPolicyAPI{}, "post:Post")
	beego.Router("/api/targets/", &api.TargetAPI{}, "get:List")
	beego.Router("/api/targets/", &api.TargetAPI{}, "post:Post")
	beego.Router("/api/targets/:id([0-9]+)", &api.TargetAPI{})
	beego.Router("/api/targets/:id([0-9]+)/policies/", &api.TargetAPI{}, "get:ListPolicies")
	beego.Router("/api/targets/ping", &api.TargetAPI{}, "post:Ping")
	// 获取所有的日志信息。
	beego.Router("/api/logs", &api.LogAPI{})
	beego.Router("/api/configs", &api.ConfigAPI{}, "get:GetInternalConfig")
	beego.Router("/api/configurations", &api.ConfigAPI{})
	beego.Router("/api/configurations/reset", &api.ConfigAPI{}, "post:Reset")
	beego.Router("/api/statistics", &api.StatisticAPI{})
	beego.Router("/api/replications", &api.ReplicationAPI{})
	beego.Router("/api/labels", &api.LabelAPI{}, "post:Post;get:List")
	beego.Router("/api/labels/:id([0-9]+)", &api.LabelAPI{}, "get:Get;put:Put;delete:Delete")
	beego.Router("/api/labels/:id([0-9]+)/resources", &api.LabelAPI{}, "get:ListResources")

	beego.Router("/api/systeminfo", &api.SystemInfoAPI{}, "get:GetGeneralInfo")
	beego.Router("/api/systeminfo/volumes", &api.SystemInfoAPI{}, "get:GetVolumeInfo")
	beego.Router("/api/systeminfo/getcert", &api.SystemInfoAPI{}, "get:GetCert")

	// 将 registry 中的 repository 数据同步到数据库中
	beego.Router("/api/internal/syncregistry", &api.InternalAPI{}, "post:SyncRegistry")
	// 重新命名
	beego.Router("/api/internal/renameadmin", &api.InternalAPI{}, "post:RenameAdmin")
	beego.Router("/api/internal/configurations", &api.ConfigAPI{}, "get:GetInternalConfig")

	// external service that hosted on harbor process:
	// /service/notifications 用于镜像上传时的通知服务
	beego.Router("/service/notifications", &registry.NotificationHandler{})
	// 启动镜像扫描任务
	beego.Router("/service/notifications/clair", &clair.Handler{}, "post:Handle")
	// jobservice 中的 hook_url 进行访问，用来更新数据库中 job 的状态信息
	beego.Router("/service/notifications/jobs/scan/:id([0-9]+)", &jobs.Handler{}, "post:HandleScan")
	beego.Router("/service/notifications/jobs/replication/:id([0-9]+)", &jobs.Handler{}, "post:HandleReplication")
	// 用来更新数据库中 job service 的工作状态 ，主要涉及到 admin job 表
	beego.Router("/service/notifications/jobs/adminjob/:id([0-9]+)", &admin.Handler{}, "post:HandleAdminJob")
	// 获取 token 信息
	beego.Router("/service/token", &token.Handler{})
	// 所有方法 registry 的请求都会被 core 组件进行处理
	beego.Router("/v2/*", &controllers.RegistryProxy{}, "*:Handle")

	// APIs for chart repository
	if config.WithChartMuseum() {
		// Charts are controlled under projects
		chartRepositoryAPIType := &api.ChartRepositoryAPI{}
		beego.Router("/api/chartrepo/health", chartRepositoryAPIType, "get:GetHealthStatus")
		beego.Router("/api/chartrepo/:repo/charts", chartRepositoryAPIType, "get:ListCharts")
		beego.Router("/api/chartrepo/:repo/charts/:name", chartRepositoryAPIType, "get:ListChartVersions")
		beego.Router("/api/chartrepo/:repo/charts/:name", chartRepositoryAPIType, "delete:DeleteChart")
		beego.Router("/api/chartrepo/:repo/charts/:name/:version", chartRepositoryAPIType, "get:GetChartVersion")
		beego.Router("/api/chartrepo/:repo/charts/:name/:version", chartRepositoryAPIType, "delete:DeleteChartVersion")
		beego.Router("/api/chartrepo/:repo/charts", chartRepositoryAPIType, "post:UploadChartVersion")
		beego.Router("/api/chartrepo/:repo/prov", chartRepositoryAPIType, "post:UploadChartProvFile")
		beego.Router("/api/chartrepo/charts", chartRepositoryAPIType, "post:UploadChartVersion")

		// Repository services
		beego.Router("/chartrepo/:repo/index.yaml", chartRepositoryAPIType, "get:GetIndexByRepo")
		beego.Router("/chartrepo/index.yaml", chartRepositoryAPIType, "get:GetIndex")
		beego.Router("/chartrepo/:repo/charts/:filename", chartRepositoryAPIType, "get:DownloadChart")

		// Labels for chart
		chartLabelAPIType := &api.ChartLabelAPI{}
		beego.Router("/api/chartrepo/:repo/charts/:name/:version/labels", chartLabelAPIType, "get:GetLabels;post:MarkLabel")
		beego.Router("/api/chartrepo/:repo/charts/:name/:version/labels/:id([0-9]+)", chartLabelAPIType, "delete:RemoveLabel")
	}

	// Error pages
	beego.ErrorController(&controllers.ErrorController{})

}
