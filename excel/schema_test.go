package excel

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildSchema(t *testing.T) {
	t.Run("parse col and alias tags", func(t *testing.T) {
		type row struct {
			Name string `ex:"col:姓名,alias:名字|name"`
			Age  int    `ex:"col:年龄"`
		}

		schema, err := buildSchema(reflect.TypeOf(row{}), nil)
		if err != nil {
			t.Fatalf("buildSchema returned error: %v", err)
		}
		if len(schema) != 2 {
			t.Fatalf("expected 2 schema entries, got %d", len(schema))
		}

		nameFound := false
		ageFound := false
		for _, item := range schema {
			switch item.fieldName {
			case "Name":
				nameFound = true
				if item.column != "姓名" {
					t.Fatalf("expected Name column to be 姓名, got %q", item.column)
				}
				if len(item.aliases) != 2 || item.aliases[0] != "名字" || item.aliases[1] != "name" {
					t.Fatalf("expected Name aliases [名字 name], got %#v", item.aliases)
				}
			case "Age":
				ageFound = true
				if item.column != "年龄" {
					t.Fatalf("expected Age column to be 年龄, got %q", item.column)
				}
			}
		}
		if !nameFound || !ageFound {
			t.Fatalf("expected schema for fields Name and Age, got %#v", schema)
		}
	})

	t.Run("duplicate column conflict returns error", func(t *testing.T) {
		type row struct {
			Name  string `ex:"col:姓名"`
			Alias string `ex:"col:别名,alias:姓名"`
		}

		_, err := buildSchema(reflect.TypeOf(row{}), nil)
		if err == nil {
			t.Fatal("expected duplicate column conflict error, got nil")
		}
		if !strings.Contains(err.Error(), "duplicate column") {
			t.Fatalf("expected duplicate column error, got %v", err)
		}
	})

	t.Run("parseExcelTag supports col and alias only", func(t *testing.T) {
		tagMap, err := parseExcelTag("col:姓名,alias:名字|name")
		if err != nil {
			t.Fatalf("parseExcelTag returned error: %v", err)
		}
		if tagMap["col"] != "姓名" {
			t.Fatalf("expected col=姓名, got %q", tagMap["col"])
		}
		if tagMap["alias"] != "名字|name" {
			t.Fatalf("expected alias=名字|name, got %q", tagMap["alias"])
		}
	})
}
