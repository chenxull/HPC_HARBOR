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

package pool

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"

	"github.com/gocraft/work"
	"github.com/goharbor/harbor/src/jobservice/env"
	"github.com/goharbor/harbor/src/jobservice/job"
	"github.com/goharbor/harbor/src/jobservice/logger"
	"github.com/goharbor/harbor/src/jobservice/models"
	"github.com/goharbor/harbor/src/jobservice/opm"
	"github.com/goharbor/harbor/src/jobservice/period"
	"github.com/goharbor/harbor/src/jobservice/utils"
	"github.com/gomodule/redigo/redis"
)

var (
	workerPoolDeadTime = 10 * time.Second
)

const (
	workerPoolStatusHealthy = "Healthy"
	workerPoolStatusDead    = "Dead"

	// Copy from period.enqueuer
	periodicEnqueuerHorizon = 4 * time.Minute

	pingRedisMaxTimes = 10
)

// GoCraftWorkPool is the pool implementation based on gocraft/work powered by redis.
type GoCraftWorkPool struct {
	namespace     string
	redisPool     *redis.Pool
	pool          *work.WorkerPool
	enqueuer      *work.Enqueuer
	sweeper       *period.Sweeper
	client        *work.Client
	context       *env.Context
	scheduler     period.Interface
	statsManager  opm.JobStatsManager
	messageServer *MessageServer
	deDuplicator  DeDuplicator

	// no need to sync as write once and then only read
	// key is name of known job
	// value is the type of known job
	knownJobs map[string]interface{}
}

// RedisPoolContext ...
// We did not use this context to pass context info so far, just a placeholder.
type RedisPoolContext struct{}

// NewGoCraftWorkPool is constructor of goCraftWorkPool.
// 创建一个 work pool 所需要的东西，任务队列，访问 redis 的客户端
//调度器，日志清理器，消息通知服务器，
func NewGoCraftWorkPool(ctx *env.Context, namespace string, workerCount uint, redisPool *redis.Pool) *GoCraftWorkPool {
	pool := work.NewWorkerPool(RedisPoolContext{}, workerCount, namespace, redisPool)
	enqueuer := work.NewEnqueuer(namespace, redisPool)
	client := work.NewClient(namespace, redisPool)
	// jobStats 信息管理器
	statsMgr := opm.NewRedisJobStatsManager(ctx.SystemContext, namespace, redisPool)
	// 周期性任务调度器
	scheduler := period.NewRedisPeriodicScheduler(ctx, namespace, redisPool, statsMgr)
	sweeper := period.NewSweeper(namespace, redisPool, client)
	// 消息服务器
	msgServer := NewMessageServer(ctx.SystemContext, namespace, redisPool)
	// 复制器
	deDepulicator := NewRedisDeDuplicator(namespace, redisPool)
	return &GoCraftWorkPool{
		namespace:     namespace,
		redisPool:     redisPool,
		pool:          pool,
		enqueuer:      enqueuer,
		scheduler:     scheduler,
		sweeper:       sweeper,
		client:        client,
		context:       ctx,
		statsManager:  statsMgr,
		knownJobs:     make(map[string]interface{}),
		messageServer: msgServer,
		deDuplicator:  deDepulicator,
	}
}

