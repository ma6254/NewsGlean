package log

// Sink 输出端抽象：任何输出目标（终端/文件/WebSocket/未来扩展）实现此接口。
type Sink interface {
	// Open 初始化输出端。失败时调用方降级（上报错误并忽略该 sink），而不是崩溃。
	Open() error
	// Write 输出一条记录。实现必须并发安全，且不应长时间阻塞调用方
	// （慢输出端如 WebSocket 应在内部异步化）。
	Write(rec Record)
	// Close 关闭输出端并刷盘。
	Close() error
}

// SinkFunc 把单个函数适配成 Sink，方便测试与轻量扩展。
type SinkFunc func(rec Record)

// Open 无操作。
func (f SinkFunc) Open() error { return nil }

// Write 调用底层函数。
func (f SinkFunc) Write(rec Record) { f(rec) }

// Close 无操作。
func (f SinkFunc) Close() error { return nil }
