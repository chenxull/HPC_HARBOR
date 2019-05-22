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
import { Injectable } from '@angular/core';
import {
  CanActivate, Router,
  ActivatedRouteSnapshot,
  RouterStateSnapshot,
  CanActivateChild
} from '@angular/router';
import { SessionService } from '../../shared/session.service';
import { ProjectService } from '../../project/project.service';
import { CommonRoutes } from '../../shared/shared.const';

/*
* 不同角色对项目的操作权限不一样，开启的功能也不相同。这相当于访问控制的实现
* */
@Injectable()
export class MemberGuard implements CanActivate, CanActivateChild {
  constructor(
    private sessionService: SessionService,
    private projectService: ProjectService,
    private router: Router) {}

  canActivate(route: ActivatedRouteSnapshot, state: RouterStateSnapshot): Promise<boolean> | boolean {
    // 解析project id
    let projectId = route.params['id'];
    this.sessionService.setProjectMembers([]);
    return new Promise((resolve, reject) => {
      let user = this.sessionService.getCurrentUser();
      if (user === null) {
        this.sessionService.retrieveUser()
        .then(() => resolve(this.checkMemberStatus(state.url, projectId)))
        .catch(() => {
          this.router.navigate([CommonRoutes.HARBOR_DEFAULT]);
          resolve(false);
        });
      } else {
        return resolve(this.checkMemberStatus(state.url, projectId));
      }
    });
  }
  // 检查当前用户状态
  checkMemberStatus(url: string, projectId: number): Promise<boolean> {
    return new Promise<boolean>((resolve, reject) => {
      // 检查此 project 有用户成员
      this.projectService.checkProjectMember(projectId)
      .subscribe(res => {
        // 将获取到的结果存储在 session 的 projectMembers 中
        this.sessionService.setProjectMembers(res);
        return resolve(true);
      },
      //  此用户不在 project 成员中
      () => {
        // Add exception for repository in project detail router activation.
        this.projectService.getProject(projectId).subscribe(project => {
          if (project.public === 1) {
            return resolve(true);
          }
          this.router.navigate([CommonRoutes.HARBOR_DEFAULT]);
          return resolve(false);
        },
        () => {
          this.router.navigate([CommonRoutes.HARBOR_DEFAULT]);
          return resolve(false);
        });
      });
    });
  }

  canActivateChild(route: ActivatedRouteSnapshot, state: RouterStateSnapshot): Promise<boolean> | boolean {
    return this.canActivate(route, state);
  }
}
