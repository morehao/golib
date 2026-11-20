package gormplugin_test

import (
	"context"
	"testing"

	"github.com/morehao/golib/biz/gcontext"
	"github.com/morehao/golib/dbaccess/gormplugin"
)

func TestWithApp_RoundTrip(t *testing.T) {
	ctx := gormplugin.WithApp(context.Background(), "appA")
	app, ok := gormplugin.AppFrom(ctx)
	if !ok || app != "appA" {
		t.Fatalf("AppFrom = %q,%v; want appA,true", app, ok)
	}
	v, inject, declared := gormplugin.AppScopeFilter(ctx)
	if v != "appA" || !inject || !declared {
		t.Fatalf("AppScopeFilter = %v,%v,%v; want appA,true,true", v, inject, declared)
	}
}

func TestAppFrom_UnsetIsFalse(t *testing.T) {
	if _, ok := gormplugin.AppFrom(context.Background()); ok {
		t.Fatal("AppFrom on empty ctx should be false")
	}
	if _, _, declared := gormplugin.AppScopeFilter(context.Background()); declared {
		t.Fatal("AppScopeFilter on empty ctx declared should be false")
	}
}

func TestAppFrom_AllScopeHasNoValue(t *testing.T) {
	ctx := gcontext.WithTenantScope(context.Background(), gcontext.AllScope())
	if _, ok := gormplugin.AppFrom(ctx); ok {
		t.Fatal("AllScope should have no single app value")
	}
	if _, inject, declared := gormplugin.AppScopeFilter(ctx); inject || !declared {
		t.Fatalf("AllScope filter inject=%v declared=%v; want false,true", inject, declared)
	}
}
