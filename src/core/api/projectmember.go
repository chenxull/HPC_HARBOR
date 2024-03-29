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
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/goharbor/harbor/src/common"
	"github.com/goharbor/harbor/src/common/dao/project"
	"github.com/goharbor/harbor/src/common/models"
	"github.com/goharbor/harbor/src/common/utils/log"
	"github.com/goharbor/harbor/src/core/auth"
)

/*
	在给具体 project 增加成员时，总共需要 3 个请求
	HPM] GET /api/projects/1/members?entityname=chenxu -> http://172.16.21.135
	[HPM] POST /api/projects/1/members -> http://172.16.21.135
	[HPM] GET /api/projects/1/members?entityname= -> http://172.16.21.135
*/
// ProjectMemberAPI handles request to /api/projects/{}/members/{}
type ProjectMemberAPI struct {
	BaseController
	id         int
	entityID   int
	entityType string
	project    *models.Project
}

// ErrDuplicateProjectMember ...
var ErrDuplicateProjectMember = errors.New("The project member specified already exist")

// ErrInvalidRole ...
var ErrInvalidRole = errors.New("Failed to update project member, role is not in 1,2,3")

// Prepare validates the URL and parms
func (pma *ProjectMemberAPI) Prepare() {
	pma.BaseController.Prepare()

	if !pma.SecurityCtx.IsAuthenticated() {
		pma.HandleUnauthorized()
		return
	}
	// pid 就是project_id
	pid, err := pma.GetInt64FromPath(":pid")
	if err != nil || pid <= 0 {
		text := "invalid project ID: "
		if err != nil {
			text += err.Error()
		} else {
			text += fmt.Sprintf("%d", pid)
		}
		pma.HandleBadRequest(text)
		return
	}
	// 调用 project manager 接口来获取 project 详细信息
	project, err := pma.ProjectMgr.Get(pid)
	if err != nil {
		pma.ParseAndHandleError(fmt.Sprintf("failed to get project %d", pid), err)
		return
	}
	if project == nil {
		pma.HandleNotFound(fmt.Sprintf("project %d not found", pid))
		return
	}
	// 将从 数据库中获取的 project 信息赋值给 pma。
	pma.project = project

	if !(pma.Ctx.Input.IsGet() && pma.SecurityCtx.HasReadPerm(pid) ||
		pma.SecurityCtx.HasAllPerm(pid)) {
		pma.HandleForbidden(pma.SecurityCtx.GetUsername())
		return
	}
	// 需要增加的成员的编号。project member id
	pmid, err := pma.GetInt64FromPath(":pmid")
	if err != nil {
		log.Warningf("Failed to get pmid from path, error %v", err)
	}
	if pmid <= 0 && (pma.Ctx.Input.IsPut() || pma.Ctx.Input.IsDelete()) {
		pma.HandleBadRequest(fmt.Sprintf("The project member id is invalid, pmid:%s", pma.GetStringFromPath(":pmid")))
		return
	}
	pma.id = int(pmid)
}

// Get ...
// 在显示当前项目成员时，entityname 为空
func (pma *ProjectMemberAPI) Get() {
	projectID := pma.project.ProjectID
	queryMember := models.Member{}
	queryMember.ProjectID = projectID
	pma.Data["json"] = make([]models.Member, 0)
	if pma.id == 0 {
		// list 中此项为空
		entityname := pma.GetString("entityname")
		memberList, err := project.SearchMemberByName(projectID, entityname)
		if err != nil {
			pma.HandleInternalServerError(fmt.Sprintf("Failed to query database for member list, error: %v", err))
			return
		}
		if len(memberList) > 0 {
			pma.Data["json"] = memberList
		}

	} else {
		// return a specific member
		queryMember.ID = pma.id
		memberList, err := project.GetProjectMember(queryMember)
		if err != nil {
			pma.HandleInternalServerError(fmt.Sprintf("Failed to query database for member list, error: %v", err))
			return
		}
		if len(memberList) == 0 {
			pma.HandleNotFound(fmt.Sprintf("The project member does not exit, pmid:%v", pma.id))
			return
		}
		// 只将成员中的第一个发送回去？
		pma.Data["json"] = memberList[0]
	}
	pma.ServeJSON()
}

