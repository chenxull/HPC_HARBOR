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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/goharbor/harbor/src/jobservice/logger"
	"github.com/goharbor/harbor/src/jobservice/opm"
	"github.com/goharbor/harbor/src/jobservice/period"

	"github.com/goharbor/harbor/src/jobservice/models"
	"github.com/goharbor/harbor/src/jobservice/utils"
	"github.com/gomodule/redigo/redis"
)

const (
	msgServerRetryTimes = 5
)

// MessageServer implements the sub/pub mechanism via redis to do async message exchanging.
// 订阅/发布模式，实现了消息的异步交换
//这个功能提供两种信息机制， 分别是订阅/发布到频道和订阅/发布到模式
type MessageServer struct {
	context   context.Context
	redisPool *redis.Pool
	namespace string
	callbacks map[string]reflect.Value // no need to sync
}

// NewMessageServer creates a new ptr of MessageServer
func NewMessageServer(ctx context.Context, namespace string, redisPool *redis.Pool) *MessageServer {
	return &MessageServer{
		context:   ctx,
		redisPool: redisPool,
		namespace: namespace,
		callbacks: make(map[string]reflect.Value),
	}
}

// Start to serve
func (ms *MessageServer) Start() error {
	defer func() {
		logger.Info("Message server is stopped")
	}()

	// 获取 redis 连接
	conn := ms.redisPool.Get() // Get one backend connection!
	// 使这个链接带有 subscribers 方法
	psc := redis.PubSubConn{
		Conn: conn,
	}
	defer psc.Close()

	// Subscribe channel。创建订阅通道，用来和在此通道上的 client 进行绑定。创建 period：policies：notifications 的订阅频道
	err := psc.Subscribe(redis.Args{}.AddFlat(utils.KeyPeriodicNotification(ms.namespace))...)
	if err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() {
		for {
			// 接受来自 消息服务器中 pub/sub 通道上的消息
			switch res := psc.Receive().(type) {
			case error:
				done <- fmt.Errorf("error occurred when receiving from pub/sub channel of message server: %s", res.(error).Error())
			//	消息类型
			case redis.Message:
				m := &models.Message{}
				if err := json.Unmarshal(res.Data, m); err != nil {
					// logged
					logger.Warningf("Read invalid message: %s\n", res.Data)
				}
				// 调用之前注册的各种回调处理函数，key 为事件名称，val 是处理函数方法
				if callback, ok := ms.callbacks[m.Event]; !ok {
					// logged
					logger.Warningf("no handler to handle event %s\n", m.Event)
				} else {
					// logged incoming events
					logger.Infof("Receive event '%s' with data(unformatted): %+#v\n", m.Event, m.Data)
					// Try to recover the concrete type
					var converted interface{}
					// 找出具体的事件类型，用 converted 对事件内容进行包装
					switch m.Event {
					case period.EventSchedulePeriodicPolicy,
						period.EventUnSchedulePeriodicPolicy:
						// ignore error, actually error should not be happened because we did not change data
						// after the last unmarshal try.
						policyObject := &period.PeriodicJobPolicy{}
						dt, _ := json.Marshal(m.Data)
						json.Unmarshal(dt, policyObject)
						converted = policyObject
					//	 镜像扫描任务会有一个 hook_url
					case opm.EventRegisterStatusHook:
						// ignore error
						hookObject := &opm.HookData{}
						dt, _ := json.Marshal(m.Data)
						json.Unmarshal(dt, hookObject)
						// 将 jobid 以及 hool_url 存储在 converted 中
						converted = hookObject
					case opm.EventFireCommand:
						// no need to convert []string
						converted = m.Data
					}
					// 使用上述获取的回调处理函数对事件进行处理
					// 可能是调用发送core
					res := callback.Call([]reflect.Value{reflect.ValueOf(converted)})
					e := res[0].Interface()
					if e != nil {
						err := e.(error)
						// logged
						logger.Errorf("Failed to fire callback with error: %s\n", err)
					}
				}
			//	订阅信息,订阅具体的 redis channel 或者 unsubscribe
			case redis.Subscription:
				switch res.Kind {
				case "subscribe":
					logger.Infof("Subscribe redis channel %s\n", res.Channel)
					break
				case "unsubscribe":
					// Unsubscribe all, means main goroutine is exiting
					logger.Infof("Unsubscribe redis channel %s\n", res.Channel)
					done <- nil
					return
				}
			}
		}
	}()

	// 完成上述工作之后，信息服务器启动完毕
	logger.Info("Message server is started")

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	// blocking here。每隔一分钟检测连接情况，用来处理完成消息。
	for err == nil {
		select {
		case <-ticker.C:
			err = psc.Ping("ping!")
		case <-ms.context.Done():
			err = errors.New("context exit")
		case err = <-done:
		}
	}

	// Unsubscribe all
	psc.Unsubscribe()

	return <-done
}

// Subscribe event with specified callback
// 订阅具体事件，当事件发生时调用 指定的回调函数 callback
func (ms *MessageServer) Subscribe(event string, callback interface{}) error {
	if utils.IsEmptyStr(event) {
		return errors.New("empty event is not allowed")
	}

	// 验证回调处理函数的正确性
	handler, err := validateCallbackFunc(callback)
	if err != nil {
		return err
	}

	ms.callbacks[event] = handler
	return nil
}

func validateCallbackFunc(callback interface{}) (reflect.Value, error) {
	if callback == nil {
		return reflect.ValueOf(nil), errors.New("nil callback handler")
	}

	vFn := reflect.ValueOf(callback)
	vFType := vFn.Type()
	if vFType.Kind() != reflect.Func {
		return reflect.ValueOf(nil), errors.New("callback handler must be a generic func")
	}

	inNum := vFType.NumIn()
	outNum := vFType.NumOut()
	if inNum != 1 || outNum != 1 {
		return reflect.ValueOf(nil), errors.New("callback handler can only be func(interface{})error format")
	}

	inType := vFType.In(0)
	var intf *interface{}
	if inType != reflect.TypeOf(intf).Elem() {
		return reflect.ValueOf(nil), errors.New("callback handler can only be func(interface{})error format")
	}

	outType := vFType.Out(0)
	var e *error
	if outType != reflect.TypeOf(e).Elem() {
		return reflect.ValueOf(nil), errors.New("callback handler can only be func(interface{})error format")
	}

	return vFn, nil
}
