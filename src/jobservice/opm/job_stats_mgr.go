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

package opm

import "github.com/goharbor/harbor/src/jobservice/models"

// Range for list scope defining
type Range int

// JobStatsManager defines the methods to handle stats of job.
// 用来处理 job 的处理状态。
type JobStatsManager interface {
	// Start to serve
	Start()

	// Shutdown the manager
	Shutdown()

	// Save the job stats
	// Async method to retry and improve performance
	//
	// jobStats models.JobStats : the job stats to be saved
	// 存储工作状态?是保存到数据库还是内存中？
	Save(jobStats models.JobStats)

	// Get the job stats from backend store
	// Sync method as we need the data
	//
	// Returns:
	//  models.JobStats : job stats data
	//  error           : error if meet any problems
	// 从后端存储中获取 job 状态
	Retrieve(jobID string) (models.JobStats, error)

	// Update the properties of the job stats
	//
	// jobID string                  : ID of the being retried job
	// fieldAndValues ...interface{} : One or more properties being updated
	//
	// Returns:
	//  error if update failed
	// 更新 job 状态的参数
	Update(jobID string, fieldAndValues ...interface{}) error

	// SetJobStatus will mark the status of job to the specified one
	// Async method to retry
	// 指定 job 的状态
	SetJobStatus(jobID string, status string)

	// Send command fro the specified job
	//
	// jobID string   : ID of the being retried job
	// command string : the command applied to the job like stop/cancel
	// isCached bool  : to indicate if only cache the op command
	//
	// Returns:
	//  error if it was not successfully sent
	// 给指定的 job 发送 command
	SendCommand(jobID string, command string, isCached bool) error

	// CtlCommand checks if control command is fired for the specified job.
	//
	// jobID string : ID of the job
	//
	// Returns:
	//  the command if it was fired
	//  error if it was not fired yet to meet some other problems
	// 检测 job 的 control command 是否存在
	CtlCommand(jobID string) (string, error)

	// CheckIn message for the specified job like detailed progress info.
	//
	// jobID string   : ID of the job
	// message string : The message being checked in
	//
	CheckIn(jobID string, message string)

	// DieAt marks the failed jobs with the time they put into dead queue.
	//
	// jobID string   : ID of the job
	// message string : The message being checked in
	//
	// 用时间标记失败的任务
	DieAt(jobID string, dieAt int64)

	// RegisterHook is used to save the hook url or cache the url in memory.
	//
	// jobID string   : ID of job
	// hookURL string : the hook url being registered
	// isCached bool  :  to indicate if only cache the hook url
	//
	// Returns:
	//  error if meet any problems
	// 保存 hook url 或者在内存中存储
	RegisterHook(jobID string, hookURL string, isCached bool) error

	// Get hook returns the web hook url for the specified job if it is registered
	//
	// jobID string   : ID of job
	//
	// Returns:
	//  the web hook url if existing
	//  non-nil error if meet any problems
	// 获取指定 job 的 hook url
	GetHook(jobID string) (string, error)

	// Mark the periodic job stats expired
	//
	// jobID string   : ID of job
	//
	// Returns:
	//  error if meet any problems
	// 将指定的周期性job 标记为过期的
	ExpirePeriodicJobStats(jobID string) error

	// Persist the links between upstream job and the executions.
	//
	// upstreamJobID string: ID of the upstream job
	// executions  ...string: IDs of the execution jobs
	//
	// Returns:
	//  error if meet any issues
	// 持久化存储 上游 job 和当前 job 的执行之间的关系。
	AttachExecution(upstreamJobID string, executions ...string) error

	// Get all the executions (IDs) fro the specified upstream Job.
	//
	// upstreamJobID string: ID of the upstream job
	// ranges      ...Range: Define the start and end for the list, e.g:
	//   0, 10 means [0:10]
	//   10 means [10:]
	//   empty means [0:-1]==all
	// Returns:
	//  the ID list of the executions if no error occurred
	//  or a non-nil error is returned
	// 从指定的上游 job，获取所有正在执行的 jobid
	GetExecutions(upstreamJobID string, ranges ...Range) ([]string, error)
}
