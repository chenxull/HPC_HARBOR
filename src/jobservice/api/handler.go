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

package api

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/mux"

	"github.com/goharbor/harbor/src/jobservice/core"
	"github.com/goharbor/harbor/src/jobservice/errs"
	"github.com/goharbor/harbor/src/jobservice/logger"
	"github.com/goharbor/harbor/src/jobservice/models"
	"github.com/goharbor/harbor/src/jobservice/opm"
)

// Handler defines approaches to handle the http requests.
type Handler interface {
	// HandleLaunchJobReq is used to handle the job submission request.
	//用来处理 job 的提交请求
	HandleLaunchJobReq(w http.ResponseWriter, req *http.Request)

	// HandleGetJobReq is used to handle the job stats query request.
	// 用来处理 查询 job 状态请求
	HandleGetJobReq(w http.ResponseWriter, req *http.Request)

	// HandleJobActionReq is used to handle the job action requests (stop/retry).
	// 用来处理 对 job 的控制请求
	HandleJobActionReq(w http.ResponseWriter, req *http.Request)

	// HandleCheckStatusReq is used to handle the job service healthy status checking request.
	// 健康检查
	HandleCheckStatusReq(w http.ResponseWriter, req *http.Request)

	// HandleJobLogReq is used to handle the request of getting job logs
	// 获取 job 日志
	HandleJobLogReq(w http.ResponseWriter, req *http.Request)
}

// DefaultHandler is the default request handler which implements the Handler interface.
// 继承了 core 中的接口
type DefaultHandler struct {
	controller core.Interface
}

// NewDefaultHandler is constructor of DefaultHandler.
func NewDefaultHandler(ctl core.Interface) *DefaultHandler {
	// ctl 是 redis worker pool 的控制器
	return &DefaultHandler{
		controller: ctl,
	}
}

// HandleLaunchJobReq is implementation of method defined in interface 'Handler'
func (dh *DefaultHandler) HandleLaunchJobReq(w http.ResponseWriter, req *http.Request) {
	// 检测是否获取到 control
	if !dh.preCheck(w, req) {
		return
	}

	data, err := ioutil.ReadAll(req.Body)
	if err != nil {
		dh.handleError(w, req, http.StatusInternalServerError, errs.ReadRequestBodyError(err))
		return
	}

	// unmarshal data
	jobReq := models.JobRequest{}
	if err = json.Unmarshal(data, &jobReq); err != nil {
		dh.handleError(w, req, http.StatusInternalServerError, errs.HandleJSONDataError(err))
		return
	}

	// Pass request to the controller for the follow-up.
	// 将 request 交给 controller 进行控制，根据 job 类型放入不同的任务队列，检测是否带有 hook
	jobStats, err := dh.controller.LaunchJob(jobReq)
	if err != nil {
		if errs.IsConflictError(err) {
			// Conflict error
			dh.handleError(w, req, http.StatusConflict, err)
		} else {
			// General error
			dh.handleError(w, req, http.StatusInternalServerError, errs.LaunchJobError(err))
		}
		return
	}
	// 将处理好的 job 状态信息发送回去
	dh.handleJSONData(w, req, http.StatusAccepted, jobStats)
}

// HandleGetJobReq is implementation of method defined in interface 'Handler'
func (dh *DefaultHandler) HandleGetJobReq(w http.ResponseWriter, req *http.Request) {
	if !dh.preCheck(w, req) {
		return
	}

	// 获取路由中的参数
	vars := mux.Vars(req)
	jobID := vars["job_id"]

	// 调用 redis 控制其来获取指定 job 的状态信息，经历了多个接口类型的函数，core -> pool -> opm
	jobStats, err := dh.controller.GetJob(jobID)
	if err != nil {
		code := http.StatusInternalServerError
		backErr := errs.GetJobStatsError(err)
		if errs.IsObjectNotFoundError(err) {
			code = http.StatusNotFound
			backErr = err
		}
		dh.handleError(w, req, code, backErr)
		return
	}

	// 发送数据给 core
	dh.handleJSONData(w, req, http.StatusOK, jobStats)
}

