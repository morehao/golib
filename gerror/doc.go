// Package gerror 定义全仓库统一的错误模型与错误码/sentinel 分层约定。
//
// # 一、错误模型概览
//
// 本包提供两类基础能力：
//
//  1. 错误码体系：Error / ErrorMap / CodeMsgMap，把「int 业务码」映射到「提示文案」，
//     并提供 GetCode / GetMsg / IsCode 等从任意 error 中提取错误码的工具函数；
//  2. sentinel（哨兵）错误与包装：Error 可作为包级哨兵使用，配合 Wrap / Wrapf / New
//     附加上下文与调用栈，使 errors.Is / errors.As 能够跨层判定。
//
// 真实符号（均在本包 error.go 中定义）：Error、ErrorMap、CodeMsgMap、GetCode、GetMsg、
// IsCode、Cause、StackTrace、FormatError，以及 Error 上的 Is/As/GetCode/GetMsg/WithMsg/
// Wrap/Wrapf/New 方法。
//
// # 二、分层约定（重要）
//
// 错误码/sentinel 必须按「可见范围」分层，不要混用：
//
//	(a) 业务码（面向外部 / 需要映射 HTTP 状态与提示文案）
//	    统一放在 gconstant 包，用 gerror.CodeMsgMap 登记 code -> msg，例如
//	    gconstant.DBErrorMsgMap / SystemErrorMsgMap / AuthErrorMsgMap / LLMErrorMsgMap。
//	    这类错误会直接返回给调用方/前端，需要稳定的数字码与用户可读文案。
//	    → 使用者通过 gerror.GetCode(err) / gerror.GetMsg(err) / gerror.IsCode(err, code)
//	      读取，无需构造哨兵。
//
//	(b) 组件内 sentinel（仅用于包内/包间 errors.Is 判定）
//	    放在各自组件包的 errors.go（如 dict/errors.go、filestore/errors.go、
//	    gsetting/errors.go、storage/errors.go、task/gcron/errors.go、
//	    task/gasync/errors.go），通过 errors.New 定义。
//	    - 文案统一使用 "<pkg>: ..." 前缀，例如 errors.New("dict: record not found")、
//	      errors.New("storage: object not found")，便于日志快速定位来源包；
//	    - 同一包内禁止重复定义同一语义的 sentinel（例如已有 ErrNotFound 就不要再
//	      定义 ErrRecordNotFound 表达同一含义）；
//	    - sentinel 只表达「类别」，具体上下文用 Wrap/Wrapf 补充，不要为每种上下文新建 sentinel。
//
//	(c) 不要用业务码体系去承载组件内部判定逻辑，也不要把组件内 sentinel 塞进
//	    gconstant 的业务码区间——两套体系职责不同。
//
// # 三、errors.Is / errors.As 推荐用法
//
// 推荐配合标准库 errors 包使用：
//
//	// 判定是否属于某类哨兵错误（可穿透 Wrap/Wrapf 的包装链）
//	if errors.Is(err, dict.ErrNotFound) { ... }
//
//	// 提取本包的 Error 以读取 Code/Msg
//	var e gerror.Error
//	if errors.As(err, &e) { use(e.GetCode(), e.GetMsg()) }
//
//	// 仅用整数码判定（等价于 GetCode(err) == code）
//	if gerror.IsCode(err, gconstant.DBFindErr) { ... }
//
// 注意：
//   - gerror.Error 的 Is 方法「仅比较 Code」，因此两个 Code 相同但语义不同的 Error
//     哨兵会被判定为相等；请确保 code 在全局唯一。
//   - 跨包存在同名但不同域的 sentinel（见下节），调用方 errors.Is 时必须带包限定
//     （dict.ErrNotFound 与 storage.ErrNotFound 是不同对象，互不相等）。
//   - errors.New 产生的是指针哨兵，比较的是同一对象；gerror.Error 是值类型哨兵，
//     比较走 Is 方法的 Code 逻辑。两者不要混着比较。
//
// # 四、跨包同名但不同域的 sentinel 清单
//
// 以下 sentinel 名字相同、但分属不同包/不同领域，语义与对象都不同，调用方
// errors.Is 时务必写全包限定，切勿只按名字判断：
//
//   - 未找到类：
//     dict.ErrNotFound    （"dict: record not found"，字典记录未找到）
//     storage.ErrNotFound （"storage: object not found"，对象存储对象未找到）
//     ↑ 同为 ErrNotFound，但一个在 dict 包、一个在 storage 包，互不相等。
//
//   - 任务运行/任务未找到类：
//     task/gcron.ErrRunNotFound / task/gcron.ErrTaskNotFound
//     task/gasync.ErrRunNotFound / task/gasync.ErrTaskNotFound
//     ↑ gcron 与 gasync 各自定义了自己的 ErrRunNotFound / ErrTaskNotFound，
//       即便同名也分属两个子包（gcron.ErrRunNotFound ≠ gasync.ErrRunNotFound），
//       跨子包判定必须带包限定。
//
//   - 其他易混同名（同样需带包限定）：
//     dict.ErrNilHandler 不存在；task/gcron.ErrNilHandler 与 task/gasync.ErrNilHandler
//     同名不同包（分别表示 gcron 任务 handler 为 nil、gasync handler 为 nil）。
//     filestore.ErrInvalidArgument 与 storage.ErrInvalidArgument 同名不同包。
//
// 结论：sentinel 一律「包内定义、包限定使用」。看到 ErrNotFound / ErrRunNotFound /
// ErrTaskNotFound / ErrInvalidArgument / ErrNilHandler 等常见名，先确认调用方期望的是
// 哪个包的对象。
package gerror
