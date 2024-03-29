
import {forkJoin as observableForkJoin,  Subscription } from "rxjs";
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
import {
    Component,
    Output,
    ChangeDetectionStrategy,
    ChangeDetectorRef,
    OnDestroy, EventEmitter
} from "@angular/core";
import { Router } from "@angular/router";

import { Comparator, State } from "@clr/angular";
import {TranslateService} from "@ngx-translate/core";

import { RoleInfo, ConfirmationTargets, ConfirmationState, ConfirmationButtons } from "../../shared/shared.const";
import { CustomComparator, doFiltering, doSorting, calculatePage } from "../../shared/shared.utils";

import { SessionService } from "../../shared/session.service";
import { StatisticHandler } from "../../shared/statictics/statistic-handler.service";
import { ConfirmationDialogService } from "../../shared/confirmation-dialog/confirmation-dialog.service";
import { MessageHandlerService } from "../../shared/message-handler/message-handler.service";
import { ConfirmationMessage } from "../../shared/confirmation-dialog/confirmation-message";
import { SearchTriggerService } from "../../base/global-search/search-trigger.service";
import {AppConfigService} from "../../app-config.service";
import {operateChanges, OperateInfo, OperationService, OperationState} from "@harbor/ui";

import { Project } from "../project";
import { ProjectService } from "../project.service";

@Component({
    selector: "list-project",
    templateUrl: "list-project.component.html",
    changeDetection: ChangeDetectionStrategy.OnPush
})
export class ListProjectComponent implements OnDestroy {
    loading = true;
    projects: Project[] = [];
    filteredType = 0; // All projects
    searchKeyword = "";
    selectedRow: Project[]  = [];
  // 会发往父组件
  @Output() addProject = new EventEmitter<void>();

    roleInfo = RoleInfo;
    repoCountComparator: Comparator<Project> = new CustomComparator<Project>("repo_count", "number");
    chartCountComparator: Comparator<Project> = new CustomComparator<Project>("chart_count", "number");
    timeComparator: Comparator<Project> = new CustomComparator<Project>("creation_time", "date");
    accessLevelComparator: Comparator<Project> = new CustomComparator<Project>("public", "string");
    roleComparator: Comparator<Project> = new CustomComparator<Project>("current_user_role_id", "number");
    currentPage = 1;
    totalCount = 0;
    pageSize = 15;
    currentState: State;
    subscription: Subscription;

    constructor(
        private session: SessionService,
        private appConfigService: AppConfigService,
        private router: Router,
        private searchTrigger: SearchTriggerService,
        private proService: ProjectService,
        private msgHandler: MessageHandlerService,
        private statisticHandler: StatisticHandler,
        private translate: TranslateService,
        private deletionDialogService: ConfirmationDialogService,
        private operationService: OperationService,
        private ref: ChangeDetectorRef) {
        this.subscription = deletionDialogService.confirmationConfirm$.subscribe(message => {
            if (message &&
                message.state === ConfirmationState.CONFIRMED &&
                message.source === ConfirmationTargets.PROJECT) {
                this.delProjects(message.data);
            }
        });

        let hnd = setInterval(() => ref.markForCheck(), 100);
        setTimeout(() => clearInterval(hnd), 5000);
    }

    get showRoleInfo(): boolean {
        return this.filteredType !== 2;
    }

    get projectCreationRestriction(): boolean {
        let account = this.session.getCurrentUser();
        if (account) {
            switch (this.appConfigService.getConfig().project_creation_restriction) {
                case "adminonly":
                    return (account.has_admin_role);
                case "everyone":
                    return true;
            }
        }
        return false;
    }

    get withChartMuseum(): boolean {
        if (this.appConfigService.getConfig().with_chartmuseum) {
            return true;
        } else {
            return false;
        }
    }

    public get isSystemAdmin(): boolean {
        let account = this.session.getCurrentUser();
        return account != null && account.has_admin_role;
    }

    public get canDelete(): boolean {
        if (this.projects.length) {
           return this.projects.some((pro: Project) => pro.current_user_role_id === 1);
        }
        return false;
    }

    ngOnDestroy(): void {
        if (this.subscription) {
            this.subscription.unsubscribe();
        }
    }

    addNewProject(): void {
        this.addProject.emit();
    }

    goToLink(proId: number): void {
        // 为何要关闭搜索
        this.searchTrigger.closeSearch(true);

        let linkUrl = ["harbor", "projects", proId, "repositories"];
        // 前端界面的路由内部跳转，进入到project 的具体存储库
        this.router.navigate(linkUrl);
    }

    selectedChange(): void {
        let hnd = setInterval(() => this.ref.markForCheck(), 100);
        setTimeout(() => clearInterval(hnd), 2000);
    }

