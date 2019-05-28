// Copyright 2018 Project Harbor Authors
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

package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/goharbor/harbor/src/common/dao"
	"github.com/goharbor/harbor/src/common/models"
	"github.com/goharbor/harbor/src/common/utils"
	"github.com/goharbor/harbor/src/common/utils/clair"
	registry_error "github.com/goharbor/harbor/src/common/utils/error"
	"github.com/goharbor/harbor/src/common/utils/log"
	"github.com/goharbor/harbor/src/common/utils/registry"
	"github.com/goharbor/harbor/src/common/utils/registry/auth"
	"github.com/goharbor/harbor/src/core/config"
	"github.com/goharbor/harbor/src/core/notifier"
	"github.com/goharbor/harbor/src/core/promgr"
	"github.com/goharbor/harbor/src/core/service/token"
	coreutils "github.com/goharbor/harbor/src/core/utils"
)

// SyncRegistry syncs the repositories of registry with database.
func SyncRegistry(pm promgr.ProjectManager) error {

	log.Infof("Start syncing repositories from registry to DB... ")

	// 获取 registry 中的概要信息
	reposInRegistry, err := catalog()
	if err != nil {
		log.Error(err)
		return err
	}

	var repoRecordsInDB []*models.RepoRecord
	// 获取数据库中已经存在的 repository
	repoRecordsInDB, err = dao.GetRepositories()
	if err != nil {
		log.Errorf("error occurred while getting all registories. %v", err)
		return err
	}

	var reposInDB []string
	for _, repoRecordInDB := range repoRecordsInDB {
		reposInDB = append(reposInDB, repoRecordInDB.Name)
	}

	var reposToAdd []string
	var reposToDel []string
	// 对于数据库中原有的 repository 数据，以及刚刚从 registry 中获取的 repository 数据。
	reposToAdd, reposToDel, err = diffRepos(reposInRegistry, reposInDB, pm)
	if err != nil {
		return err
	}

	if len(reposToAdd) > 0 {
		log.Debugf("Start adding repositories into DB... ")
		for _, repoToAdd := range reposToAdd {
			// 将一个 repository 分成二部分：project 和镜像名称。
			project, _ := utils.ParseRepository(repoToAdd)
			// 从 access_log 中获取此镜像被拉取多少次的信息
			pullCount, err := dao.CountPull(repoToAdd)
			if err != nil {
				log.Errorf("Error happens when counting pull count from access log: %v", err)
			}
			// 获取 project 的详细数据
			pro, err := pm.Get(project)
			if err != nil {
				log.Errorf("failed to get project %s: %v", project, err)
				continue
			}
			repoRecord := models.RepoRecord{
				Name:      repoToAdd,
				ProjectID: pro.ProjectID,
				PullCount: pullCount,
			}

			// 将数据写入到 repository 数据中
			if err := dao.AddRepository(repoRecord); err != nil {
				log.Errorf("Error happens when adding the missing repository: %v", err)
			} else {
				log.Debugf("Add repository: %s success.", repoToAdd)
			}
		}
	}

	if len(reposToDel) > 0 {
		log.Debugf("Start deleting repositories from DB... ")
		for _, repoToDel := range reposToDel {
			if err := dao.DeleteRepository(repoToDel); err != nil {
				log.Errorf("Error happens when deleting the repository: %v", err)
			} else {
				log.Debugf("Delete repository: %s success.", repoToDel)
			}
		}
	}

	log.Infof("Sync repositories from registry to DB is done.")
	return nil
}

func catalog() ([]string, error) {
	repositories := []string{}

	rc, err := initRegistryClient()
	if err != nil {
		return repositories, err
	}
	// 获取 registry 的概要信息
	repositories, err = rc.Catalog()
	if err != nil {
		return repositories, err
	}

	return repositories, nil
}

