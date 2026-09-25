package javascript

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/require"
	"github.com/komari-monitor/komari/database/models"
	"github.com/komari-monitor/komari/utils/messageSender/factory"
)

type JavaScriptSender struct {
	Addition
	mu          sync.Mutex
	ctx         context.Context
	timers      []scriptTimer
	operations  int
	vm          *goja.Runtime
	noopProgram *goja.Program
}

func (j *JavaScriptSender) GetName() string {
	return "Javascript"
}

func (j *JavaScriptSender) GetConfiguration() factory.Configuration {
	return &j.Addition
}

func (j *JavaScriptSender) Init() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.initRuntime()
}
func (j *JavaScriptSender) initRuntime() error {
	if j.Addition.Script == "" {
		return errors.New("JavaScript script is empty")
	}

	// 创建 JavaScript 运行时
	j.vm = goja.New()

	// 预编译一个 no-op 程序,用于驱动微任务队列
	prog, errc := goja.Compile("noop.js", "void 0", false)
	if errc == nil {
		j.noopProgram = prog
	}

	// 设置 require 支持
	new(require.Registry).Enable(j.vm)

	// 注入全局对象和函数
	j.setupGlobals()

	// Script loading has the same execution deadline as sending.
	end := j.beginExecution(30 * time.Second)
	defer end()
	_, err := j.vm.RunString(j.Addition.Script)
	if err != nil {
		return fmt.Errorf("failed to load JavaScript script: %v", err)
	}

	// 验证 sendMessage 函数是否存在
	sendMessage := j.vm.Get("sendMessage")
	if sendMessage == nil || goja.IsUndefined(sendMessage) {
		return errors.New("sendMessage function not defined in script")
	}

	// 验证是否可调用
	if _, ok := goja.AssertFunction(sendMessage); !ok {
		return errors.New("sendMessage is not a function")
	}

	// sendEvent 函数是可选的,不强制要求存在

	return nil
}

func (j *JavaScriptSender) Destroy() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.vm = nil
	j.timers = nil
	return nil
}

func (j *JavaScriptSender) SendTextMessage(message, title string) error {
	return j.execute("sendMessage", message, title)
}

func (j *JavaScriptSender) SendEvent(event models.EventMessage) error {
	return j.executeEvent(event)
}

func (j *JavaScriptSender) executeEvent(event models.EventMessage) error {
	// A missing sendEvent is handled inside the serialized execution.
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	var value map[string]interface{}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	err = j.execute("sendEvent", value)
	if errors.Is(err, errMissingHandler) {
		return j.fallbackToTextMessage(event)
	}
	return err
}

var errMissingHandler = errors.New("JavaScript handler is not defined")

type scriptTimer struct {
	at   time.Time
	call goja.Callable
}

func (j *JavaScriptSender) beginExecution(timeout time.Duration) func() {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	j.ctx = ctx
	j.timers = nil
	j.operations = 0
	j.vm.ClearInterrupt()
	vm := j.vm
	interrupted := make(chan struct{})
	timer := time.AfterFunc(timeout, func() { vm.Interrupt("JavaScript execution timeout"); close(interrupted) })
	return func() {
		cancel()
		if !timer.Stop() {
			<-interrupted
		}
		vm.ClearInterrupt()
		j.timers = nil
	}
}

func (j *JavaScriptSender) execute(name string, args ...interface{}) (err error) {
	if !j.mu.TryLock() {
		return errors.New("JavaScript sender is busy")
	}
	defer j.mu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("JavaScript panic: %v", r)
		}
	}()
	if j.vm == nil {
		if err := j.initRuntime(); err != nil {
			return err
		}
	}
	fn, ok := goja.AssertFunction(j.vm.Get(name))
	if !ok {
		return errMissingHandler
	}
	end := j.beginExecution(30 * time.Second)
	defer end()
	values := make([]goja.Value, len(args))
	for i, arg := range args {
		values[i] = j.vm.ToValue(arg)
	}
	result, err := fn(goja.Undefined(), values...)
	if err != nil {
		return err
	}
	if promise, ok := result.Export().(*goja.Promise); ok {
		for promise.State() == goja.PromiseStatePending || len(j.timers) > 0 {
			if j.ctx.Err() != nil {
				return errors.New("JavaScript execution timeout")
			}
			now := time.Now()
			pending := j.timers
			j.timers = nil
			for _, timer := range pending {
				if !timer.at.After(now) {
					if _, err := timer.call(goja.Undefined()); err != nil {
						return err
					}
				} else {
					j.timers = append(j.timers, timer)
				}
			}
			j.runMicrotasks()
			if promise.State() == goja.PromiseStatePending {
				select {
				case <-j.ctx.Done():
					return errors.New("JavaScript execution timeout")
				case <-time.After(5 * time.Millisecond):
				}
			}
		}
		if promise.State() == goja.PromiseStateRejected {
			return fmt.Errorf("Promise rejected: %v", promise.Result())
		}
		result = promise.Result()
	}
	if !result.ToBoolean() {
		return errors.New(name + " returned false")
	}
	return nil
}