// Start to serve
// Unblock action
func (gcwp *GoCraftWorkPool) Start() error {
	if gcwp.redisPool == nil ||
		gcwp.pool == nil ||
		gcwp.context.SystemContext == nil {
		// report and exit
		return errors.New("Redis worker pool can not start as it's not correctly configured")
	}

	// Test the redis connection
	if err := gcwp.ping(); err != nil {
		return err
	}

	done := make(chan interface{}, 1)
	// 非阻塞的
	gcwp.context.WG.Add(1)
	go func() {
		var err error

		defer func() {
			gcwp.context.WG.Done()
			if err != nil {
				// report error
				gcwp.context.ErrorChan <- err
				done <- struct{}{} // exit immediately
			}
		}()

		// Register callbacks。注册事件对应回调处理函数
		if err = gcwp.messageServer.Subscribe(period.EventSchedulePeriodicPolicy,
			func(data interface{}) error {
				// 在内存中存储周期性的任务数据,数据的类型必须为 PeriodicJobPolicy
				return gcwp.handleSchedulePolicy(data)
			}); err != nil {
			return
		}
		if err = gcwp.messageServer.Subscribe(period.EventUnSchedulePeriodicPolicy,
			func(data interface{}) error {
				// 从内存中取消指定的周期性的任务
				return gcwp.handleUnSchedulePolicy(data)
			}); err != nil {
			return
		}
		if err = gcwp.messageServer.Subscribe(opm.EventRegisterStatusHook,
			func(data interface{}) error {
				//  在内存中存储 hook_url。这里定义了如何处理 hook_url
				return gcwp.handleRegisterStatusHook(data)
			}); err != nil {
			return
		}
		if err = gcwp.messageServer.Subscribe(opm.EventFireCommand,
			func(data interface{}) error {
				//  将任务的操作加入到maintaining list中
				return gcwp.handleOPCommandFiring(data)
			}); err != nil {
			return
		}

		startTimes := 0
	START_MSG_SERVER:
		// Start message server
		// messageServer 就是实现消息异步交换的处理器
		if err = gcwp.messageServer.Start(); err != nil {
			logger.Errorf("Message server exits with error: %s\n", err.Error())
			if startTimes < msgServerRetryTimes {
				startTimes++
				time.Sleep(time.Duration((int)(math.Pow(2, (float64)(startTimes)))+5) * time.Second)
				logger.Infof("Restart message server (%d times)\n", startTimes)
				goto START_MSG_SERVER
			}

			return
		}
	}()

	gcwp.context.WG.Add(1)
	go func() {
		defer func() {
			gcwp.context.WG.Done()
			gcwp.statsManager.Shutdown()
		}()
		// Start stats manager。状态管理器是非阻塞的
		// None-blocking
		gcwp.statsManager.Start()

		// blocking call。任务调度是阻塞的，调度器是专门针对于周期性任务来设计的。
		//启动任务队列接受任务。实现比较复杂
		gcwp.scheduler.Start()
	}()

	gcwp.context.WG.Add(1)

	go func() {
		defer func() {
			gcwp.context.WG.Done()
			logger.Infof("Redis worker pool is stopped")
		}()

		// Clear dirty data before pool starting
		if err := gcwp.sweeper.ClearOutdatedScheduledJobs(); err != nil {
			// Only logged
			logger.Errorf("Clear outdated data before pool starting failed with error:%s\n", err)
		}

		// Append middlewares。增加 workpool 的中间件，用来携带日志功能
		// 当接受到任务时，会在日志中记录
		gcwp.pool.Middleware((*RedisPoolContext).logJob)
		//启动 worker pool，不停的监听 redis 中的任务
		gcwp.pool.Start()
		logger.Infof("Redis worker pool is started")

		// Block on listening context and done signal
		select {
		case <-gcwp.context.SystemContext.Done():
		case <-done:
		}

		gcwp.pool.Stop()
	}()

	return nil
}

// RegisterJob is used to register the job to the pool.
// j is the type of job
// 在 RegisterJob 时 就已经开始执行 job 了
func (gcwp *GoCraftWorkPool) RegisterJob(name string, j interface{}) error {
	if utils.IsEmptyStr(name) || j == nil {
		return errors.New("job can not be registered with empty name or nil interface")
	}

	// j must be job.Interface
	if _, ok := j.(job.Interface); !ok {
		return errors.New("job must implement the job.Interface")
	}

	// 1:1 constraint
	if jInList, ok := gcwp.knownJobs[name]; ok {
		return fmt.Errorf("Job name %s has been already registered with %s", name, reflect.TypeOf(jInList).String())
	}

	// Same job implementation can be only registered with one name
	// 注册过的 job 就需要被执行
	for jName, jInList := range gcwp.knownJobs {
		// 判断 job 的类型，在调用不同的 接口实现
		// j 是结构体指针，通过反射之后获得的是此 job 结构体的实现
		jobImpl := reflect.TypeOf(j).String()
		// 判断已有的 job 类型和传入的 job 实现的类型是否相同
		if reflect.TypeOf(jInList).String() == jobImpl {
			return fmt.Errorf("Job %s has been already registered with name %s", jobImpl, jName)
		}
	}

	// 创建需要 redis 执行的任务
	redisJob := NewRedisJob(j, gcwp.context, gcwp.statsManager, gcwp.deDuplicator)

	// Get more info from j
	theJ := Wrap(j)

	// 给每种类型的 job 创建执行函数。提前在 redis 中注册好。当redis 中有新 job 时，只需调用对应处理函数即可
	// 将其存储到 work pool 中
	gcwp.pool.JobWithOptions(name,
		work.JobOptions{MaxFails: theJ.MaxFails()},
		func(job *work.Job) error {
			// 执行 job 的逻辑重点
			return redisJob.Run(job)
		}, // Use generic handler to handle as we do not accept context with this way. 这只是测试用 ？
	)
	gcwp.knownJobs[name] = j // keep the name of registered jobs as known jobs for future validation

	logger.Infof("Register job %s with name %s", reflect.TypeOf(j).String(), name)

	return nil
}

