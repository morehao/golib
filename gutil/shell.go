package gutil

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ExecShell 通过 `sh -c` 执行一条 shell 命令，返回合并后的 stdout+stderr。
// 命令非零退出时返回 error（err 中携带退出信息，输出仍通过返回的 string 带回）。
//
// 注意：命令字符串会被 shell 解释，切勿拼接未经校验的外部输入，以免命令注入。
func ExecShell(cmdStr string) (string, error) {
	return ExecShellContext(context.Background(), cmdStr)
}

// ExecShellContext 与 ExecShell 相同，但可通过 ctx 控制超时/取消。
// ctx 取消时返回的 error 可由 errors.Is(err, context.Canceled/DeadlineExceeded) 判定。
func ExecShellContext(ctx context.Context, cmdStr string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := strings.TrimRight(buf.String(), "\n")
	if err != nil {
		// cmd.Run 在被 ctx 终止时返回 "signal: killed"，这里归一化为 ctx 的真实原因，
		// 便于调用方用 errors.Is(err, context.DeadlineExceeded/Canceled) 判定。
		if ctxErr := ctx.Err(); ctxErr != nil {
			return out, fmt.Errorf("gutil: exec shell %q: %w", cmdStr, ctxErr)
		}
		return out, fmt.Errorf("gutil: exec shell %q: %w", cmdStr, err)
	}
	return out, nil
}
