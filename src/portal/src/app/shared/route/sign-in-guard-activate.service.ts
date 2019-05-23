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
import { CommonRoutes } from '../../shared/shared.const';

@Injectable()
export class SignInGuard implements CanActivate, CanActivateChild {
  constructor(private authService: SessionService, private router: Router) { }

  canActivate(route: ActivatedRouteSnapshot, state: RouterStateSnapshot): Promise<boolean> | boolean {
    // If user has logged in, should not login again
    return new Promise((resolve, reject) => {
      // 判断路由参数中是否有 signout 参数
      let queryParams = route.queryParams;
      if (queryParams && queryParams['signout']) {
        this.authService.signOff() // 用户退出
          .then(() => {
            // 清楚 session 缓存
            this.authService.clear(); // Destroy session cache
            return resolve(true);
          })
          .catch(error => {
            console.error(error);
            return resolve(false);
          });
      } else {
        // 如果能成功获取用户信息，路由跳转到项目页面
        let user = this.authService.getCurrentUser();
        if (user === null) {
          this.authService.retrieveUser()
            .then(() => {
              this.router.navigate([CommonRoutes.HARBOR_DEFAULT]);
              return resolve(false);
            })
            .catch(error => {
              return resolve(true);
            });
        } else {
          this.router.navigate([CommonRoutes.HARBOR_DEFAULT]);
          return resolve(false);
        }
      }
    });
  }

  canActivateChild(route: ActivatedRouteSnapshot, state: RouterStateSnapshot): Promise<boolean> | boolean {
    return this.canActivate(route, state);
  }
}
