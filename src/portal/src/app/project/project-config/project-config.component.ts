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
import { Component, OnInit } from '@angular/core';
import { ActivatedRoute, Router } from '@angular/router';
import { SessionService } from '../../shared/session.service';
import { SessionUser } from '../../shared/session-user';
import { Project } from '../project';

@Component({
  selector: 'app-project-config',
  templateUrl: './project-config.component.html',
  styleUrls: ['./project-config.component.scss']
})
export class ProjectConfigComponent implements OnInit {

  projectId: number;
  projectName: string;
  currentUser: SessionUser;
  hasSignedIn: boolean;
  hasProjectAdminRole: boolean;

  constructor(
    private route: ActivatedRoute,
    private router: Router,
    private session: SessionService) {}

  ngOnInit() {
    this.projectId = +this.route.snapshot.parent.params['id'];
    this.currentUser = this.session.getCurrentUser();
    this.hasSignedIn = this.session.getCurrentUser() !== null;
    // ActivatedRoute 用来监控路由中的数据
    let resolverData = this.route.snapshot.parent.data;
    if (resolverData) {
      // 获取 resolve 中获取的 project 信息 projectResolver
      let pro: Project = <Project>resolverData['projectResolver'];
      this.hasProjectAdminRole = pro.has_project_admin_role;
      this.projectName = pro.name;
    }
  }
}