// RegisterJobs is used to register multiple jobs to pool.
func (gcwp *GoCraftWorkPool) RegisterJobs(jobs map[string]interface{}) error {
	if jobs == nil || len(jobs) == 0 {
		return nil
	}

	for name, j := range jobs {
		if err := gcwp.RegisterJob(name, j); err != nil {
			return err
		}
	}

	return nil
}

// Enqueue job
//
func (gcwp *GoCraftWorkPool) Enqueue(jobName string, params models.Parameters, isUnique bool) (models.JobStats, error) {
	var (
		j   *work.Job
		err error
	)

	// As the job is declared to be unique,
	// check the uniqueness of the job,
	// if no duplicated job existing (including the running jobs),
	// set the unique flag.
	// 检测此 job 是队列中唯一的，避免重复操作
	if isUnique {
		if err = gcwp.deDuplicator.Unique(jobName, params); err != nil {
			return models.JobStats{}, err
		}

		if j, err = gcwp.enqueuer.EnqueueUnique(jobName, params); err != nil {
			return models.JobStats{}, err
		}
	} else {
		// Enqueue job.将 job 入工作队列。返回的是一个 job 类型。
		// 目前猜测，harbor 中的 jobservice 可能是从工作框架中获取任务来进行执行的
		if j, err = gcwp.enqueuer.Enqueue(jobName, params); err != nil {
			return models.JobStats{}, err
		}
	}

	// avoid backend pool bug
	if j == nil {
		return models.JobStats{}, fmt.Errorf("job '%s' can not be enqueued, please check the job metatdata", jobName)
	}

	// 构造 job 的状态信息，将执行状态设置为 Pending准备执行
	res := generateResult(j, job.JobKindGeneric, isUnique)
	// Save data with async way. Once it fails to do, let it escape
	// The client method may help if the job is still in progress when get stats of this job
	// 消息会放入 statsManager 的 processChan 中。推测有协程在监控着数据的变化
	gcwp.statsManager.Save(res)

	return res, nil
}

// Schedule job
func (gcwp *GoCraftWorkPool) Schedule(jobName string, params models.Parameters, runAfterSeconds uint64, isUnique bool) (models.JobStats, error) {
	var (
		j   *work.ScheduledJob
		err error
	)

	// As the job is declared to be unique,
	// check the uniqueness of the job,
	// if no duplicated job existing (including the running jobs),
	// set the unique flag.
	if isUnique {
		if err = gcwp.deDuplicator.Unique(jobName, params); err != nil {
			return models.JobStats{}, err
		}

		if j, err = gcwp.enqueuer.EnqueueUniqueIn(jobName, int64(runAfterSeconds), params); err != nil {
			return models.JobStats{}, err
		}
	} else {
		// Enqueue job in
		if j, err = gcwp.enqueuer.EnqueueIn(jobName, int64(runAfterSeconds), params); err != nil {
			return models.JobStats{}, err
		}
	}

	// avoid backend pool bug
	if j == nil {
		return models.JobStats{}, fmt.Errorf("job '%s' can not be enqueued, please check the job metatdata", jobName)
	}

	res := generateResult(j.Job, job.JobKindScheduled, isUnique)
	res.Stats.RunAt = j.RunAt

	// As job is already scheduled, we should not block this call
	// Once it fails to do, use client method to help get the status of the escape job
	gcwp.statsManager.Save(res)

	return res, nil
}

