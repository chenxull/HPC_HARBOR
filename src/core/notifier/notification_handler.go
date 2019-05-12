package notifier

// NotificationHandler defines what operations a notification handler
// should have.
type NotificationHandler interface {
	// Handle the event when it coming.
	// value might be optional, it depends on usages.
	Handle(value interface{}) error

	// IsStateful returns whether the handler is stateful or not.
	// If handler is stateful, it will not be triggerred in parallel.
	// Otherwise, the handler will be triggered concurrently if more
	// than one same handler are matched the topics.
	// 检测 handler 是否为有状态的，如果是。这些 handler 不会并行的去执行。
	IsStateful() bool
}
