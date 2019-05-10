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
import { Component, OnInit, OnDestroy } from '@angular/core';
import { Subscription } from "rxjs";

import { GlobalSearchService } from './global-search.service';
import { SearchResults } from './search-results';
import { SearchTriggerService } from './search-trigger.service';

import { AppConfigService } from './../../app-config.service';
import { MessageHandlerService } from '../../shared/message-handler/message-handler.service';

@Component({
    selector: "search-result",
    templateUrl: "search-result.component.html",
    styleUrls: ["search.component.scss"],

    providers: [GlobalSearchService]
})

export class SearchResultComponent implements OnInit, OnDestroy {
    // 搜索结果结构体
    searchResults: SearchResults = new SearchResults();
    originalCopy: SearchResults;

    currentTerm: string = "";

    // Open or close
    stateIndicator: boolean = false;
    // Search in progress
    onGoing: boolean = false;

    // Whether or not mouse point is onto the close indicator
    mouseOn: boolean = false;

    // Watch message channel
    searchSub: Subscription;
    closeSearchSub: Subscription;

    constructor(
        private search: GlobalSearchService,
        private msgHandler: MessageHandlerService,
        private searchTrigger: SearchTriggerService, // 触发器
        private appConfigService: AppConfigService) { }

    ngOnInit() {
        this.searchSub = this.searchTrigger.searchTriggerChan$.subscribe(term => {
            this.doSearch(term);
        });
        this.closeSearchSub = this.searchTrigger.searchCloseChan$.subscribe(close => {
            this.close();
        });
    }

    ngOnDestroy() {
        if (this.searchSub) {
            this.searchSub.unsubscribe();
        }

        if (this.closeSearchSub) {
            this.closeSearchSub.unsubscribe();
        }
    }

    clone(src: SearchResults): SearchResults {
        let res: SearchResults = new SearchResults();

        if (src) {
            src.project.forEach(pro => res.project.push(Object.assign({}, pro)));
            src.repository.forEach(repo => res.repository.push(Object.assign({}, repo)));
            if (this.withHelmChart) {
                src.chart.forEach(chart => res.chart.push(JSON.parse(JSON.stringify(chart))));
            }
            return res;
        }

        return res; // Empty object
    }

    public get state(): boolean {
        return this.stateIndicator;
    }

    public get done(): boolean {
        return !this.onGoing;
    }

    public get hover(): boolean {
        return this.mouseOn;
    }

    // Show the results
    show(): void {
        this.stateIndicator = true;
    }

    // Close the result page
    close(): void {
        this.stateIndicator = false;
        this.searchTrigger.clear(true);
    }

    // Call search service to complete the search request
    doSearch(term: string): void {
        // Only search none empty term
        if (!term || term.trim() === "") {
            return;
        }
        // Do nothing if search is ongoing
        if (this.onGoing) {
            return;
        }
        // Confirm page is displayed
        if (!this.stateIndicator) {
            this.show();
        }

        this.currentTerm = term;

        // If term is empty, then clear the results
        if (term === "") {
            this.searchResults.project = [];
            this.searchResults.repository = [];
            if (this.withHelmChart) {
                this.searchResults.chart = [];
            }
            return;
        }
        // Show spinner
        this.onGoing = true;

        // 调用 search 服务，发送 http 请求给后端，从数据库中获取结果。
        this.search.doSearch(term)
            .then(searchResults => {
                this.onGoing = false;
                this.originalCopy = searchResults; // Keep the original data
                this.searchResults = this.clone(searchResults);
            })
            .catch(error => {
                this.onGoing = false;
                this.msgHandler.handleError(error);
            });
    }
    get withHelmChart(): boolean {
        return this.appConfigService.getConfig().with_chartmuseum;
    }
}
