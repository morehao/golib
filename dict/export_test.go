package dict

// 测试专用：把全局单例恢复为未初始化状态，使单例相关用例与执行顺序无关。
func resetDefaultDict() {
	initMu.Lock()
	defer initMu.Unlock()
	defaultDict = nil
	initialized = false
}
