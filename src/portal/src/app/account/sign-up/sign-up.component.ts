// Copyright (c) 2017 VMware, Inc. All Rights Reserved.
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
import { Component, Output, ViewChild, EventEmitter } from '@angular/core';
import { Modal } from '@clr/angular';

import { NewUserFormComponent } from '../../shared/new-user-form/new-user-form.component';
import { User } from '../../user/user';
import { SessionService } from '../../shared/session.service';
import { UserService } from '../../user/user.service';
import { InlineAlertComponent } from '../../shared/inline-alert/inline-alert.component';


@Component({
    selector: 'sign-up',
    templateUrl: "sign-up.component.html"
})
export class SignUpComponent {
    opened: boolean = false;
    staticBackdrop: boolean = true;
    error: any;
    onGoing: boolean = false;
    formValueChanged: boolean = false;

    // 发送到 sign-in 父组件，然后 sign-in 组件调用handleUserCreation（）方法将注册的用户名输入到框中。
    @Output() userCreation = new EventEmitter<User>();

    constructor(
        private session: SessionService,
        private userService: UserService) { }

    @ViewChild(NewUserFormComponent)
    newUserForm: NewUserFormComponent;

    @ViewChild(InlineAlertComponent)
    inlienAlert: InlineAlertComponent;

    @ViewChild(Modal)
    modal: Modal;

    getNewUser(): User {
        return this.newUserForm.getData();
    }

    public get inProgress(): boolean {
        return this.onGoing;
    }

    public get isValid(): boolean {
        return this.newUserForm.isValid && this.error == null;
    }

    // 由子组件new-user-form.component.ts发送来的事件触发的，当父组件监听到事件发生时，就调用此函数
    formValueChange(flag: boolean): void {
        if (flag) {
            this.formValueChanged = true;
        }
        if (this.error != null) {
            this.error = null; // clear error
        }
        this.inlienAlert.close(); // Close alert if being shown
    }

    open(): void {
        // Reset state
        this.newUserForm.reset();
        this.formValueChanged = false;
        this.error = null;
        this.onGoing = false;
        this.inlienAlert.close();
         // 弹出模态框
         this.modal.open();
    }

    // 在关闭模态框时，需要检查表单中数据是否有改动。有改动的话会弹出确认表单
    close(): void {
        if (this.formValueChanged) {
            if (this.newUserForm.isEmpty()) {
                this.opened = false;
            } else {
                // Need user confirmation
                this.inlienAlert.showInlineConfirmation({
                    message: "ALERT.FORM_CHANGE_CONFIRMATION"
                });
            }
        } else {
            this.opened = false;
        }
    }

    confirmCancel($event: any): void {
        this.opened = false;
        this.modal.close();
    }

    // Create new user 关键操作，注册新用户
    create(): void {
        // Double confirm everything is ok
        // Form is valid
        if (!this.isValid) {
            return;
        }

        // We have new user data，获取子组件中的注册用户信息存储在 u 中
        let u = this.getNewUser();
        if (!u) {
            return;
        }

        // Start process
        this.onGoing = true;

        this.userService.addUser(u)
            .then(() => {
                this.onGoing = false;
                this.opened = false;
                this.modal.close();
                this.userCreation.emit(u);
            })
            .catch(error => {
                this.onGoing = false;
                this.error = error;
                this.inlienAlert.showInlineError(error);
            });
    }
}
