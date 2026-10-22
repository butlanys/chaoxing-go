// Package notify 外部通知服务，对应 Python 原版 /root/chaoxing-ref/api/notification.py。
package notify

// Notifier 通知服务接口
type Notifier interface {
	Name() string
	Send(msg string) error
}

// logger 最小日志接口
type logger interface {
	Info(format string, a ...any)
	Warn(format string, a ...any)
	Error(format string, a ...any)
	Debug(format string, a ...any)
}

// New（依据 [notification] 配置构造通知服务；未配置时返回 Noop）的实现见 notify.go：
//
//	func New(conf map[string]string, log logger) Notifier
