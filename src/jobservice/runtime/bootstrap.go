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

package runtime

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/goharbor/harbor/src/common/job"
	"github.com/goharbor/harbor/src/jobservice/api"
	"github.com/goharbor/harbor/src/jobservice/config"
	"github.com/goharbor/harbor/src/jobservice/core"
	"github.com/goharbor/harbor/src/jobservice/env"
	jsjob "github.com/goharbor/harbor/src/jobservice/job"
	"github.com/goharbor/harbor/src/jobservice/job/impl"
	"github.com/goharbor/harbor/src/jobservice/job/impl/gc"
	"github.com/goharbor/harbor/src/jobservice/job/impl/replication"
	"github.com/goharbor/harbor/src/jobservice/job/impl/scan"
	"github.com/goharbor/harbor/src/jobservice/logger"
	"github.com/goharbor/harbor/src/jobservice/models"
	"github.com/goharbor/harbor/src/jobservice/pool"
	"github.com/goharbor/harbor/src/jobservice/utils"
	"github.com/gomodule/redigo/redis"
)

const (
	dialConnectionTimeout = 30 * time.Second
	healthCheckPeriod     = time.Minute
	dialReadTimeout       = healthCheckPeriod + 10*time.Second
	dialWriteTimeout      = 10 * time.Second
)

// JobService ...
var JobService = &Bootstrap{}

// Bootstrap is coordinating process to help load and start the other components to serve.
type Bootstrap struct {
	jobConextInitializer env.JobContextInitializer
}

// SetJobContextInitializer set the job context initializer
func (bs *Bootstrap) SetJobContextInitializer(initializer env.JobContextInitializer) {
	if initializer != nil {
		bs.jobConextInitializer = initializer
	}
}

// LoadAndRun will load configurations, initialize components and then start the related process to serve requests.
// Return error if meet any problems.
func (bs *Bootstrap) LoadAndRun(ctx context.Context, cancel context.CancelFunc) {
	// 父 context 的建立
	rootContext := &env.Context{
		SystemContext: ctx,
		WG:            &sync.WaitGroup{},
		ErrorChan:     make(chan error, 1), // with 1 buffer
	}

	// Build specified job context
	if bs.jobConextInitializer != nil {
		if jobCtx, err := bs.jobConextInitializer(rootContext); err == nil {
			// 用main.go 中定义的 jobcontext 初始器来对 rootcontext 进行加工，将 jobcontext 与 rootcontext 绑定。
			rootContext.JobContext = jobCtx
		} else {
			logger.Fatalf("Failed to initialize job context: %s\n", err)
		}
	}

	// Start the pool
	var (
		backendPool pool.Interface
		wpErr       error
	)
	// 初始化并启动工作池，当有 job 来是分配 work 来执行。
	if config.DefaultConfig.PoolConfig.Backend == config.JobServicePoolBackendRedis {
		backendPool, wpErr = bs.loadAndRunRedisWorkerPool(rootContext, config.DefaultConfig)
		if wpErr != nil {
			logger.Fatalf("Failed to load and run worker pool: %s\n", wpErr.Error())
		}
	} else {
		logger.Fatalf("Worker pool backend '%s' is not supported", config.DefaultConfig.PoolConfig.Backend)
	}

	// Initialize controller
	ctl := core.NewController(backendPool)
	// Keep the job launch func in the system context
	var launchJobFunc jsjob.LaunchJobFunc = func(req models.JobRequest) (models.JobStats, error) {
		return ctl.LaunchJob(req)
	}
	rootContext.SystemContext = context.WithValue(rootContext.SystemContext, utils.CtlKeyOfLaunchJobFunc, launchJobFunc)

	// Start the API server
	apiServer := bs.loadAndRunAPIServer(rootContext, config.DefaultConfig, ctl)
	logger.Infof("Server is started at %s:%d with %s", "", config.DefaultConfig.Port, config.DefaultConfig.Protocol)

	// To indicate if any errors occurred
	var err error
	// Block here
	// 定义系统信号通道，主要是中断和杀死信号
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, os.Kill)
	select {
	case <-sig:
	case err = <-rootContext.ErrorChan:
	}

	// Call cancel to send termination signal to other interested parts.
	cancel()

	// Gracefully shutdown
	apiServer.Stop()

	// In case stop is called before the server is ready
	close := make(chan bool, 1)
	go func() {
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()

		select {
		case <-timer.C:
			// Try again
			apiServer.Stop()
		case <-close:
			return
		}

	}()

	rootContext.WG.Wait()
	close <- true

	if err != nil {
		logger.Fatalf("Server exit with error: %s\n", err)
	}

	logger.Infof("Server gracefully exit")
}

// Load and run the API server.
func (bs *Bootstrap) loadAndRunAPIServer(ctx *env.Context, cfg *config.Configuration, ctl *core.Controller) *api.Server {
	// Initialized API server
	authProvider := &api.SecretAuthenticator{}
	handler := api.NewDefaultHandler(ctl)
	router := api.NewBaseRouter(handler, authProvider)
	serverConfig := api.ServerConfig{
		Protocol: cfg.Protocol,
		Port:     cfg.Port,
	}
	if cfg.HTTPSConfig != nil {
		serverConfig.Cert = cfg.HTTPSConfig.Cert
		serverConfig.Key = cfg.HTTPSConfig.Key
	}

	server := api.NewServer(ctx, router, serverConfig)
	// Start processes
	server.Start()

	return server
}

// Load and run the worker pool
func (bs *Bootstrap) loadAndRunRedisWorkerPool(ctx *env.Context, cfg *config.Configuration) (pool.Interface, error) {
	redisPool := &redis.Pool{
		MaxActive: 6, // 指定时间内允许的最大连接数
		MaxIdle:   6,
		Wait:      true,
		Dial: func() (redis.Conn, error) { // 连接到 redis
			return redis.DialURL(
				cfg.PoolConfig.RedisPoolCfg.RedisURL,
				redis.DialConnectTimeout(dialConnectionTimeout),
				redis.DialReadTimeout(dialReadTimeout),
				redis.DialWriteTimeout(dialWriteTimeout),
			)
		},
		// 检测空闲连接的健康
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			if time.Since(t) < time.Minute {
				return nil
			}

			_, err := c.Do("PING")
			return err
		},
	}

	redisWorkerPool := pool.NewGoCraftWorkPool(ctx,
		fmt.Sprintf("{%s}", cfg.PoolConfig.RedisPoolCfg.Namespace),
		cfg.PoolConfig.WorkerCount,
		redisPool)
	// Register jobs here
	// 注册 job 信息，在初始的过程中，会执行 DemoJob
	if err := redisWorkerPool.RegisterJob(impl.KnownJobDemo, (*impl.DemoJob)(nil)); err != nil {
		// exit
		return nil, err
	}
	// 注册各种类型的 job，这些工作都会在启动的时候被执行一遍
	if err := redisWorkerPool.RegisterJobs(
		map[string]interface{}{
			job.ImageScanJob:    (*scan.ClairJob)(nil),
			job.ImageScanAllJob: (*scan.All)(nil),
			job.ImageTransfer:   (*replication.Transfer)(nil),
			job.ImageDelete:     (*replication.Deleter)(nil),
			job.ImageReplicate:  (*replication.Replicator)(nil),
			job.ImageGC:         (*gc.GarbageCollector)(nil),
		}); err != nil {
		// exit
		return nil, err
	}

	//	启动redis 工作池，开始处理任务
	if err := redisWorkerPool.Start(); err != nil {
		return nil, err
	}

	return redisWorkerPool, nil
}
