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
import { Component } from '@angular/core';
import { Title } from '@angular/platform-browser';

import { TranslateService } from '@ngx-translate/core';
import { CookieService } from 'ngx-cookie';

import { SessionService } from './shared/session.service';
import { AppConfigService } from './app-config.service';

@Component({
    selector: 'harbor-app',
    templateUrl: 'app.component.html'
})
export class AppComponent {
    // 初始化各种服务：翻译，cookie，session，配置服务，title 服务
    constructor(
        private translate: TranslateService,
        private cookie: CookieService,
        private session: SessionService,
        private appConfigService: AppConfigService,
        private titleService: Title) {
        // Override page title
        let key: string = "APP_TITLE.HARBOR";
        if (this.appConfigService.isIntegrationMode()) {
            key = "APP_TITLE.REG";
        }

        translate.get(key).subscribe((res: string) => {
            this.titleService.setTitle(res);
        });
    }
}
