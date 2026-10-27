// Package gincontext 是 gin 边界的适配层：读取请求信息、写租户作用域、渲染响应、
// 捕获请求/响应体。
//
// 它只应出现在 controller 和 middleware 中：
//
//   - controller 从 *gin.Context 取出身份/元数据，填进请求 DTO，再把
//     c.Request.Context() 交给 service；
//   - middleware 用 SetTenantScope 写租户作用域（context 里唯一的业务值）。
//
// service / dao / 协议层收 context.Context，身份通过 DTO 字段显式传递，不要
// import 本包。这样 service 单测不需要 gin engine，也不会因为"传 gin ctx 还是
// 纯 ctx"得到不同结果。
package gincontext
