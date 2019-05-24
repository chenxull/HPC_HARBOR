import { Component, Input, OnInit, ViewChild } from '@angular/core';

import { toPromise, compareValue, clone } from '../utils';
import { ProjectService } from '../service/project.service';
import { ErrorHandler } from '../error-handler/error-handler';
import { State } from '@clr/angular';

import { ConfirmationState, ConfirmationTargets } from '../shared/shared.const';
import { ConfirmationMessage } from '../confirmation-dialog/confirmation-message';
import { ConfirmationDialogComponent } from '../confirmation-dialog/confirmation-dialog.component';
import { ConfirmationAcknowledgement } from '../confirmation-dialog/confirmation-state-message';
import { TranslateService } from '@ngx-translate/core';

import { Project } from './project';
import {SystemInfo, SystemInfoService} from '../service/index';

// 定义一个project 策略类
export class ProjectPolicy {
  Public: boolean;
  ContentTrust: boolean;
  PreventVulImg: boolean;
  PreventVulImgSeverity: string;
  ScanImgOnPush: boolean;

  constructor() {
    this.Public = false;
    this.ContentTrust = false;
    this.PreventVulImg = false;
    this.PreventVulImgSeverity = 'low';
    this.ScanImgOnPush = false;
  }

  // 根据传入的 Project 数据来初始化配置信息
  initByProject(pro: Project) {
    this.Public = pro.metadata.public === 'true' ? true : false;
    this.ContentTrust = pro.metadata.enable_content_trust === 'true' ? true : false;
    this.PreventVulImg = pro.metadata.prevent_vul === 'true' ? true : false;
    if (pro.metadata.severity) { this.PreventVulImgSeverity = pro.metadata.severity; }
    this.ScanImgOnPush = pro.metadata.auto_scan === 'true' ? true : false;
  }
}

@Component({
  selector: 'hbr-project-policy-config',
  templateUrl: './project-policy-config.component.html',
  styleUrls: ['./project-policy-config.component.scss']
})
export class ProjectPolicyConfigComponent implements OnInit {
  onGoing = false;
  // 下面四个 Input 是从父组件传来的数据
  @Input() projectId: number;
  @Input() projectName = 'unknown';

  @Input() hasSignedIn: boolean;
  @Input() hasProjectAdminRole: boolean;

  @ViewChild('cfgConfirmationDialog') confirmationDlg: ConfirmationDialogComponent;

  systemInfo: SystemInfo;
  // 数据通过表单的方式 与前端页面进行数据交换
  // orgProjectPolicy 是旧的配置数据
  orgProjectPolicy = new ProjectPolicy();
  // projectPolicy 是新的配置数据
  projectPolicy = new ProjectPolicy();

  // 定义的危险级别
  severityOptions = [
    {severity: 'high', severityLevel: 'VULNERABILITY.SEVERITY.HIGH'},
    {severity: 'medium', severityLevel: 'VULNERABILITY.SEVERITY.MEDIUM'},
    {severity: 'low', severityLevel: 'VULNERABILITY.SEVERITY.LOW'},
    {severity: 'negligible', severityLevel: 'VULNERABILITY.SEVERITY.NEGLIGIBLE'},
  ];
  // 初始化四种服务
  constructor(
    private errorHandler: ErrorHandler,
    private translate: TranslateService,
    private projectService: ProjectService,
    private systemInfoService: SystemInfoService, // 从后端服务器获取系统配置信息
  ) {}

  ngOnInit(): void {
    // assert if project id exist
    if (!this.projectId) {
      this.errorHandler.error('Project ID cannot be unset.');
      return;
    }

    // get system info。使用异步通信的方式。收到信息之后，存放在 systemInfo 中
    toPromise<SystemInfo>(this.systemInfoService.getSystemInfo())
    .then(systemInfo => this.systemInfo = systemInfo)
    .catch(error => this.errorHandler.error(error));

    // retrive project level policy data
    // 获取此 project 的
    this.retrieve();
  }

  public get withNotary(): boolean {
    return this.systemInfo ? this.systemInfo.with_notary : false;
  }

  public get withClair(): boolean {
    return this.systemInfo ? this.systemInfo.with_clair : false;
  }

  retrieve(state?: State): any {
    toPromise<Project>(this.projectService.getProject(this.projectId))
    .then(
      response => {
        // 根据从后端服务器获取的最新数据来 初始化配置信息
        this.orgProjectPolicy.initByProject(response);
        this.projectPolicy.initByProject(response);
      })
    .catch(error => this.errorHandler.error(error));
  }

  updateProjectPolicy(projectId: string|number, pp: ProjectPolicy) {
    this.projectService.updateProjectPolicy(projectId, pp);
  }

  refresh() {
    this.retrieve();
  }

  isValid() {
    let flag = false;
    if (!this.projectPolicy.PreventVulImg || this.severityOptions.some(x => x.severity === this.projectPolicy.PreventVulImgSeverity)) {
      flag = true;
    }
    return flag;
  }

  hasChanges() {
    return !compareValue(this.orgProjectPolicy, this.projectPolicy);
  }

  save() {
    if (!this.hasChanges()) {
      return;
    }
    this.onGoing = true;
    // 异步的方式，后端 API：/api/projects/:id([0-9]+）
    toPromise<any>(this.projectService.updateProjectPolicy(this.projectId, this.projectPolicy))
    .then(() => {
      this.onGoing = false;
      // 需要翻译，对照 json 文件来
      this.translate.get('CONFIG.SAVE_SUCCESS').subscribe((res: string) => {
        this.errorHandler.info(res);
      });
      // 更新页面，所有会有一个 get 请求
      this.refresh();
    })
    .catch(error => {
      this.onGoing = false;
      this.errorHandler.error(error);
    });
  }

  cancel(): void {
    let msg = new ConfirmationMessage(
        'CONFIG.CONFIRM_TITLE',
        'CONFIG.CONFIRM_SUMMARY',
        '',
        {},
        ConfirmationTargets.CONFIG
    );
    this.confirmationDlg.open(msg);
  }

  reset(): void {
    this.projectPolicy = clone(this.orgProjectPolicy);
  }

  confirmCancel(ack: ConfirmationAcknowledgement): void {
    if (ack && ack.source === ConfirmationTargets.CONFIG &&
        ack.state === ConfirmationState.CONFIRMED) {
        this.reset();
    }
  }
}