// PeriodicallyEnqueue job
func (gcwp *GoCraftWorkPool) PeriodicallyEnqueue(jobName string, params models.Parameters, cronSetting string) (models.JobStats, error) {
	id, nextRun, err := gcwp.scheduler.Schedule(jobName, params, cronSetting)
	if err != nil {
		return models.JobStats{}, err
	}

	res := models.JobStats{
		Stats: &models.JobStatData{
			JobID:                id,
			JobName:              jobName,
			Status:               job.JobStatusPending,
			JobKind:              job.JobKindPeriodic,
			CronSpec:             cronSetting,
			EnqueueTime:          time.Now().Unix(),
			UpdateTime:           time.Now().Unix(),
			RefLink:              fmt.Sprintf("/api/v1/jobs/%s", id),
			RunAt:                nextRun,
			IsMultipleExecutions: true, // True for periodic job
		},
	}

	gcwp.statsManager.Save(res)

	return res, nil
}

// GetJobStats return the job stats of the specified enqueued job.
func (gcwp *GoCraftWorkPool) GetJobStats(jobID string) (models.JobStats, error) {
	if utils.IsEmptyStr(jobID) {
		return models.JobStats{}, errors.New("empty job ID")
	}

	// 在后端的 redis worker pool 中还定义了一个状态管理器，专门用来管理 job 状态信息
	return gcwp.statsManager.Retrieve(jobID)
}

// Stats of pool
func (gcwp *GoCraftWorkPool) Stats() (models.JobPoolStats, error) {
	// Get the status of workerpool via client
	// 使用 client 向 redis 发送心跳请求
	hbs, err := gcwp.client.WorkerPoolHeartbeats()
	if err != nil {
		return models.JobPoolStats{}, err
	}

	// Find the heartbeat of this pool via pid
	stats := make([]*models.JobPoolStatsData, 0)
	for _, hb := range hbs {
		if hb.HeartbeatAt == 0 {
			continue // invalid ones
		}

		wPoolStatus := workerPoolStatusHealthy
		if time.Unix(hb.HeartbeatAt, 0).Add(workerPoolDeadTime).Before(time.Now()) {
			wPoolStatus = workerPoolStatusDead
		}
		stat := &models.JobPoolStatsData{
			WorkerPoolID: hb.WorkerPoolID,
			StartedAt:    hb.StartedAt,
			HeartbeatAt:  hb.HeartbeatAt,
			JobNames:     hb.JobNames,
			Concurrency:  hb.Concurrency,
			Status:       wPoolStatus,
		}
		stats = append(stats, stat)
	}

	if len(stats) == 0 {
		return models.JobPoolStats{}, errors.New("Failed to get stats of worker pools")
	}

	return models.JobPoolStats{
		Pools: stats,
	}, nil
}

// StopJob will stop the job
func (gcwp *GoCraftWorkPool) StopJob(jobID string) error {
	if utils.IsEmptyStr(jobID) {
		return errors.New("empty job ID")
	}

	// 先获取 job status
	theJob, err := gcwp.statsManager.Retrieve(jobID)
	if err != nil {
		return err
	}

	// 根据 job 的种类，执行不同的 stop 策略
	switch theJob.Stats.JobKind {
	// 对于 generic 类型的 job 来说，只有处于 running 状态的才可以 stop
	case job.JobKindGeneric:
		// Only running job can be stopped
		if theJob.Stats.Status != job.JobStatusRunning {
			return fmt.Errorf("job '%s' is not a running job", jobID)
		}
	case job.JobKindScheduled:
		// we need to delete the scheduled job in the queue if it is not running yet
		// otherwise, stop it.
		// 对于 scheduled 类型的 job，如果处于等待状态，直接从调度表中删除。然后将其状态更新为 stopped
		if theJob.Stats.Status == job.JobStatusPending {
			if err := gcwp.client.DeleteScheduledJob(theJob.Stats.RunAt, jobID); err != nil {
				return err
			}

			// Update the job status to 'stopped'
			gcwp.statsManager.SetJobStatus(jobID, job.JobStatusStopped)

			logger.Debugf("Scheduled job which plan to run at %d '%s' is stopped", theJob.Stats.RunAt, jobID)

			return nil
		}
		/*
		对于 Periodic 类型的 job ，需要进行多步操作。
		1. 删除 周期性job 的 policy
		2. 删除用来调度这个周期性 job 的实例
		3. 让其过期
		*/
	case job.JobKindPeriodic:
		// firstly delete the periodic job policy
		if err := gcwp.scheduler.UnSchedule(jobID); err != nil {
			return err
		}

		logger.Infof("Periodic job policy %s is removed", jobID)

		// secondly we need try to delete the job instances scheduled for this periodic job, a try best action
		if err := gcwp.deleteScheduledJobsOfPeriodicPolicy(theJob.Stats.JobID); err != nil {
			// only logged
			logger.Errorf("Errors happened when deleting jobs of periodic policy %s: %s", theJob.Stats.JobID, err)
		}

		// thirdly expire the job stats of this periodic job if exists
		if err := gcwp.statsManager.ExpirePeriodicJobStats(theJob.Stats.JobID); err != nil {
			// only logged
			logger.Errorf("Expire the stats of job %s failed with error: %s\n", theJob.Stats.JobID, err)
		}

		return nil
	default:
		return fmt.Errorf("Job kind %s is not supported", theJob.Stats.JobKind)
	}

	// Check if the job has 'running' instance
	if theJob.Stats.Status == job.JobStatusRunning {
		// Send 'stop' ctl command to the running instance
		if err := gcwp.statsManager.SendCommand(jobID, opm.CtlCommandStop, false); err != nil {
			return err
		}
	}

	return nil
}

