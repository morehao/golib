#!/usr/bin/env bash
# check-gasync-deps.sh — 校验 gasync 执行侧解耦后的依赖边界（设计 V1/V2 断言）。
#
# 约定：
#   - source/db 只依赖 gasync 根包 + gorm；不得 import manager / worker / asynq。
#   - worker    只依赖 gasync 根包；不得 import gorm / manager / source/db / asynq。
#   - manager   单向依赖 source/db（source/db 反向不得依赖 manager → 无循环引用）。
#
# 用法： bash scripts/check-gasync-deps.sh
# 任一断言失败即退出非 0，可直接接进 CI / pre-commit。
set -euo pipefail

cd "$(dirname "$0")/.."

fail=0
check_absent() {
  local pkg="$1" ; shift
  local deps
  deps="$(go list -deps "$pkg" 2>/dev/null)"
  for banned in "$@"; do
    if echo "$deps" | grep -qE "$banned"; then
      echo "✗ [边界违规] $pkg 依赖了禁止的 $banned"
      fail=1
    fi
  done
}

echo "== gasync 依赖边界断言 =="

# V2: source/db 边界
check_absent ./task/gasync/source/db \
  'github.com/morehao/golib/task/gasync/manager$' \
  'github.com/morehao/golib/task/gasync/worker$' \
  'github.com/hibiken/asynq'
echo "  source/db: 不依赖 manager/worker/asynq"

# V2: worker 边界
check_absent ./task/gasync/worker \
  '^gorm\.io/gorm$' \
  'github.com/morehao/golib/task/gasync/manager$' \
  'github.com/morehao/golib/task/gasync/source/db$' \
  'github.com/hibiken/asynq'
echo "  worker: 不依赖 gorm/manager/source-db/asynq"

# V1: 无环 — source/db 不得反向依赖 manager
if go list -deps ./task/gasync/source/db 2>/dev/null | grep -q 'task/gasync/manager'; then
  echo "✗ [循环引用] source/db 反向依赖 manager"
  fail=1
else
  echo "  source/db → manager: 无反向依赖（无环）"
fi

# 正向：manager 应当依赖 source/db（单向）
if go list -deps ./task/gasync/manager 2>/dev/null | grep -q 'task/gasync/source/db'; then
  echo "  manager → source/db: 存在单向依赖（符合预期）"
else
  echo "⚠ manager 未依赖 source/db（可能结构调整，请确认）"
fi

if [ "$fail" -ne 0 ]; then
  echo "== 断言失败 =="
  exit 1
fi
echo "== 全部通过 =="