func diffRepos(reposInRegistry []string, reposInDB []string,
	pm promgr.ProjectManager) ([]string, []string, error) {
	var needsAdd []string
	var needsDel []string

	sort.Strings(reposInRegistry)
	sort.Strings(reposInDB)

	i, j := 0, 0
	repoInR, repoInD := "", ""
	for i < len(reposInRegistry) && j < len(reposInDB) {
		repoInR = reposInRegistry[i]
		repoInD = reposInDB[j]
		// 先排序，然后直接调用 strings 中的比较函数来判断数据是否相同
		d := strings.Compare(repoInR, repoInD)
		if d < 0 {
			i++
			// 检查 数据库中是否存在此project
			exist, err := projectExists(pm, repoInR)
			if err != nil {
				log.Errorf("failed to check the existence of project %s: %v", repoInR, err)
				continue
			}

			if !exist {
				continue
			}

			// TODO remove the workaround when the bug of registry is fixed
			client, err := coreutils.NewRepositoryClientForUI("harbor-core", repoInR)
			if err != nil {
				return needsAdd, needsDel, err
			}

			// 检查此 tag 的 镜像 是否存在与 registry 中，存在返回 true
			exist, err = repositoryExist(repoInR, client)
			//检查项目是否存在，检查 repository 是否真的在 registry 存在
			if err != nil {
				return needsAdd, needsDel, err
			}

			if !exist {
				continue
			}

			needsAdd = append(needsAdd, repoInR)
		} else if d > 0 {
			needsDel = append(needsDel, repoInD)
			j++
		} else {
			// TODO remove the workaround when the bug of registry is fixed
			client, err := coreutils.NewRepositoryClientForUI("harbor-core", repoInR)
			if err != nil {
				return needsAdd, needsDel, err
			}

			exist, err := repositoryExist(repoInR, client)
			if err != nil {
				return needsAdd, needsDel, err
			}

			if !exist {
				needsDel = append(needsDel, repoInD)
			}

			i++
			j++
		}
	}

	for i < len(reposInRegistry) {
		repoInR = reposInRegistry[i]
		i++
		exist, err := projectExists(pm, repoInR)
		if err != nil {
			log.Errorf("failed to check whether project of %s exists: %v", repoInR, err)
			continue
		}

		if !exist {
			continue
		}

		client, err := coreutils.NewRepositoryClientForUI("harbor-core", repoInR)
		if err != nil {
			log.Errorf("failed to create repository client: %v", err)
			continue
		}

		exist, err = repositoryExist(repoInR, client)
		if err != nil {
			log.Errorf("failed to check the existence of repository %s: %v", repoInR, err)
			continue
		}

		if !exist {
			continue
		}

		needsAdd = append(needsAdd, repoInR)
	}

	for j < len(reposInDB) {
		needsDel = append(needsDel, reposInDB[j])
		j++
	}

	return needsAdd, needsDel, nil
}

func projectExists(pm promgr.ProjectManager, repository string) (bool, error) {
	project, _ := utils.ParseRepository(repository)
	return pm.Exists(project)
}

func initRegistryClient() (r *registry.Registry, err error) {
	endpoint, err := config.RegistryURL()
	if err != nil {
		return nil, err
	}

	addr := endpoint
	if strings.Contains(endpoint, "://") {
		addr = strings.Split(endpoint, "://")[1]
	}

	if err := utils.TestTCPConn(addr, 60, 2); err != nil {
		return nil, err
	}

	authorizer := auth.NewRawTokenAuthorizer("harbor-core", token.Registry)
	return registry.NewRegistry(endpoint, &http.Client{
		Transport: registry.NewTransport(registry.GetHTTPTransport(), authorizer),
	})
}

func buildReplicationURL() string {
	url := config.InternalJobServiceURL()
	return fmt.Sprintf("%s/api/jobs/replication", url)
}

func buildJobLogURL(jobID string, jobType string) string {
	url := config.InternalJobServiceURL()
	return fmt.Sprintf("%s/api/jobs/%s/%s/log", url, jobType, jobID)
}

func buildReplicationActionURL() string {
	url := config.InternalJobServiceURL()
	return fmt.Sprintf("%s/api/jobs/replication/actions", url)
}

func repositoryExist(name string, client *registry.Repository) (bool, error) {
	// 获取此 repository 的 tag 数据
	tags, err := client.ListTag()
	if err != nil {
		if regErr, ok := err.(*registry_error.HTTPError); ok && regErr.StatusCode == http.StatusNotFound {
			return false, nil
		}
		return false, err
	}
	return len(tags) != 0, nil
}

// transformVulnerabilities transforms the returned value of Clair API to a list of VulnerabilityItem
func transformVulnerabilities(layerWithVuln *models.ClairLayerEnvelope) []*models.VulnerabilityItem {
	res := []*models.VulnerabilityItem{}
	l := layerWithVuln.Layer
	if l == nil {
		return res
	}
	features := l.Features
	if features == nil {
		return res
	}
	for _, f := range features {
		vulnerabilities := f.Vulnerabilities
		if vulnerabilities == nil {
			continue
		}
		for _, v := range vulnerabilities {
			vItem := &models.VulnerabilityItem{
				ID:          v.Name,
				Pkg:         f.Name,
				Version:     f.Version,
				Severity:    clair.ParseClairSev(v.Severity),
				Fixed:       v.FixedBy,
				Link:        v.Link,
				Description: v.Description,
			}
			res = append(res, vItem)
		}
	}
	return res
}

// Watch the configuration changes.
// Wrap the same method in common utils.
func watchConfigChanges(cfg map[string]interface{}) error {
	return notifier.WatchConfigChanges(cfg)
}
