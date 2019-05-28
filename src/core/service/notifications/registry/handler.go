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

package registry

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/goharbor/harbor/src/common/dao"
	clairdao "github.com/goharbor/harbor/src/common/dao/clair"
	"github.com/goharbor/harbor/src/common/models"
	"github.com/goharbor/harbor/src/common/utils"
	"github.com/goharbor/harbor/src/common/utils/log"
	"github.com/goharbor/harbor/src/core/api"
	"github.com/goharbor/harbor/src/core/config"
	"github.com/goharbor/harbor/src/core/notifier"
	coreutils "github.com/goharbor/harbor/src/core/utils"
	rep_notification "github.com/goharbor/harbor/src/replication/event/notification"
	"github.com/goharbor/harbor/src/replication/event/topic"
)

// NotificationHandler handles request on /service/notifications/, which listens to registry's events.
// 用来接收来自内部 registry 事件。比如说当用户上传镜像后，registry 会通知 core，根据相关配置判断是否进行镜像扫描任务
type NotificationHandler struct {
	api.BaseController
}

const manifestPattern = `^application/vnd.docker.distribution.manifest.v\d\+(json|prettyjws)`
const vicPrefix = "vic/"

// Post handles POST request, and records audit log or refreshes cache based on event.
func (n *NotificationHandler) Post() {
	var notification models.Notification
	// 从通知中的 json 数据中获取Notification
	err := json.Unmarshal(n.Ctx.Input.CopyBody(1<<32), &notification)

	if err != nil {
		log.Errorf("failed to decode notification: %v", err)
		return
	}

	// 对事件进行过滤，只有来自外部的 docker-client 的 pull or push 请求，或则来自 jobservice 的 push 请求事件通过过滤。
	events, err := filterEvents(&notification)
	if err != nil {
		log.Errorf("failed to filter events: %v", err)
		return
	}

	for _, event := range events {
		//repository = library/ubuntu
		repository := event.Target.Repository
		// 对于library/ubuntu ，提取出 project = library。
		project, _ := utils.ParseRepository(repository)
		tag := event.Target.Tag
		action := event.Action

		user := event.Actor.Name
		if len(user) == 0 {
			user = "anonymous"
		}

		// 根据project name 获取 project 的详细信息
		pro, err := config.GlobalProjectMgr.Get(project)
		if err != nil {
			log.Errorf("failed to get project by name %s: %v", project, err)
			return
		}
		if pro == nil {
			log.Warningf("project %s not found", project)
			continue
		}

		go func() {
			// 将记录日志存入数据库中持久化存储。
			if err := dao.AddAccessLog(models.AccessLog{
				Username:  user,
				ProjectID: pro.ProjectID,
				RepoName:  repository,
				RepoTag:   tag,
				Operation: action,
				OpTime:    time.Now(),
			}); err != nil {
				log.Errorf("failed to add access log: %v", err)
			}
		}()

		if action == "push" {
			go func() {
				// 从数据库中检查是否存在对应的镜像存储仓库。 例如 repository = library/ubuntu
				exist := dao.RepositoryExists(repository)
				if exist {
					return
				}
				log.Debugf("Add repository %s into DB.", repository)
				repoRecord := models.RepoRecord{
					Name:      repository,
					ProjectID: pro.ProjectID,
				}
				// 将 push 来的 repository 信息存储在数据库中
				if err := dao.AddRepository(repoRecord); err != nil {
					log.Errorf("Error happens when adding repository: %v", err)
				}
			}()
			// 等待镜像的 manifest 准备好，manifest 准备好意味着镜像完成了上传
			if !coreutils.WaitForManifestReady(repository, tag, 5) {
				log.Errorf("Manifest for image %s:%s is not ready, skip the follow up actions.", repository, tag)
				return
			}

			// 当完成完成镜像manifest 的检查之后，发布消息
			go func() {
				image := repository + ":" + tag
				// 发出指定镜像复制的事件
				err := notifier.Publish(topic.ReplicationEventTopicOnPush, rep_notification.OnPushNotification{
					Image: image,
				})
				if err != nil {
					log.Errorf("failed to publish on push topic for resource %s: %v", image, err)
					return
				}
				log.Debugf("the on push topic for resource %s published", image)
			}()

			// 检查是否需要自动扫描镜像仓库，当有镜像上传到指定的 repository 中时。
			if autoScanEnabled(pro) {
				last, err := clairdao.GetLastUpdate()
				if err != nil {
					log.Errorf("Failed to get last update from Clair DB, error: %v, the auto scan will be skipped.", err)
				} else if last == 0 {
					log.Infof("The Vulnerability data is not ready in Clair DB, the auto scan will be skipped.", err)
				} else if err := coreutils.TriggerImageScan(repository, tag); err != nil {
					log.Warningf("Failed to scan image, repository: %s, tag: %s, error: %v", repository, tag, err)
				}
			}
		}
		if action == "pull" {
			go func() {
				log.Debugf("Increase the repository %s pull count.", repository)
				if err := dao.IncreasePullCount(repository); err != nil {
					log.Errorf("Error happens when increasing pull count: %v", repository)
				}
			}()
		}
	}
}

// 只有符合正则要求的请求类型事件才可以返回
func filterEvents(notification *models.Notification) ([]*models.Event, error) {
	events := []*models.Event{}

	for _, event := range notification.Events {
		log.Debugf("receive an event: \n----ID: %s \n----target: %s:%s \n----digest: %s \n----action: %s \n----mediatype: %s \n----user-agent: %s", event.ID, event.Target.Repository,
			event.Target.Tag, event.Target.Digest, event.Action, event.Target.MediaType, event.Request.UserAgent)

		// 判断 media type 是否匹配
		isManifest, err := regexp.MatchString(manifestPattern, event.Target.MediaType)
		if err != nil {
			log.Errorf("failed to match the media type against pattern: %v", err)
			continue
		}

		if !isManifest {
			continue
		}

		if checkEvent(&event) {
			events = append(events, &event)
			log.Debugf("add event to collection: %s", event.ID)
			continue
		}
	}

	return events, nil
}

func checkEvent(event *models.Event) bool {
	// pull and push manifest
	//当事件请求的发起着不是harbor-registry-client ，说明事件的请求来自于用户同时 action =pull or push。可以判断为pull and push manifest 请求。
	if strings.ToLower(strings.TrimSpace(event.Request.UserAgent)) != "harbor-registry-client" && (event.Action == "pull" || event.Action == "push") {
		return true
	}
	// push manifest by job-service
	// 当事件请求的发起者是 harbor-registry-client 时，这是一个内部的请求，由 jobservice 发起，用来与其他 harbor 仓库进行同步。
	if strings.ToLower(strings.TrimSpace(event.Request.UserAgent)) == "harbor-registry-client" && event.Action == "push" {
		return true
	}
	return false
}

func autoScanEnabled(project *models.Project) bool {
	if !config.WithClair() {
		log.Debugf("Auto Scan disabled because Harbor is not deployed with Clair")
		return false
	}

	return project.AutoScan()
}

// Render returns nil as it won't render any template.
func (n *NotificationHandler) Render() error {
	return nil
}
