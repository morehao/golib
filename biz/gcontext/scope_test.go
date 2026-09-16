package gcontext

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTenantScopeRoundTrip(t *testing.T) {
	ctx := WithTenantScope(context.Background(), CurrentScope("t-1"))

	scope, ok := TenantScopeFrom(ctx)
	require.True(t, ok)
	require.Equal(t, TenantScopeCurrent, scope.Kind)
	require.Equal(t, "t-1", scope.TenantID)
}

func TestTenantScopeFromUndeclared(t *testing.T) {
	_, ok := TenantScopeFrom(context.Background())
	require.False(t, ok)

	_, ok = TenantScopeFrom(nil)
	require.False(t, ok)

	// Current/Explicit 缺 TenantID 视为未声明，避免"空作用域"被当成合法值放行。
	_, ok = TenantScopeFrom(WithTenantScope(context.Background(), TenantScope{Kind: TenantScopeCurrent}))
	require.False(t, ok)
	_, ok = TenantScopeFrom(WithTenantScope(context.Background(), ExplicitScope("")))
	require.False(t, ok)
}

func TestTenantScopeAllIsDeclaredWithoutTenant(t *testing.T) {
	scope, ok := TenantScopeFrom(WithTenantScope(context.Background(), AllScope()))
	require.True(t, ok)
	require.Equal(t, TenantScopeAll, scope.Kind)
	require.Empty(t, scope.TenantID)
}

func TestTenantScopeFilter(t *testing.T) {
	cases := []struct {
		name         string
		ctx          context.Context
		wantValue    any
		wantInject   bool
		wantDeclared bool
	}{
		{"unset", context.Background(), nil, false, false},
		{"current", WithTenantScope(context.Background(), CurrentScope("t-1")), "t-1", true, true},
		{"explicit", WithTenantScope(context.Background(), ExplicitScope("t-2")), "t-2", true, true},
		{"all", WithTenantScope(context.Background(), AllScope()), nil, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value, inject, declared := TenantScopeFilter(tc.ctx)
			require.Equal(t, tc.wantValue, value)
			require.Equal(t, tc.wantInject, inject)
			require.Equal(t, tc.wantDeclared, declared)
		})
	}
}

func TestTenantScopeKindString(t *testing.T) {
	require.Equal(t, "unset", TenantScopeUnset.String())
	require.Equal(t, "current", TenantScopeCurrent.String())
	require.Equal(t, "explicit", TenantScopeExplicit.String())
	require.Equal(t, "all", TenantScopeAll.String())
	require.Equal(t, "unset", TenantScopeKind(99).String())
}
