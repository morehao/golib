package gast

import (
	"bufio"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

func HasPackageKeywords(file string) (bool, error) {
	fileSet := token.NewFileSet()
	node, parseErr := parser.ParseFile(fileSet, file, nil, parser.ParseComments)
	if parseErr != nil {
		return false, parseErr
	}
	return node.Name != nil, nil
}

func HasImportKeywords(file string) (bool, error) {
	fileSet := token.NewFileSet()
	node, parseErr := parser.ParseFile(fileSet, file, nil, parser.ParseComments)
	if parseErr != nil {
		return false, parseErr
	}
	for _, decl := range node.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if ok && genDecl.Tok == token.IMPORT {
			return true, nil
		}
	}
	return false, nil
}

// TrimFileTitle 去除文件中的 package 和 import 声明，返回剩余内容
func TrimFileTitle(file string) (string, error) {
	// 解析文件以获取 AST
	fileSet := token.NewFileSet()
	node, parseErr := parser.ParseFile(fileSet, file, nil, parser.ParseComments)
	if parseErr != nil {
		return "", parseErr
	}

	// 获取文件内容
	fileHandle, readErr := os.Open(file)
	if readErr != nil {
		return "", readErr
	}
	defer fileHandle.Close()

	// 找到 package 和 import 语句的截止行号
	titleEndLine := fileSet.Position(node.Package).Line // 获取 package 声明的行号
	for _, decl := range node.Decls {
		genDecl, isGenDecl := decl.(*ast.GenDecl)
		if isGenDecl && genDecl.Tok == token.IMPORT {
			pos := fileSet.Position(genDecl.End())
			if pos.Line > titleEndLine {
				titleEndLine = pos.Line
			}
		}
	}

	// 使用 bufio.Scanner 读取文件内容并跳过标题部分
	scanner := bufio.NewScanner(fileHandle)
	var trimmedContent strings.Builder
	currentLine := 1

	// 跳过标题部分，包括空行和缩进
	for scanner.Scan() {
		if currentLine > titleEndLine && strings.TrimSpace(scanner.Text()) != "" {
			trimmedContent.WriteString(scanner.Text() + "\n")
		}
		currentLine++
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	// 返回修改后的文件内容
	return trimmedContent.String(), nil
}

// FindFunction 在指定的文件中查找函数，注意，这里只查找非方法函数
func FindFunction(file string, functionName string) (*ast.FuncDecl, bool, error) {
	// 解析文件以获取AST
	fileSet := token.NewFileSet()
	node, parseErr := parser.ParseFile(fileSet, file, nil, parser.ParseComments)
	if parseErr != nil {
		return nil, false, parseErr
	}
	for _, decl := range node.Decls {
		if funcDecl, ok := decl.(*ast.FuncDecl); ok {
			if funcDecl.Recv == nil {
				if funcDecl.Name.Name == functionName {
					return funcDecl, true, nil
				}
			}
		}
	}
	return nil, false, nil
}

// FindMethod 在指定的文件中查找方法, 注意，这里只查找指定接收者类型的方法
func FindMethod(file string, receiverTypeName, methodName string) (*ast.FuncDecl, bool, error) {
	// 解析文件以获取AST
	fileSet := token.NewFileSet()
	node, parseErr := parser.ParseFile(fileSet, file, nil, parser.ParseComments)
	if parseErr != nil {
		return nil, false, parseErr
	}
	for _, decl := range node.Decls {
		if funcDecl, ok := decl.(*ast.FuncDecl); ok {
			if funcDecl.Recv != nil {
				for _, recv := range funcDecl.Recv.List {
					if recvType, ok := recv.Type.(*ast.StarExpr); ok {
						if ident, ok := recvType.X.(*ast.Ident); ok && ident.Name == receiverTypeName {
							if funcDecl.Name.Name == methodName {
								return funcDecl, true, nil
							}
						}
					} else if ident, ok := recv.Type.(*ast.Ident); ok {
						if ident.Name == receiverTypeName {
							if funcDecl.Name.Name == methodName {
								return funcDecl, true, nil
							}
						}
					}
				}
			}
		}
	}
	return nil, false, nil
}

// interfaceContainsMethod 检查指定接口是否已经包含了指定的方法。
func interfaceContainsMethod(filePath, interfaceName, methodName string) (bool, error) {
	// 解析文件以获取 AST。
	fileSet := token.NewFileSet()
	node, err := parser.ParseFile(fileSet, filePath, nil, 0)
	if err != nil {
		return false, err
	}

	// 遍历 AST 查找指定的接口。
	var contains bool
	ast.Inspect(node, func(n ast.Node) bool {
		// 查找接口类型声明。
		typeSpec, ok := n.(*ast.TypeSpec)
		if !ok {
			return true // 继续遍历 AST。
		}
		if typeSpec.Name.Name == interfaceName {
			interfaceType, ok := typeSpec.Type.(*ast.InterfaceType)
			if !ok {
				return true
			}
			// 遍历接口中的方法，检查是否包含指定的方法。
			for _, method := range interfaceType.Methods.List {
				if len(method.Names) > 0 && method.Names[0].Name == methodName {
					contains = true
					return false // 停止遍历 AST。
				}
			}
		}
		return true
	})

	return contains, nil
}

// getMethodDeclaration 获取指定方法声明
func getMethodDeclaration(filePath, receiverType, methodName string) (string, error) {
	// 解析文件以获取 AST。
	fileSet := token.NewFileSet()
	node, err := parser.ParseFile(fileSet, filePath, nil, 0)
	if err != nil {
		return "", err
	}

	// 遍历 AST 查找指定的方法。
	var methodDecl string
	ast.Inspect(node, func(n ast.Node) bool {
		// 查找函数声明。
		funcDecl, ok := n.(*ast.FuncDecl)
		if !ok {
			return true // 继续遍历 AST。
		}

		// 检查是否为指定的接收者和方法。
		if funcDecl.Recv != nil && len(funcDecl.Recv.List) > 0 {
			recvType, ok := funcDecl.Recv.List[0].Type.(*ast.StarExpr)
			if !ok {
				return true
			}
			ident, ok := recvType.X.(*ast.Ident)
			if !ok || ident.Name != receiverType {
				return true
			}
			if funcDecl.Name.Name == methodName {
				// 找到方法，构建声明字符串。
				methodDecl = methodName + fieldListToString(funcDecl.Type.Params, false) + " " + fieldListToString(funcDecl.Type.Results, true)
				return false // 停止遍历 AST。
			}
		}
		return true
	})

	if methodDecl == "" {
		return "", fmt.Errorf("未找到接收者类型 '%s' 和方法名 '%s' 的方法声明", receiverType, methodName)
	}

	return methodDecl, nil
}

// fieldListToString 将 *ast.FieldList 转换为字符串表示，用于参数和返回值。
// isResults 参数指示这个字段列表是否是函数的返回值列表。
func fieldListToString(fl *ast.FieldList, isResults bool) string {
	if fl == nil || len(fl.List) == 0 {
		if isResults {
			return ""
		}
		return "()"
	}

	var fields []string
	for _, field := range fl.List {
		typeStr := exprToString(field.Type)
		if len(field.Names) > 0 {
			for _, name := range field.Names {
				fields = append(fields, name.Name+" "+typeStr)
			}
		} else {
			fields = append(fields, typeStr)
		}
	}

	// 如果是返回值列表且有多个字段，或者是参数列表，用括号括起来。
	if isResults && len(fields) > 1 {
		return "(" + strings.Join(fields, ", ") + ")"
	} else if isResults && len(fields) == 1 {
		// 单个返回值不需要括号
		return strings.Join(fields, ", ")
	}
	// 参数列表始终需要括号
	return "(" + strings.Join(fields, ", ") + ")"
}

// GetFunctionLines 获取指定文件中指定函数的起始和结束行数
func GetFunctionLines(filePath, functionName string) (int, int, error) {
	fileSet := token.NewFileSet()
	node, parseErr := parser.ParseFile(fileSet, filePath, nil, parser.ParseComments)
	if parseErr != nil {
		return 0, 0, fmt.Errorf("failed to parse file: %w", parseErr)
	}

	var funcDecl *ast.FuncDecl
	ast.Inspect(node, func(n ast.Node) bool {
		if f, ok := n.(*ast.FuncDecl); ok && f.Name.Name == functionName {
			funcDecl = f
			return false
		}
		return true
	})
	if funcDecl == nil {
		return 0, 0, errors.New("function does not exist")
	}

	startLine := fileSet.Position(funcDecl.Body.Lbrace).Line
	endLine := fileSet.Position(funcDecl.Body.Rbrace).Line
	return startLine, endLine, nil
}