// CancelJob will cancel the job
func (gcwp *GoCraftWorkPool) CancelJob(jobID string) error {
	if utils.IsEmptyStr(jobID) {
		return errors.New("empty job ID")
	}

	theJob, err := gcwp.statsManager.Retrieve(jobID)
	if err != nil {
		return err
	}

	switch theJob.Stats.JobKind {
	case job.JobKindGeneric:
		if theJob.Stats.Status != job.JobStatusRunning {
			return fmt.Errorf("only running job can be cancelled, job '%s' seems not running now", theJob.Stats.JobID)
		}

		// Send 'cancel' ctl command to the running instance
		if err := gcwp.statsManager.SendCommand(jobID, opm.CtlCommandCancel, false); err != nil {
			return err
		}
		break
	default:
		return fmt.Errorf("job kind '%s' does not support 'cancel' operation", theJob.Stats.JobKind)
	}

	return nil
}

// RetryJob retry the job
func (gcwp *GoCraftWorkPool) RetryJob(jobID string) error {
	if utils.IsEmptyStr(jobID) {
		return errors.New("empty job ID")
	}

	theJob, err := gcwp.statsManager.Retrieve(jobID)
	if err != nil {
		return err
	}

	if theJob.Stats.DieAt == 0 {
		return fmt.Errorf("job '%s' is not a retryable job", jobID)
	}

	return gcwp.client.RetryDeadJob(theJob.Stats.DieAt, jobID)
}

// IsKnownJob ...
func (gcwp *GoCraftWorkPool) IsKnownJob(name string) (interface{}, bool) {
	v, ok := gcwp.knownJobs[name]
	return v, ok
}

// ValidateJobParameters ...
func (gcwp *GoCraftWorkPool) ValidateJobParameters(jobType interface{}, params map[string]interface{}) error {
	if jobType == nil {
		return errors.New("nil job type")
	}

	// 获取job 的类型，指扫描任务，GC，垃圾回收等
	theJ := Wrap(jobType)
	// theJ已经获取 job 的类型，下面会自动更具 job 的类型调用对应的验证函数。clair 的执行返回 nil
	return theJ.Validate(params)
}

// RegisterHook registers status hook url
// sync method
func (gcwp *GoCraftWorkPool) RegisterHook(jobID string, hookURL string) error {
	if utils.IsEmptyStr(jobID) {
		return errors.New("empty job ID")
	}

	if !utils.IsValidURL(hookURL) {
		return errors.New("invalid hook url")
	}

	return gcwp.statsManager.RegisterHook(jobID, hookURL, false)
}

