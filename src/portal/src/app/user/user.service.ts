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
import { Injectable } from '@angular/core';
import { Http } from '@angular/http';


import {HTTP_JSON_OPTIONS, HTTP_GET_OPTIONS} from "../shared/shared.utils";
import { User, LDAPUser } from './user';
import LDAPUsertoUser from './user';

const userMgmtEndpoint = '/api/users';
const ldapUserEndpoint = '/api/ldap/users';

/**
 * Define related methods to handle account and session corresponding things
 *
 **
 * class SessionService
 */
@Injectable()
export class UserService {

    constructor(private http: Http) { }

    // Handle the related exceptions
    handleError(error: any): Promise<any> {
        return Promise.reject(error.message || error);
    }

    // Get the user list
    // 向后端发送请求，这是一个异步请求。then 是对返回数据的处理
    getUsers(): Promise<User[]> {
        return this.http.get(userMgmtEndpoint, HTTP_GET_OPTIONS)
            .toPromise()
            .then(response => response.json() as User[]) // 直接将返回的jsob 格式的数据转化为 user 类型
            .catch(error => this.handleError(error));
    }

    // Add new user，由 sign-up 组件调用，向数据库中增加新用户。
    addUser(user: User): Promise<any> {
        return this.http.post(userMgmtEndpoint, JSON.stringify(user), HTTP_JSON_OPTIONS).toPromise()
            .then(() => null)
            .catch(error => this.handleError(error));
    }

    // Delete the specified user
    deleteUser(userId: number): Promise<any> {
        return this.http.delete(userMgmtEndpoint + "/" + userId, HTTP_JSON_OPTIONS)
            .toPromise()
            .then(() => null)
            .catch(error => this.handleError(error));
    }

    // Update user to enable/disable the admin role
    updateUser(user: User): Promise<any> {
        return this.http.put(userMgmtEndpoint + "/" + user.user_id, JSON.stringify(user), HTTP_JSON_OPTIONS)
            .toPromise()
            .then(() => null)
            .catch(error => this.handleError(error));
    }

    // Set user admin role
    // 设置指定用户角色身份为管理员
    updateUserRole(user: User): Promise<any> {
        return this.http.put(userMgmtEndpoint + "/" + user.user_id + "/sysadmin", JSON.stringify(user), HTTP_JSON_OPTIONS)
            .toPromise()
            .then(() => null)
            .catch(error => this.handleError(error));
    }

    // admin change normal user pwd
    // 没有携带有自己的账号密码。在访问后端的时候，后端是如何确认其身份的？
    changePassword(uid: number, newPassword: string, confirmPwd: string): Promise<any> {
        if (!uid || !newPassword) {
            return Promise.reject("Invalid change uid or password");
        }
        // 后端 API：/api/users/:id([0-9]+)/password
        return this.http.put(userMgmtEndpoint + '/' + uid + '/password',
            {
                "old_password": newPassword,
                'new_password': confirmPwd
            },
            HTTP_JSON_OPTIONS)
            .toPromise()
            .then(response => response)  // 返回收到的结果
            .catch(error => {
                return Promise.reject(error);
            });
    }
    // 可以无视
    // Get User from LDAP。
    getLDAPUsers(username: string): Promise<User[]> {
        return this.http.get(`${ldapUserEndpoint}/search?username=${username}`, HTTP_GET_OPTIONS)
        .toPromise()
        .then(response => {
            let ldapUser = response.json() as LDAPUser[] || [];
            return ldapUser.map(u => LDAPUsertoUser(u));
        })
        .catch( error => this.handleError(error));
    }

    importLDAPUsers(usernames: string[]): Promise<any> {
        return this.http.post(`${ldapUserEndpoint}/import`, JSON.stringify({ldap_uid_list: usernames}), HTTP_JSON_OPTIONS)
        .toPromise()
        .then(() => null )
        .catch(err => this.handleError(err));
    }
}
