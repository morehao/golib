package gconc

// Stats 描述工作池的实时运行统计信息。
type Stats struct {
	ActiveWorkers  int32
	PendingTasks   int32
	CompletedTasks int64
	FailedTasks    int64
}
