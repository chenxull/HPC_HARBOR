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

package core

import (
	"errors"
	"fmt"

	"github.com/goharbor/harbor/src/jobservice/logger"

	"github.com/goharbor/harbor/src/jobservice/job"
	"github.com/goharbor/harbor/src/jobservice/models"
	"github.com/goharbor/harbor/src/jobservice/pool"
	"github.com/goharbor/harbor/src/jobservice/utils"
	"github.com/robfig/cron"
)

const (
	hookActivated   = "activated"
	hookDeactivated = "error"
)

// Controller implement the core interface and provides related job handle methods.
// Controller will coordinate the lower components to complete the process as a commander role.
// Controller 充当一个指挥官的角色
type Controller struct {
	// Refer the backend pool
	// Controller 结构体继承了 pool的 interface，用来实现对 redis pool 的各种操作.
	backendPool pool.Interface
}

// NewController is constructor of Controller.
func NewController(backendPool pool.Interface) *Controller {
	return &Controller{
		backendPool: backendPool,
	}
}

// LaunchJob is implementation of same method in core interface.
func (c *Controller) LaunchJob(req models.JobRequest) (models.JobStats, error) {
	if err := validJobReq(req); err != nil {
		return models.JobStats{}, err
	}

	// Validate job name
	// 从 redis 中查询，此job 的名字是否存储在 redis 中
	jobType, isKnownJob := c.backendPool.IsKnownJob(req.Job.Name)
	if !isKnownJob {
		return models.JobStats{}, fmt.Errorf("job with name '%s' is unknown", req.Job.Name)
	}

	// Validate parameters。通过接口反射的方式 根据 jobType 获取到了job 的具体类型（GC,Clair 等）
	if err := c.backendPool.ValidateJobParameters(jobType, req.Job.Parameters); err != nil {
		return models.JobStats{}, err
	}

	// Enqueue job regarding of the kind
	var (
		res models.JobStats
		err error
	)
	// 根据 job 类型将其安排到不同的调度队列中：Scheduled，Periodic，Generic
	switch req.Job.Metadata.JobKind {
	case job.JobKindScheduled:
		res, err = c.backendPool.Schedule(
			req.Job.Name,
			req.Job.Parameters,
			req.Job.Metadata.ScheduleDelay,
			req.Job.Metadata.IsUnique)
	case job.JobKindPeriodic:
		res, err = c.backendPool.PeriodicallyEnqueue(
			req.Job.Name,
			req.Job.Parameters,
			req.Job.Metadata.Cron)
	default:
		// 默认工作类型为 Generic，镜像扫描任务就是属于此种类型
		res, err = c.backendPool.Enqueue(req.Job.Name, req.Job.Parameters, req.Job.Metadata.IsUnique)
	}

	// Register status hook?
	if err == nil {
		if !utils.IsEmptyStr(req.Job.StatusHook) {
			// 在 redis 中注册 jobid+status hook。这个 hook 是用来将数据发送给 core 组件的，jobID 正好在数据库 img_scan_job表中
			if err := c.backendPool.RegisterHook(res.Stats.JobID, req.Job.StatusHook); err != nil {
				res.Stats.HookStatus = hookDeactivated
			} else {
				res.Stats.HookStatus = hookActivated
			}
		}
	}

	return res, err
}

// GetJob is implementation of same method in core interface.
func (c *Controller)  GetJob(jobID string) (models.JobStats, error) {
	if utils.IsEmptyStr(jobID) {
		return models.JobStats{}, errors.New("empty job ID")
	}

	// 从 后端的 redis 中获取信息
	return c.backendPool.GetJobStats(jobID)
}

// StopJob is implementation of same method in core interface.
func (c *Controller) StopJob(jobID string) error {
	if utils.IsEmptyStr(jobID) {
		return errors.New("empty job ID")
	}

	return c.backendPool.StopJob(jobID)
}

// CancelJob is implementation of same method in core interface.
func (c *Controller) CancelJob(jobID string) error {
	if utils.IsEmptyStr(jobID) {
		return errors.New("empty job ID")
	}

	return c.backendPool.CancelJob(jobID)
}

// RetryJob is implementation of same method in core interface.
func (c *Controller) RetryJob(jobID string) error {
	if utils.IsEmptyStr(jobID) {
		return errors.New("empty job ID")
	}

	return c.backendPool.RetryJob(jobID)
}

// GetJobLogData is used to return the log text data for the specified job if exists
func (c *Controller) GetJobLogData(jobID string) ([]byte, error) {
	if utils.IsEmptyStr(jobID) {
		return nil, errors.New("empty job ID")
	}

	logData, err := logger.Retrieve(jobID)
	if err != nil {
		return nil, err
	}

	return logData, nil
}

// CheckStatus is implementation of same method in core interface.
func (c *Controller) CheckStatus() (models.JobPoolStats, error) {
	return c.backendPool.Stats()
}

/*
	 验证函数的逻辑如下：
		1. 检测不同类型任务需要满足的基本条件
*/
func validJobReq(req models.JobRequest) error {
	if req.Job == nil {
		return errors.New("empty job request is not allowed")
	}

	if utils.IsEmptyStr(req.Job.Name) {
		return errors.New("name of job must be specified")
	}

	if req.Job.Metadata == nil {
		return errors.New("metadata of job is missing")
	}

	if req.Job.Metadata.JobKind != job.JobKindGeneric &&
		req.Job.Metadata.JobKind != job.JobKindPeriodic &&
		req.Job.Metadata.JobKind != job.JobKindScheduled {
		return fmt.Errorf(
			"job kind '%s' is not supported, only support '%s','%s','%s'",
			req.Job.Metadata.JobKind,
			job.JobKindGeneric,
			job.JobKindScheduled,
			job.JobKindPeriodic)
	}

	if req.Job.Metadata.JobKind == job.JobKindScheduled &&
		req.Job.Metadata.ScheduleDelay == 0 {
		return fmt.Errorf("'schedule_delay' must be specified if the job kind is '%s'", job.JobKindScheduled)
	}

	if req.Job.Metadata.JobKind == job.JobKindPeriodic {
		if utils.IsEmptyStr(req.Job.Metadata.Cron) {
			return fmt.Errorf("'cron_spec' must be specified if the job kind is '%s'", job.JobKindPeriodic)
		}

		if _, err := cron.Parse(req.Job.Metadata.Cron); err != nil {
			return fmt.Errorf("'cron_spec' is not correctly set: %s", err)
		}
	}

	return nil
}
