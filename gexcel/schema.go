package gexcel

import (
	"fmt"
	"reflect"
	"strings"
)

// ColumnRule describes an explicit field->column mapping, overriding tags.
type ColumnRule struct {
	Field  string
	Column string
	Alias  []string
}

type columnSchema struct {
	fieldName  string
	fieldIndex int
	column     string
	aliases    []string
}

// buildSchemaFromType builds the column schema for a (possibly generic) type.
func buildSchemaFromType[T any]() ([]columnSchema, error) {
	var zero T
	return buildSchema(reflect.TypeOf(zero), nil)
}

// buildSchema builds the column schema from struct tags, then applies explicit
// rules (if any) and validates for conflicts. It is shared by reader and writer.
func buildSchema(typ reflect.Type, explicit []ColumnRule) ([]columnSchema, error) {
	for typ != nil && typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ == nil || typ.Kind() != reflect.Struct {
		return nil, ErrSchemaNotStruct
	}

	schema := make([]columnSchema, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" { // unexported
			continue
		}
		tagValue := field.Tag.Get("excel")
		if strings.TrimSpace(tagValue) == "" {
			continue
		}
		tagMap, err := parseExcelTag(tagValue)
		if err != nil {
			return nil, fmt.Errorf("%w: field %s: %v", ErrInvalidTag, field.Name, err)
		}
		column := strings.TrimSpace(tagMap["col"])
		if column == "" {
			continue
		}
		schema = append(schema, columnSchema{
			fieldName:  field.Name,
			fieldIndex: i,
			column:     column,
			aliases:    splitAliases(tagMap["alias"]),
		})
	}

	return buildSchemaFromRules(typ, schema, explicit)
}

func buildSchemaFromRules(typ reflect.Type, current []columnSchema, explicit []ColumnRule) ([]columnSchema, error) {
	if len(explicit) == 0 {
		return current, validateSchemaConflicts(current)
	}

	for typ != nil && typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ == nil || typ.Kind() != reflect.Struct {
		return nil, ErrSchemaNotStruct
	}

	idxByField := make(map[string]int, len(current))
	for i := range current {
		idxByField[current[i].fieldName] = i
	}

	for _, rule := range explicit {
		fieldName := strings.TrimSpace(rule.Field)
		column := strings.TrimSpace(rule.Column)
		if fieldName == "" {
			return nil, fmt.Errorf("%w: column rule field is required", ErrInvalidTag)
		}
		if column == "" {
			return nil, fmt.Errorf("%w: column rule column is required for field %s", ErrInvalidTag, fieldName)
		}
		field, ok := typ.FieldByName(fieldName)
		if !ok {
			return nil, fmt.Errorf("%w: field %s not found", ErrInvalidTag, fieldName)
		}
		if field.PkgPath != "" {
			return nil, fmt.Errorf("%w: field %s is not exported", ErrInvalidTag, fieldName)
		}

		aliases := append([]string(nil), rule.Alias...)
		for i := range aliases {
			aliases[i] = strings.TrimSpace(aliases[i])
		}

		if idx, exists := idxByField[fieldName]; exists {
			current[idx].column = column
			current[idx].aliases = aliases
			continue
		}
		current = append(current, columnSchema{
			fieldName:  fieldName,
			fieldIndex: field.Index[0],
			column:     column,
			aliases:    aliases,
		})
		idxByField[fieldName] = len(current) - 1
	}

	if err := validateSchemaConflicts(current); err != nil {
		return nil, err
	}
	return current, nil
}

// orderColumns reorders schema so that fields listed in `fields` come first, in
// that order; remaining fields keep their original order. Unknown names are
// ignored. Used by the writer to honour explicit column order.
func orderColumns(base []columnSchema, fields []string) []columnSchema {
	if len(fields) == 0 {
		return base
	}
	idxByField := make(map[string]int, len(base))
	for i := range base {
		idxByField[base[i].fieldName] = i
	}

	ordered := make([]columnSchema, 0, len(base))
	used := make(map[string]struct{}, len(base))
	for _, field := range fields {
		name := strings.TrimSpace(field)
		if name == "" {
			continue
		}
		idx, ok := idxByField[name]
		if !ok {
			continue
		}
		if _, exists := used[name]; exists {
			continue
		}
		ordered = append(ordered, base[idx])
		used[name] = struct{}{}
	}
	for i := range base {
		if _, exists := used[base[i].fieldName]; exists {
			continue
		}
		ordered = append(ordered, base[i])
	}
	return ordered
}

func parseExcelTag(tag string) (map[string]string, error) {
	result := make(map[string]string)
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return result, nil
	}
	for _, raw := range strings.Split(tag, ",") {
		part := strings.TrimSpace(raw)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("invalid excel tag segment %q", part)
		}
		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])
		if key == "" {
			return nil, fmt.Errorf("invalid excel tag segment %q", part)
		}
		switch key {
		case "col", "alias":
		default:
			return nil, fmt.Errorf("unsupported excel tag key %q", key)
		}
		if _, exists := result[key]; exists {
			return nil, fmt.Errorf("duplicate excel tag key %q", key)
		}
		result[key] = value
	}
	return result, nil
}

func splitAliases(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, "|")
	aliases := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		alias := strings.TrimSpace(p)
		if alias == "" {
			continue
		}
		if _, ok := seen[alias]; ok {
			continue
		}
		seen[alias] = struct{}{}
		aliases = append(aliases, alias)
	}
	if len(aliases) == 0 {
		return nil
	}
	return aliases
}

func validateSchemaConflicts(schema []columnSchema) error {
	owner := make(map[string]string, len(schema))
	for _, item := range schema {
		names := make([]string, 0, len(item.aliases)+1)
		names = append(names, strings.TrimSpace(item.column))
		names = append(names, item.aliases...)
		for _, rawName := range names {
			name := strings.TrimSpace(rawName)
			if name == "" {
				continue
			}
			if prev, exists := owner[name]; exists && prev != item.fieldName {
				return fmt.Errorf("duplicate column %q between fields %s and %s", name, prev, item.fieldName)
			}
			owner[name] = item.fieldName
		}
	}
	return nil
}
