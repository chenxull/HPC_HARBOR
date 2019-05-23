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
import { NgModule } from '@angular/core';
import { RouterModule, Routes } from '@angular/router';

import { SystemAdminGuard } from './shared/route/system-admin-activate.service';
import { AuthCheckGuard } from './shared/route/auth-user-activate.service';
import { SignInGuard } from './shared/route/sign-in-guard-activate.service';
import { MemberGuard } from './shared/route/member-guard-activate.service';

import { PageNotFoundComponent } from './shared/not-found/not-found.component';
import { HarborShellComponent } from './base/harbor-shell/harbor-shell.component';
import { ConfigurationComponent } from './config/config.component';

import { UserComponent } from './user/user.component';
import { SignInComponent } from './account/sign-in/sign-in.component';
import { ResetPasswordComponent } from './account/password-setting/reset-password/reset-password.component';
import { GroupComponent } from './group/group.component';

import { TotalReplicationPageComponent } from './replication/total-replication/total-replication-page.component';
import { DestinationPageComponent } from './replication/destination/destination-page.component';
import { ReplicationPageComponent } from './replication/replication-page.component';

import { AuditLogComponent } from './log/audit-log.component';
import { LogPageComponent } from './log/log-page.component';

import { RepositoryPageComponent } from './repository/repository-page.component';
import { TagRepositoryComponent } from './repository/tag-repository/tag-repository.component';
import { TagDetailPageComponent } from './repository/tag-detail/tag-detail-page.component';
import { LeavingRepositoryRouteDeactivate } from './shared/route/leaving-repository-deactivate.service';

import { ProjectComponent } from './project/project.component';
import { ProjectDetailComponent } from './project/project-detail/project-detail.component';
import { MemberComponent } from './project/member/member.component';
import {ProjectLabelComponent} from "./project/project-label/project-label.component";
import { ProjectConfigComponent } from './project/project-config/project-config.component';
import { ProjectRoutingResolver } from './project/project-routing-resolver.service';
import { ListChartsComponent } from './project/list-charts/list-charts.component';
import { ListChartVersionsComponent } from './project/list-chart-versions/list-chart-versions.component';
import { ChartDetailComponent } from './project/chart-detail/chart-detail.component';

const harborRoutes: Routes = [
  { path: '', redirectTo: 'harbor', pathMatch: 'full' },
  { path: 'reset_password', component: ResetPasswordComponent },
  {
    path: 'harbor',
    component: HarborShellComponent,
    // 授权检查，就是检查是否能从 session 服务中获取当前用户信息
    canActivateChild: [AuthCheckGuard], // 必须满足这个条件才能启动 子路由。条件为：获取当前用户信息
    children: [
      { path: '', redirectTo: 'sign-in', pathMatch: 'full' },
      {
        path: 'sign-in',
        component: SignInComponent,
        canActivate: [SignInGuard] // 路由守卫，验证此路由能否激活。当成功登录可以获取用户信息时，此路由失效跳转到 projects 中。 在路由中检测到 signout 时，激活组件
      },
      {
        // 登录后显示的第一个页面
        path: 'projects',
        component: ProjectComponent
      },
      {
        // 查看日志，没有任何路由守卫
        path: 'logs',
        component: LogPageComponent
      },
      {
        path: 'users',
        component: UserComponent,
        canActivate: [SystemAdminGuard]  // 判断用户是否为系统管理员,只有系统管理员才能够访问这些组件
      },
      {
        path: 'groups',
        component: GroupComponent,
        canActivate: [SystemAdminGuard]
      },
      {
        path: 'registries',
        component: DestinationPageComponent,
        canActivate: [SystemAdminGuard]
      },
      {
        path: 'replications',
        component: TotalReplicationPageComponent,
        canActivate: [SystemAdminGuard],
        canActivateChild: [SystemAdminGuard],
      },
      {
        path: 'tags/:id/:repo',
        component: TagRepositoryComponent,
        canActivate: [MemberGuard], // 用来检查用户是否在此 project 的成员中。只要在此成员中，才可以对其进行操作访问
        // resolve 在进入路由之前去服务器读数据，把需要的数据都读好以后，带着这些数据进到路由里，立刻就把数据显示出来。
        resolve: {
          projectResolver: ProjectRoutingResolver
        }
      },
      {
        path: 'projects/:id/repositories/:repo',
        component: TagRepositoryComponent,
        canActivate: [MemberGuard],
        // 离开时候的路由守卫。提醒用户执行保存操作后才能离开
        canDeactivate: [LeavingRepositoryRouteDeactivate],
        resolve: {
          projectResolver: ProjectRoutingResolver
        }
      },
      {
        path: 'projects/:id/repositories/:repo/tags/:tag',
        component: TagDetailPageComponent,
        canActivate: [MemberGuard],
        resolve: {
          projectResolver: ProjectRoutingResolver
        },
      },
      {
        path: 'projects/:id/helm-charts/:chart/versions',
        component: ListChartVersionsComponent,
        canActivate: [MemberGuard],
        resolve: {
          projectResolver: ProjectRoutingResolver
        },
      },
      {
        path: 'projects/:id/helm-charts/:chart/versions/:version',
        component: ChartDetailComponent,
        canActivate: [MemberGuard],
        resolve: {
          projectResolver: ProjectRoutingResolver
        },
      },
      {
        path: 'projects/:id',
        component: ProjectDetailComponent,
        canActivate: [MemberGuard], // 用来检查用户是否在此 project 的成员中。只要在此成员中，才可以对其进行操作访问
        resolve: {
          projectResolver: ProjectRoutingResolver // 检查当前用户的成员类型，系统管理员 or 项目管理员 或则其他的身份
        },
        children: [
          {
            path: 'repositories', // 显示此project 下有多少镜像存储库
            component: RepositoryPageComponent
          },
          {
            path: 'helm-charts',
            component: ListChartsComponent
          },
          {
            path: 'repositories/:repo/tags',  // 对指定的镜像镜像进行一步操作，如扫描，复制摘要等
            component: TagRepositoryComponent,
          },
          {
            path: 'replications',
            component: ReplicationPageComponent,
          },
          {
            path: 'members',
            component: MemberComponent
          },
          {
            path: 'logs',
            component: AuditLogComponent
          },
          {
            path: 'labels',
            component: ProjectLabelComponent
          },
          {
            path: 'configs',
            component: ProjectConfigComponent
          }
        ]
      },
      {
        path: 'configs',
        component: ConfigurationComponent,
        canActivate: [SystemAdminGuard]
      },
      {
        path: 'registry',
        component: DestinationPageComponent,
        canActivate: [SystemAdminGuard],
        canActivateChild: [SystemAdminGuard],
      }
    ]
  },
  { path: "**", component: PageNotFoundComponent }
];

@NgModule({
  imports: [
    RouterModule.forRoot(harborRoutes)
  ],
  exports: [RouterModule]
})
export class HarborRoutingModule {

}