// HandleJobActionReq is implementation of method defined in interface 'Handler'
// 对 job 进行控制 stop/cancel
func (dh *DefaultHandler) HandleJobActionReq(w http.ResponseWriter, req *http.Request) {
	if !dh.preCheck(w, req) {
		return
	}

	vars := mux.Vars(req)
	jobID := vars["job_id"]

	data, err := ioutil.ReadAll(req.Body)
	if err != nil {
		dh.handleError(w, req, http.StatusInternalServerError, errs.ReadRequestBodyError(err))
		return
	}

	// unmarshal data。只有一个参数 action
	jobActionReq := models.JobActionRequest{}
	if err = json.Unmarshal(data, &jobActionReq); err != nil {
		dh.handleError(w, req, http.StatusInternalServerError, errs.HandleJSONDataError(err))
		return
	}

	// stop cancel retry 三中操作
	switch jobActionReq.Action {
	case opm.CtlCommandStop:
		if err := dh.controller.StopJob(jobID); err != nil {
			code := http.StatusInternalServerError
			backErr := errs.StopJobError(err)
			if errs.IsObjectNotFoundError(err) {
				code = http.StatusNotFound
				backErr = err
			}
			dh.handleError(w, req, code, backErr)
			return
		}
	case opm.CtlCommandCancel:
		if err := dh.controller.CancelJob(jobID); err != nil {
			code := http.StatusInternalServerError
			backErr := errs.CancelJobError(err)
			if errs.IsObjectNotFoundError(err) {
				code = http.StatusNotFound
				backErr = err
			}
			dh.handleError(w, req, code, backErr)
			return
		}
	case opm.CtlCommandRetry:
		if err := dh.controller.RetryJob(jobID); err != nil {
			code := http.StatusInternalServerError
			backErr := errs.RetryJobError(err)
			if errs.IsObjectNotFoundError(err) {
				code = http.StatusNotFound
				backErr = err
			}
			dh.handleError(w, req, code, backErr)
			return
		}
	default:
		dh.handleError(w, req, http.StatusNotImplemented, errs.UnknownActionNameError(fmt.Errorf("%s", jobID)))
		return
	}

	// 日志记录
	dh.log(req, http.StatusNoContent, string(data))

	w.WriteHeader(http.StatusNoContent) // only header, no content returned
}

// HandleCheckStatusReq is implementation of method defined in interface 'Handler'
// 健康检查，获取 redis 的健康状态
func (dh *DefaultHandler) HandleCheckStatusReq(w http.ResponseWriter, req *http.Request) {
	if !dh.preCheck(w, req) {
		return
	}

	stats, err := dh.controller.CheckStatus()
	if err != nil {
		dh.handleError(w, req, http.StatusInternalServerError, errs.CheckStatsError(err))
		return
	}

	dh.handleJSONData(w, req, http.StatusOK, stats)
}

// HandleJobLogReq is implementation of method defined in interface 'Handler'
func (dh *DefaultHandler) HandleJobLogReq(w http.ResponseWriter, req *http.Request) {
	if !dh.preCheck(w, req) {
		return
	}

	vars := mux.Vars(req)
	jobID := vars["job_id"]

	if strings.Contains(jobID, "..") || strings.ContainsRune(jobID, os.PathSeparator) {
		dh.handleError(w, req, http.StatusBadRequest, fmt.Errorf("Invalid Job ID: %s", jobID))
		return
	}

	logData, err := dh.controller.GetJobLogData(jobID)
	if err != nil {
		code := http.StatusInternalServerError
		backErr := errs.GetJobLogError(err)
		if errs.IsObjectNotFoundError(err) {
			code = http.StatusNotFound
			backErr = err
		}
		dh.handleError(w, req, code, backErr)
		return
	}

	dh.log(req, http.StatusOK, "")

	w.WriteHeader(http.StatusOK)
	w.Write(logData)
}

func (dh *DefaultHandler) handleJSONData(w http.ResponseWriter, req *http.Request, code int, object interface{}) {
	data, err := json.Marshal(object)
	if err != nil {
		dh.handleError(w, req, http.StatusInternalServerError, errs.HandleJSONDataError(err))
		return
	}

	logger.Debugf("Serve http request '%s %s': %d %s", req.Method, req.URL.String(), code, data)

	w.Header().Set(http.CanonicalHeaderKey("Accept"), "application/json")
	w.Header().Set(http.CanonicalHeaderKey("content-type"), "application/json")
	w.WriteHeader(code)
	w.Write(data)
}

func (dh *DefaultHandler) handleError(w http.ResponseWriter, req *http.Request, code int, err error) {
	// Log all errors
	logger.Errorf("Serve http request '%s %s' error: %d %s", req.Method, req.URL.String(), code, err.Error())

	w.WriteHeader(code)
	w.Write([]byte(err.Error()))
}

func (dh *DefaultHandler) preCheck(w http.ResponseWriter, req *http.Request) bool {
	if dh.controller == nil {
		dh.handleError(w, req, http.StatusInternalServerError, errs.MissingBackendHandlerError(fmt.Errorf("nil controller")))
		return false
	}

	return true
}

func (dh *DefaultHandler) log(req *http.Request, code int, text string) {
	logger.Debugf("Serve http request '%s %s': %d %s", req.Method, req.URL.String(), code, text)
}
