package gutil

import (
	"fmt"
	"testing"
)

func TestSnakeToPascal(t *testing.T) {
	fmt.Println(SnakeToPascal("workflow"))
}

func TestCamelToSnakeCase(t *testing.T) {
	fmt.Println(CamelToSnakeCase("companyAccount"))
}

func TestFirstLetterToLower(t *testing.T) {
	fmt.Println(FirstLetterToLower("Workflow"))
}

func TestReplaceIdToID(t *testing.T) {
	fmt.Println(ReplaceIdToID(""))
}