// A try best method to delete the scheduled jobs of one periodic job
func (gcwp *GoCraftWorkPool) deleteScheduledJobsOfPeriodicPolicy(policyID string) error {
	// Check the scope of [-periodicEnqueuerHorizon, -1]
	// If the job is still not completed after a 'periodicEnqueuerHorizon', just ignore it
	now := time.Now().Unix() // Baseline
	startTime := now - (int64)(periodicEnqueuerHorizon/time.Minute)*60

	// Try to delete more
	// Get the range scope
	start := (opm.Range)(startTime)
	ids, err := gcwp.statsManager.GetExecutions(policyID, start)
	if err != nil {
		return err
	}

	logger.Debugf("Found scheduled jobs '%v' in scope [%d,+inf] for periodic job policy %s", ids, start, policyID)

	if len(ids) == 0 {
		// Treat as a normal case, nothing need to do
		return nil
	}

	multiErrs := []string{}
	for _, id := range ids {
		subJob, err := gcwp.statsManager.Retrieve(id)
		if err != nil {
			multiErrs = append(multiErrs, err.Error())
			continue // going on
		}

		if subJob.Stats.Status == job.JobStatusRunning {
			// Send 'stop' ctl command to the running instance
			if err := gcwp.statsManager.SendCommand(subJob.Stats.JobID, opm.CtlCommandStop, false); err != nil {
				multiErrs = append(multiErrs, err.Error())
				continue
			}

			logger.Debugf("Stop running job %s for periodic job policy %s", subJob.Stats.JobID, policyID)
		} else {
			if subJob.Stats.JobKind == job.JobKindScheduled &&
				subJob.Stats.Status == job.JobStatusPending {
				// The pending scheduled job
				if err := gcwp.client.DeleteScheduledJob(subJob.Stats.RunAt, subJob.Stats.JobID); err != nil {
					multiErrs = append(multiErrs, err.Error())
					continue // going on
				}

				// Log action
				logger.Debugf("Delete scheduled job for periodic job policy %s: runat = %d", policyID, subJob.Stats.RunAt)
			}
		}
	}

	if len(multiErrs) > 0 {
		return errors.New(strings.Join(multiErrs, "\n"))
	}

	return nil
}

func (gcwp *GoCraftWorkPool) handleSchedulePolicy(data interface{}) error {
	if data == nil {
		return errors.New("nil data interface")
	}

	pl, ok := data.(*period.PeriodicJobPolicy)
	if !ok {
		return errors.New("malformed policy object")
	}

	return gcwp.scheduler.AcceptPeriodicPolicy(pl)
}

func (gcwp *GoCraftWorkPool) handleUnSchedulePolicy(data interface{}) error {
	if data == nil {
		return errors.New("nil data interface")
	}

	pl, ok := data.(*period.PeriodicJobPolicy)
	if !ok {
		return errors.New("malformed policy object")
	}

	removed := gcwp.scheduler.RemovePeriodicPolicy(pl.PolicyID)
	if removed == nil {
		return errors.New("nothing removed")
	}

	return nil
}

func (gcwp *GoCraftWorkPool) handleRegisterStatusHook(data interface{}) error {
	if data == nil {
		return errors.New("nil data interface")
	}

	// 类型判断
	hook, ok := data.(*opm.HookData)
	if !ok {
		return errors.New("malformed hook object")
	}

	return gcwp.statsManager.RegisterHook(hook.JobID, hook.HookURL, true)
}

func (gcwp *GoCraftWorkPool) handleOPCommandFiring(data interface{}) error {
	if data == nil {
		return errors.New("nil data interface")
	}

	commands, ok := data.([]interface{})
	if !ok || len(commands) != 2 {
		return errors.New("malformed op commands object")
	}
	jobID, ok := commands[0].(string)
	command, ok := commands[1].(string)
	if !ok {
		return errors.New("malformed op command info")
	}

	// Put the command into the maintaining list
	return gcwp.statsManager.SendCommand(jobID, command, true)
}

// log the job
func (rpc *RedisPoolContext) logJob(job *work.Job, next work.NextMiddlewareFunc) error {
	logger.Infof("Job incoming: %s:%s", job.Name, job.ID)
	return next()
}

// Ping the redis server
func (gcwp *GoCraftWorkPool) ping() error {
	// 获取 redis 连接
	conn := gcwp.redisPool.Get()
	defer conn.Close()

	var err error
	for count := 1; count <= pingRedisMaxTimes; count++ {
		if _, err = conn.Do("ping"); err == nil {
			return nil
		}

		time.Sleep(time.Duration(count+4) * time.Second)
	}

	return fmt.Errorf("connect to redis server timeout: %s", err.Error())
}

// generate the job stats data
func generateResult(j *work.Job, jobKind string, isUnique bool) models.JobStats {
	if j == nil {
		return models.JobStats{}
	}

	return models.JobStats{
		Stats: &models.JobStatData{
			JobID:       j.ID,
			JobName:     j.Name,
			JobKind:     jobKind,
			IsUnique:    isUnique,
			Status:      job.JobStatusPending,
			EnqueueTime: j.EnqueuedAt,
			UpdateTime:  time.Now().Unix(),
			// 对应着数据库中 img_scan_job 表中的数据
			RefLink:     fmt.Sprintf("/api/v1/jobs/%s", j.ID),
		},
	}
}