    clrLoad(state: State) {
        this.selectedRow = [];

        // Keep state for future filtering and sorting
        this.currentState = state;

        let pageNumber: number = calculatePage(state);
        if (pageNumber <= 0) { pageNumber = 1; }

        this.loading = true;

        let passInFilteredType: number = undefined;
        if (this.filteredType > 0) {
            passInFilteredType = this.filteredType - 1;
        }
        this.proService.listProjects(this.searchKeyword, passInFilteredType, pageNumber, this.pageSize).toPromise()
            .then(response => {
                // Get total count
                if (response.headers) {
                    let xHeader: string = response.headers.get("X-Total-Count");
                    if (xHeader) {
                        this.totalCount = parseInt(xHeader, 0);
                    }
                }

                this.projects = response.json() as Project[];
                // Do customising filtering and sorting
                this.projects = doFiltering<Project>(this.projects, state);
                this.projects = doSorting<Project>(this.projects, state);

                this.loading = false;
            })
            .catch(error => {
                this.loading = false;
                this.msgHandler.handleError(error);
            });

        // Force refresh view
        let hnd = setInterval(() => this.ref.markForCheck(), 100);
        setTimeout(() => clearInterval(hnd), 5000);
    }

    newReplicationRule(p: Project) {
        if (p) {
            this.router.navigateByUrl(`/harbor/projects/${p.project_id}/replications?is_create=true`);
        }
    }

    toggleProject(p: Project) {
        if (p) {
            p.metadata.public === "true" ? p.metadata.public = "false" : p.metadata.public = "true";
            this.proService
                .toggleProjectPublic(p.project_id, p.metadata.public)
                .subscribe(
                response => {
                    this.msgHandler.showSuccess("PROJECT.TOGGLED_SUCCESS");
                    let pp: Project = this.projects.find((item: Project) => item.project_id === p.project_id);
                    if (pp) {
                        pp.metadata.public = p.metadata.public;
                        this.statisticHandler.refresh();
                    }
                },
                error => this.msgHandler.handleError(error)
                );

            // Force refresh view
            let hnd = setInterval(() => this.ref.markForCheck(), 100);
            setTimeout(() => clearInterval(hnd), 2000);
        }
    }

    deleteProjects(p: Project[]) {
        let nameArr: string[] = [];
        if (p && p.length) {
            p.forEach(data => {
                nameArr.push(data.name);
            });
            let deletionMessage = new ConfirmationMessage(
                "PROJECT.DELETION_TITLE",
                "PROJECT.DELETION_SUMMARY",
                nameArr.join(","),
                p,
                ConfirmationTargets.PROJECT,
                ConfirmationButtons.DELETE_CANCEL
                );
                this.deletionDialogService.openComfirmDialog(deletionMessage);
        }
    }
    delProjects(projects: Project[]) {
        let observableLists: any[] = [];
        if (projects && projects.length) {
            projects.forEach(data => {
                observableLists.push(this.delOperate(data));
            });
            Promise.all(observableLists).then(item => {
                let st: State = this.getStateAfterDeletion();
                this.selectedRow = [];
                if (!st) {
                    this.refresh();
                } else {
                    this.clrLoad(st);
                    this.statisticHandler.refresh();
                }
            });
        }
    }

    delOperate(project: Project) {
        // init operation info
        let operMessage = new OperateInfo();
        operMessage.name = 'OPERATION.DELETE_PROJECT';
        operMessage.data.id = project.project_id;
        operMessage.state = OperationState.progressing;
        operMessage.data.name = project.name;
        this.operationService.publishInfo(operMessage);

        return this.proService.deleteProject(project.project_id)
            .then(
                () => {
                    this.translate.get("BATCH.DELETED_SUCCESS").subscribe(res => {
                        operateChanges(operMessage, OperationState.success);
                    });
                },
                error => {
                    if (error && error.status === 412) {
                        observableForkJoin(this.translate.get("BATCH.DELETED_FAILURE"),
                            this.translate.get("PROJECT.FAILED_TO_DELETE_PROJECT")).subscribe(res => {
                            operateChanges(operMessage, OperationState.failure, res[1]);
                        });
                    } else {
                        this.translate.get("BATCH.DELETED_FAILURE").subscribe(res => {
                            operateChanges(operMessage, OperationState.failure, res);
                        });
                    }
                });
    }

    refresh(): void {
        this.currentPage = 1;
        this.filteredType = 0;
        this.searchKeyword = "";

        this.reload();
        this.statisticHandler.refresh();
    }

    doFilterProject(filter: number): void {
        this.currentPage = 1;
        this.filteredType = filter;
        this.reload();
    }

    doSearchProject(proName: string): void {
        this.currentPage = 1;
        this.searchKeyword = proName;
        this.reload();
    }

    reload(): void {
        let st: State = this.currentState;
        if (!st) {
            st = {
                page: {}
            };
        }
        st.page.from = 0;
        st.page.to = this.pageSize - 1;
        st.page.size = this.pageSize;

        this.clrLoad(st);
    }

    getStateAfterDeletion(): State {
        let total: number = this.totalCount - this.selectedRow.length;
        if (total <= 0) { return null; }

        let totalPages: number = Math.ceil(total / this.pageSize);
        let targetPageNumber: number = this.currentPage;

        if (this.currentPage > totalPages) {
            targetPageNumber = totalPages; // Should == currentPage -1
        }

        let st: State = this.currentState;
        if (!st) {
            st = { page: {} };
        }
        st.page.size = this.pageSize;
        st.page.from = (targetPageNumber - 1) * this.pageSize;
        st.page.to = targetPageNumber * this.pageSize - 1;

        return st;
    }

}
