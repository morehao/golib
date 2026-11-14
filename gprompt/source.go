package gprompt

import "context"

// Source 是 Prompt 数据的读取抽象，与 dict.Source 同构：
// 读路径只依赖这个接口，DB（gorm）/ 内存两种实现可互换；
// 未来可在其上叠加缓存装饰器（二期），不影响 PromptService。
//
// 约定：读方法在「不存在」时返回 (nil, nil) 之外，也允许返回 sentinel error
// （ErrPromptNotFound / ErrPromptDisabled / ErrVersionNotFound），由具体实现决定；
// PromptService 两种都会正确区分。
type Source interface {
	// GetPromptByCode 按 code 点查启用中的 Prompt 及其当前版本内容。
	// Prompt 不存在返回 ErrPromptNotFound；停用返回 ErrPromptDisabled。
	GetPromptByCode(ctx context.Context, code string) (*PromptSnapshot, error)
	// GetVersion 按 code + version 点查指定版本快照。
	// Prompt 或版本不存在分别返回 ErrPromptNotFound / ErrVersionNotFound。
	GetVersion(ctx context.Context, code string, version int) (*PromptSnapshot, error)
}
