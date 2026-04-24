package gerror

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
)

// ═══════════════════════════════════════════════════════════════
// 基础类型定义
// ═══════════════════════════════════════════════════════════════

// Error 哨兵错误，不可变，仅用于定义和比较
// 通常作为包级变量使用：var ErrNotFound = Error{Code: 404, Msg: "not found"}
type Error struct {
	Code int
	Msg  string
}

// ErrorMap code -> Error 的映射
type ErrorMap map[int]Error

// CodeMsgMap code -> msg 字符串的映射
type CodeMsgMap map[int]string

// ═══════════════════════════════════════════════════════════════
// Error 哨兵方法
// ═══════════════════════════════════════════════════════════════

// Error 实现 error 接口
func (e Error) Error() string {
	return fmt.Sprintf("[%d] %s", e.Code, e.Msg)
}

// Is 仅比较 Code，支持 errors.Is 链式查找
// 使得 errors.Is(wrappedErr, ErrNotFound) 能够正确匹配
func (e Error) Is(target error) bool {
	var t Error
	if errors.As(target, &t) {
		return e.Code == t.Code
	}
	return false
}

// GetCode 获取错误码
func (e Error) GetCode() int { return e.Code }

// GetMsg 获取错误信息
func (e Error) GetMsg() string { return e.Msg }

// WithMsg 返回新副本并替换 Msg，不修改原始哨兵
func (e Error) WithMsg(msg string) Error {
	return Error{Code: e.Code, Msg: msg}
}

// Wrap 包装底层 error，附加调用栈，不修改哨兵自身
func (e Error) Wrap(cause error) error {
	if cause == nil {
		return nil
	}
	return newWrapped(e, e.Msg, cause)
}

// Wrapf 包装底层 error，支持格式化上下文描述
func (e Error) Wrapf(cause error, format string, args ...any) error {
	if cause == nil {
		return nil
	}
	return newWrapped(e, fmt.Sprintf(format, args...), cause)
}

// New 基于哨兵创建独立错误（不包装其他 error），附加调用栈
func (e Error) New(msg string) error {
	return newWrapped(e, msg, nil)
}

// ═══════════════════════════════════════════════════════════════
// wrappedError 包装错误（携带上下文和调用栈）
// ═══════════════════════════════════════════════════════════════

type wrappedError struct {
	sentinel Error     // 原始哨兵，保留 Code 用于比较
	msg      string    // 当次错误的上下文描述
	cause    error     // 被包装的底层错误
	stack    []uintptr // 调用栈 PC 列表
}

// newWrapped 统一构造入口，跳过 3 层内部帧：
// runtime.Callers -> newWrapped -> Wrap/Wrapf/New
func newWrapped(sentinel Error, msg string, cause error) *wrappedError {
	pc := make([]uintptr, 32)
	n := runtime.Callers(3, pc)
	return &wrappedError{
		sentinel: sentinel,
		msg:      msg,
		cause:    cause,
		stack:    pc[:n],
	}
}

// Error 实现 error 接口
func (w *wrappedError) Error() string {
	if w.cause != nil {
		return fmt.Sprintf("%s: %s", w.msg, w.cause.Error())
	}
	return w.msg
}

// Unwrap 支持 errors.Is / errors.As 向下解包
func (w *wrappedError) Unwrap() error { return w.cause }

// Is 委托给哨兵比较，使 errors.Is(w, ErrXxx) 生效
func (w *wrappedError) Is(target error) bool {
	return w.sentinel.Is(target)
}

// As 支持 errors.As(err, &Error{}) 提取哨兵
func (w *wrappedError) As(target interface{}) bool {
	if t, ok := target.(*Error); ok {
		*t = w.sentinel
		return true
	}
	return false
}

// StackTrace 返回格式化的调用栈字符串列表
func (w *wrappedError) StackTrace() []string {
	frames := runtime.CallersFrames(w.stack)
	var result []string
	for {
		f, more := frames.Next()
		result = append(result, fmt.Sprintf("%s:%d\n\t%s", f.File, f.Line, f.Function))
		if !more {
			break
		}
	}
	return result
}

// ═══════════════════════════════════════════════════════════════
// ErrorMap 方法
// ═══════════════════════════════════════════════════════════════

func (m ErrorMap) Get(code int) (Error, bool) {
	e, ok := m[code]
	return e, ok
}

func (m ErrorMap) MustGet(code int) Error {
	e, ok := m[code]
	if !ok {
		panic(fmt.Sprintf("gerror: code %d not registered", code))
	}
	return e
}

// ═══════════════════════════════════════════════════════════════
// CodeMsgMap 方法
// ═══════════════════════════════════════════════════════════════

func (m CodeMsgMap) Get(code int) (string, bool) {
	msg, ok := m[code]
	return msg, ok
}

func (m CodeMsgMap) GetOrDefault(code int, defaultMsg string) string {
	if msg, ok := m[code]; ok {
		return msg
	}
	return defaultMsg
}

// ToErrorMap 转换为 ErrorMap
func (m CodeMsgMap) ToErrorMap() ErrorMap {
	em := make(ErrorMap, len(m))
	for code, msg := range m {
		em[code] = Error{Code: code, Msg: msg}
	}
	return em
}

// ═══════════════════════════════════════════════════════════════
// 全局工具函数
// ═══════════════════════════════════════════════════════════════

// GetCode 从任意 error 中提取业务错误码，找不到返回 -1
func GetCode(err error) int {
	var e Error
	if errors.As(err, &e) {
		return e.Code
	}
	return -1
}

// GetMsg 从任意 error 中提取业务错误信息，找不到则返回 err.Error()
func GetMsg(err error) string {
	if err == nil {
		return ""
	}
	var e Error
	if errors.As(err, &e) {
		return e.Msg
	}
	return err.Error()
}

// IsCode 直接用 code 整数判断，无需构造哨兵
func IsCode(err error, code int) bool {
	return GetCode(err) == code
}

// Cause 获取 error 链最底层的原始错误
func Cause(err error) error {
	for {
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			return err
		}
		err = unwrapped
	}
}

// StackTrace 从任意 error 中提取调用栈；
// 如果该 error 不携带栈信息则返回 nil
func StackTrace(err error) []string {
	var w *wrappedError
	if errors.As(err, &w) {
		return w.StackTrace()
	}
	return nil
}

// FormatError 输出完整的错误信息 + 调用栈，用于日志记录
func FormatError(err error) string {
	if err == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("error: ")
	sb.WriteString(err.Error())

	if stack := StackTrace(err); len(stack) > 0 {
		sb.WriteString("\nstack trace:\n")
		for _, frame := range stack {
			sb.WriteString(frame)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}
