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
import { SharedModule } from '../shared/shared.module';
import { UserComponent } from './user.component';
import { NewUserModalComponent } from './new-user-modal.component';
import { UserService } from './user.service';
import {ChangePasswordComponent} from "./change-password/change-password.component";

// user 模块中有修改密码和创建新用户的功能。
// 用户管理的显示界面
@NgModule({
  imports: [
    SharedModule
  ],
  declarations: [
    UserComponent,
    ChangePasswordComponent,
    NewUserModalComponent
  ],
  exports: [
    UserComponent
  ],
  // 对整个模块注入 用户服务,基本上对用户的操作都在这个服务中实现
  providers: [UserService]
})
export class UserModule {

}
