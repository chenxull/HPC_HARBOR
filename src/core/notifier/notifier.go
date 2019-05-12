package notifier

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/goharbor/harbor/src/common/utils/log"
)

// HandlerIndexer is setup the relationship between the handler type and
// instance.
type HandlerIndexer map[string]NotificationHandler

// Notification wraps the topic and related data value if existing.
type Notification struct {
	// Topic of notification
	// Required
	Topic string

	// Value of notification.
	// Optional
	Value interface{}
}

// HandlerChannel provides not only the chan itself but also the count of
// handlers related with this chan.
type HandlerChannel struct {
	// To indicate how many handler instances bound with this chan.
	boundCount uint32

	// The chan for controlling concurrent executions.
	channel chan bool
}

// NotificationWatcher is defined to accept the events published
// by the sender and match it with pre-registered notification handler
// and then trigger the execution of the found handler.
// 监控发布的事件，如果此事件与注册在 handlers 中的事件相匹配，触发此事件的 handler。
type NotificationWatcher struct {
	// For handle concurrent scenario.
	*sync.RWMutex

	// To keep the registered handlers in memory.
	// Each topic can register multiple handlers.
	// Each handler can bind to multiple topics.
	// topic 和 handlers 是多对多的关系
	handlers map[string]HandlerIndexer

	// Keep the channels which are used to control the concurrent executions
	// of multiple stateful handlers with same type.
	// 使用此 channel 并发的执行多个相同类型的有状态的 handlers
	handlerChannels map[string]*HandlerChannel
}

// notificationWatcher is a default notification watcher in package level.
var notificationWatcher = NewNotificationWatcher()

// NewNotificationWatcher is constructor of NotificationWatcher.
func NewNotificationWatcher() *NotificationWatcher {
	return &NotificationWatcher{
		new(sync.RWMutex),
		make(map[string]HandlerIndexer),
		make(map[string]*HandlerChannel),
	}
}

// Handle the related topic with the specified handler.
func (nw *NotificationWatcher) Handle(topic string, handler NotificationHandler) error {
	if strings.TrimSpace(topic) == "" {
		return errors.New("Empty topic is not supported")
	}

	if handler == nil {
		return errors.New("Nil handler can not be registered")
	}

	defer nw.Unlock()
	nw.Lock()

	// 通过反射获取 handler 的具体类型, 如果 nw 中没有topic 对应的 handle，对其进行注册
	t := reflect.TypeOf(handler).String()
	if indexer, ok := nw.handlers[topic]; ok {
		if _, existing := indexer[t]; existing {
			return fmt.Errorf("Topic %s has already register the handler with type %s", topic, t)
		}

		indexer[t] = handler
	} else {
		newIndexer := make(HandlerIndexer)
		newIndexer[t] = handler
		nw.handlers[topic] = newIndexer
	}

	if handler.IsStateful() {
		// First time
		if handlerChan, ok := nw.handlerChannels[t]; !ok {
			nw.handlerChannels[t] = &HandlerChannel{1, make(chan bool, 1)}
		} else {
			// Already have chan, just increase count
			handlerChan.boundCount++
		}
	}

	return nil
}

// UnHandle is to revoke the registered handler with the specified topic.
// 'handler' is optional, the type name of the handler. If it's empty value,
// then revoke the whole topic, otherwise only revoke the specified handler.
func (nw *NotificationWatcher) UnHandle(topic string, handler string) error {
	if strings.TrimSpace(topic) == "" {
		return errors.New("Empty topic is not supported")
	}

	defer nw.Unlock()
	nw.Lock()

	var revokeHandler = func(indexer HandlerIndexer, handlerType string) bool {
		// Find the specified one
		if hd, existing := indexer[handlerType]; existing {
			delete(indexer, handlerType)
			if len(indexer) == 0 {
				// No handler existing, then remove topic
				delete(nw.handlers, topic)
			}

			// Update channel counter or remove channel
			if hd.IsStateful() {
				if theChan, yes := nw.handlerChannels[handlerType]; yes {
					theChan.boundCount--
					if theChan.boundCount == 0 {
						// Empty, then remove the channel
						delete(nw.handlerChannels, handlerType)
					}
				}
			}

			return true
		}

		return false
	}

	if indexer, ok := nw.handlers[topic]; ok {
		// 当没有指定 handler 时，删除所有的 handler
		if strings.TrimSpace(handler) == "" {
			for t := range indexer {
				revokeHandler(indexer, t)
			}

			return nil
		}

		// Revoke the specified handler.
		if revokeHandler(indexer, handler) {
			return nil
		}
	}

	return fmt.Errorf("Failed to revoke handler %s with topic %s", handler, topic)
}

// Notify that notification is coming.
func (nw *NotificationWatcher) Notify(notification Notification) error {
	if strings.TrimSpace(notification.Topic) == "" {
		return errors.New("Empty topic can not be notified")
	}

	defer nw.RUnlock()
	nw.RLock()

	var (
		indexer  HandlerIndexer
		ok       bool
		handlers = []NotificationHandler{}
	)
	// 获取 map[topic]handler
	if indexer, ok = nw.handlers[notification.Topic]; !ok {
		return fmt.Errorf("No handlers registered for handling topic %s", notification.Topic)
	}

	// 提取出对应 topic 中的 handlers 出来
	for _, h := range indexer {
		handlers = append(handlers, h)
	}

	// Trigger handlers
	// 遍历启动 goroutine 来执行通知处理程序
	for _, h := range handlers {
		var handlerChan chan bool
		// 如果此 handler 是有状态的，需要为其创建指定类型的 handlerchan，存放通知信息
		if h.IsStateful() {
			t := reflect.TypeOf(h).String()
			handlerChan = nw.handlerChannels[t].channel
		}
		go func(hd NotificationHandler, ch chan bool) {
			if hd.IsStateful() && ch != nil {
				ch <- true
			}
			go func() {
				defer func() {
					if hd.IsStateful() && ch != nil {
						<-ch
					}
				}()
				// 调用指定事件的 handler，处理传入的 value
				if err := hd.Handle(notification.Value); err != nil {
					// Currently, we just log the error
					log.Errorf("Error occurred when triggering handler %s of topic %s: %s\n", reflect.TypeOf(hd).String(), notification.Topic, err.Error())
				} else {
					log.Infof("Handle notification with topic '%s': %#v\n", notification.Topic, notification.Value)
				}
			}()
		}(h, handlerChan)
	}

	return nil
}

// Subscribe is a wrapper utility method for NotificationWatcher.handle()
func Subscribe(topic string, handler NotificationHandler) error {
	return notificationWatcher.Handle(topic, handler)
}

// UnSubscribe is a wrapper utility method for NotificationWatcher.UnHandle()
func UnSubscribe(topic string, handler string) error {
	return notificationWatcher.UnHandle(topic, handler)
}

// Publish is a wrapper utility method for NotificationWatcher.notify()
// 通知
func Publish(topic string, value interface{}) error {
	return notificationWatcher.Notify(Notification{
		Topic: topic,
		Value: value,
	})
}