// Post ... Add a project member
func (pma *ProjectMemberAPI) Post() {
	projectID := pma.project.ProjectID
	var request models.MemberReq
	pma.DecodeJSONReq(&request)
	request.MemberGroup.LdapGroupDN = strings.TrimSpace(request.MemberGroup.LdapGroupDN)

	pmid, err := AddProjectMember(projectID, request)
	if err == auth.ErrorGroupNotExist || err == auth.ErrorUserNotExist {
		pma.HandleNotFound(fmt.Sprintf("Failed to add project member, error: %v", err))
		return
	} else if err == auth.ErrDuplicateLDAPGroup {
		pma.HandleConflict(fmt.Sprintf("Failed to add project member, already exist LDAP group or project member, groupDN:%v", request.MemberGroup.LdapGroupDN))
		return
	} else if err == ErrDuplicateProjectMember {
		pma.HandleConflict(fmt.Sprintf("Failed to add project member, already exist LDAP group or project member, groupMemberID:%v", request.MemberGroup.ID))
		return
	} else if err == ErrInvalidRole {
		pma.HandleBadRequest(fmt.Sprintf("Invalid role ID, role ID %v", request.Role))
		return
	} else if err == auth.ErrInvalidLDAPGroupDN {
		pma.HandleBadRequest(fmt.Sprintf("Invalid LDAP DN: %v", request.MemberGroup.LdapGroupDN))
		return
	} else if err != nil {
		pma.HandleInternalServerError(fmt.Sprintf("Failed to add project member, error: %v", err))
		return
	}
	pma.Redirect(http.StatusCreated, strconv.FormatInt(int64(pmid), 10))
}

// Put ... Update an exist project member
func (pma *ProjectMemberAPI) Put() {
	pid := pma.project.ProjectID
	pmID := pma.id
	var req models.Member
	pma.DecodeJSONReq(&req)
	if req.Role < 1 || req.Role > 3 {
		pma.HandleBadRequest(fmt.Sprintf("Invalid role id %v", req.Role))
		return
	}
	err := project.UpdateProjectMemberRole(pmID, req.Role)
	if err != nil {
		pma.HandleInternalServerError(fmt.Sprintf("Failed to update DB to add project user role, project id: %d, pmid : %d, role id: %d", pid, pmID, req.Role))
		return
	}
}

// Delete ...
func (pma *ProjectMemberAPI) Delete() {
	pmid := pma.id
	err := project.DeleteProjectMemberByID(pmid)
	if err != nil {
		pma.HandleInternalServerError(fmt.Sprintf("Failed to delete project roles for user, project member id: %d, error: %v", pmid, err))
		return
	}
}

// AddProjectMember ...
func AddProjectMember(projectID int64, request models.MemberReq) (int, error) {
	var member models.Member
	member.ProjectID = projectID
	member.Role = request.Role
	if request.MemberUser.UserID > 0 {
		member.EntityID = request.MemberUser.UserID
		member.EntityType = common.UserMember
	} else if request.MemberGroup.ID > 0 {
		member.EntityID = request.MemberGroup.ID
		member.EntityType = common.GroupMember
	} else if len(request.MemberUser.Username) > 0 {
		member.EntityType = common.UserMember
		userID, err := auth.SearchAndOnBoardUser(request.MemberUser.Username)
		if err != nil {
			return 0, err
		}
		member.EntityID = userID
	} else if len(request.MemberGroup.LdapGroupDN) > 0 {

		// If groupname provided, use the provided groupname to name this group
		groupID, err := auth.SearchAndOnBoardGroup(request.MemberGroup.LdapGroupDN, request.MemberGroup.GroupName)
		if err != nil {
			return 0, err
		}
		member.EntityID = groupID
		member.EntityType = common.GroupMember
	}
	if member.EntityID <= 0 {
		return 0, fmt.Errorf("Can not get valid member entity, request: %+v", request)
	}

	// Check if member already exist in current project
	memberList, err := project.GetProjectMember(models.Member{
		ProjectID:  member.ProjectID,
		EntityID:   member.EntityID,
		EntityType: member.EntityType,
	})
	if err != nil {
		return 0, err
	}
	if len(memberList) > 0 {
		return 0, ErrDuplicateProjectMember
	}

	if member.Role < 1 || member.Role > 3 {
		// Return invalid role error
		return 0, ErrInvalidRole
	}
	return project.AddProjectMember(member)
}