// fallbackToTextMessage 当没有定义 sendEvent 时,回退到使用文本消息格式
func (j *JavaScriptSender) fallbackToTextMessage(event models.EventMessage) error {
	// 构建简单的文本消息
	message := fmt.Sprintf("%s%s%s\nEvent: %s\nMessage: %s\nTime: %s",
		event.Emoji, event.Emoji, event.Emoji,
		event.Event,
		event.Message,
		event.Time.Format(time.RFC3339))

	// 添加客户端信息
	if len(event.Clients) > 0 {
		clientNames := make([]string, 0, len(event.Clients))
		for _, c := range event.Clients {
			name := c.Name
			if name == "" {
				name = c.UUID
			}
			clientNames = append(clientNames, name)
		}
		message = fmt.Sprintf("%s%s%s\nEvent: %s\nClients: %s\nMessage: %s\nTime: %s",
			event.Emoji, event.Emoji, event.Emoji,
			event.Event,
			clientNames,
			event.Message,
			event.Time.Format(time.RFC3339))
	}

	return j.SendTextMessage(message, event.Event)
}

// runMicrotasks 安全地推动 goja 的微任务队列(例如 Promise 回调)
func (j *JavaScriptSender) runMicrotasks() {
	if j.vm == nil {
		return
	}
	if j.noopProgram != nil {
		_, _ = j.vm.RunProgram(j.noopProgram)
		return
	}
	// 兜底: 直接运行一段 no-op 代码
	_, _ = j.vm.RunString("void 0")
}

func (j *JavaScriptSender) setupGlobals() {
	// 注入 console.log
	console := j.vm.NewObject()
	console.Set("log", func(call goja.FunctionCall) goja.Value {
		var args []interface{}
		for _, arg := range call.Arguments {
			args = append(args, arg.Export())
		}
		fmt.Println(args...)
		return goja.Undefined()
	})
	console.Set("error", func(call goja.FunctionCall) goja.Value {
		fmt.Print("Error: ")
		for i, arg := range call.Arguments {
			if i > 0 {
				fmt.Print(" ")
			}
			fmt.Print(arg.Export())
		}
		fmt.Println()
		return goja.Undefined()
	})
	j.vm.Set("console", console)

	// 注入 fetch API
	j.vm.Set("fetch", j.createFetchFunction())

	// 注入 XMLHttpRequest (xhr)
	j.vm.Set("XMLHttpRequest", j.createXHRConstructor())

	// 注入 setTimeout
	j.vm.Set("setTimeout", func(call goja.FunctionCall) goja.Value {
		callback := call.Argument(0)
		delay := call.Argument(1).ToInteger()

		if delay < 0 {
			delay = 0
		}
		if delay > 30000 || len(j.timers) >= 128 || j.operations >= 256 {
			panic(j.vm.NewTypeError("timer quota exceeded"))
		}
		j.operations++
		if fn, ok := goja.AssertFunction(callback); ok {
			j.timers = append(j.timers, scriptTimer{time.Now().Add(time.Duration(delay) * time.Millisecond), fn})
		}

		return goja.Undefined()
	})

	// 注入 Promise 构造函数
	j.vm.RunString(`
		if (typeof Promise === 'undefined') {
			// Promise polyfill 会由 goja 自动提供
		}
	`)
}

func init() {
	factory.RegisterMessageSender(func() factory.IMessageSender {
		return &JavaScriptSender{}
	})
}

// 确保实现了 IMessageSender 接口
var _ factory.IMessageSender = (*JavaScriptSender)(nil)
