package codegen

import (
	"database/sql"
	"strings"
	"testing"
)

// TestBuildColumnType 钉住 PostgreSQL 列类型的拼装规则：
// 只有真正接受 typmod 的类型才拼长度/精度。
//
// 回归背景：整数与浮点在 information_schema 里同样带 numeric_precision
// （bigint=64、integer=32、double precision=53），早先一律拼成 int8(64) / float8(53)，
// 而 PostgreSQL 不允许这些类型带 typmod，生成物拿去建表会直接报
// `type modifier is not allowed for type "int8"`（SQLSTATE 42601）。
func TestBuildColumnType(t *testing.T) {
	impl := &postgresqlImpl{}
	valid := func(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: true} }

	cases := []struct {
		name string
		col  postgresqlTableColumn
		want string
	}{
		// 整数：有 numeric_precision，但必须保持裸类型
		{"bigint 不带精度", postgresqlTableColumn{UdtName: "int8", NumericPrecision: valid(64)}, "int8"},
		{"integer 不带精度", postgresqlTableColumn{UdtName: "int4", NumericPrecision: valid(32)}, "int4"},
		{"smallint 不带精度", postgresqlTableColumn{UdtName: "int2", NumericPrecision: valid(16)}, "int2"},

		// 浮点：同样必须保持裸类型
		{"double precision 不带精度", postgresqlTableColumn{UdtName: "float8", NumericPrecision: valid(53)}, "float8"},
		{"real 不带精度", postgresqlTableColumn{UdtName: "float4", NumericPrecision: valid(24)}, "float4"},

		// numeric/decimal：精度与小数位都要保留
		{"numeric 带精度与小数位", postgresqlTableColumn{UdtName: "numeric", NumericPrecision: valid(10), NumericScale: valid(2)}, "numeric(10,2)"},
		{"numeric 小数位为 0", postgresqlTableColumn{UdtName: "numeric", NumericPrecision: valid(10), NumericScale: valid(0)}, "numeric(10)"},
		{"numeric 无 typmod", postgresqlTableColumn{UdtName: "numeric"}, "numeric"},

		// 字符与位串：长度要保留
		{"varchar 带长度", postgresqlTableColumn{UdtName: "varchar", CharacterMaximumLength: valid(32)}, "varchar(32)"},
		{"bpchar 带长度", postgresqlTableColumn{UdtName: "bpchar", CharacterMaximumLength: valid(10)}, "bpchar(10)"},
		{"bit 带长度", postgresqlTableColumn{UdtName: "bit", CharacterMaximumLength: valid(8)}, "bit(8)"},

		// 其余类型保持裸类型
		{"text", postgresqlTableColumn{UdtName: "text"}, "text"},
		{"timestamptz 不拼 datetime_precision", postgresqlTableColumn{UdtName: "timestamptz", DatetimePrecision: valid(6)}, "timestamptz"},
		{"bool", postgresqlTableColumn{UdtName: "bool"}, "bool"},
		{"jsonb", postgresqlTableColumn{UdtName: "jsonb"}, "jsonb"},
		{"uuid", postgresqlTableColumn{UdtName: "uuid"}, "uuid"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := impl.buildColumnType(c.col)
			if got != c.want {
				t.Fatalf("buildColumnType(udt=%s, precision=%v, scale=%v, maxlen=%v) = %q, want %q",
					c.col.UdtName, c.col.NumericPrecision, c.col.NumericScale, c.col.CharacterMaximumLength, got, c.want)
			}
			// 一律不允许再出现整数/浮点带括号的形态
			for _, bad := range []string{"int2(", "int4(", "int8(", "float4(", "float8("} {
				if strings.Contains(got, bad) {
					t.Fatalf("buildColumnType() = %q 含非法 typmod %q", got, bad)
				}
			}
		})
	}
}

// bogusTypmods 是 PostgreSQL 里绝不该出现在列类型中的形态。
var bogusTypmods = []string{"int2(", "int4(", "int8(", "float4(", "float8("}

// assertNoBogusTypmod 用真实数据库的内省结果做回归守卫：
// 整数/浮点列不得被拼上 typmod，否则生成物建表会报 SQLSTATE 42601。
func assertNoBogusTypmod(t *testing.T, res *ModuleTplAnalysisRes) {
	t.Helper()
	if res == nil {
		t.Fatal("assertNoBogusTypmod: nil analysis result")
	}
	checked := 0
	for _, item := range res.TplAnalysisList {
		for _, field := range item.ModelFields {
			checked++
			for _, bad := range bogusTypmods {
				if strings.Contains(field.ColumnType, bad) {
					t.Errorf("列 %s 的 ColumnType = %q 含非法 typmod %q，建表会报 SQLSTATE 42601",
						field.ColumnName, field.ColumnType, bad)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("assertNoBogusTypmod: 分析结果里没有 ModelFields，校验未生效")
	}
}
