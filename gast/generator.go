package gast

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"strings"
)

// AddMethodToInterfaceInFile 在指定的文件中查找指定的接口，并在其中添加指定的方法
func AddMethodToInterfaceInFile(file, interfaceName, receiverTypeName, methodName string) error {

	// 查找接收者类型的方法声明
	methodDecl, found, findErr := FindMethod(file, receiverTypeName, methodName)
	if findErr != nil {
		return findErr
	}
	if !found {
		return errors.New("method not found: " + methodName)
	}
	methodType := methodDecl.Type

	methodExists := false

	src, err := os.ReadFile(file)
	if err != nil {
		return err
	}

	fileSet := token.NewFileSet()
	node, parseFileErr := parser.ParseFile(fileSet, file, src, parser.ParseComments)
	if parseFileErr != nil {
		return parseFileErr
	}

	// 遍历文件中的所有类型声明，查找指定的接口
	ast.Inspect(node, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.TypeSpec:
			if x.Name.Name == interfaceName {
				iface, ok := x.Type.(*ast.InterfaceType)
				if !ok {
					return true
				}
				for _, method := range iface.Methods.List {
					if len(method.Names) > 0 && method.Names[0].Name == methodName {
						methodExists = true
						return false
					}
				}
				if !methodExists {
					// 将方法添加到接口中
					iface.Methods.List = append(iface.Methods.List, &ast.Field{
						Names: []*ast.Ident{ast.NewIdent(methodName)},
						Type:  methodType,
					})
				}
			}
		}
		return true
	})

	if !methodExists {
		// 根据修改后的AST生成新代码
		var buf bytes.Buffer
		if err := format.Node(&buf, fileSet, node); err != nil {
			return err
		}

		// 重写文件
		if err := os.WriteFile(file, buf.Bytes(), 0644); err != nil {
			return err
		}
	}

	return nil
}

// AddContentToFunc 在指定函数的函数体内添加内容
func AddContentToFunc(functionFilepath, functionName, content string) error {
	// 解析整个文件
	fileSet := token.NewFileSet()
	node, parseErr := parser.ParseFile(fileSet, functionFilepath, nil, parser.ParseComments)
	if parseErr != nil {
		return fmt.Errorf("failed to parse file: %w", parseErr)
	}

	// 查找目标函数
	var funcDecl *ast.FuncDecl
	ast.Inspect(node, func(n ast.Node) bool {
		if f, ok := n.(*ast.FuncDecl); ok && f.Name.Name == functionName {
			funcDecl = f
			return false
		}
		return true
	})
	if funcDecl == nil {
		return errors.New("function does not exist")
	}

	// 直接插入内容
	newStmt := &ast.ExprStmt{
		X: &ast.BasicLit{
			Kind:  token.STRING,
			Value: content,
		},
	}
	funcDecl.Body.List = append(funcDecl.Body.List, newStmt)

	// 使用 bytes.Buffer 处理修改后的内容
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fileSet, node); err != nil {
		return fmt.Errorf("failed to write updated content: %w", err)
	}

	// 打开已存在的目标文件
	file, openErr := os.OpenFile(functionFilepath, os.O_WRONLY|os.O_TRUNC, 0644)
	if openErr != nil {
		return fmt.Errorf("failed to open destination file: %v", openErr)
	}
	defer file.Close()

	// 将处理后的代码写回文件
	if _, writeErr := file.Write(buf.Bytes()); writeErr != nil {
		return fmt.Errorf("failed to write content: %w", writeErr)
	}
	return nil
}

// AddFunction 将指定的函数内容添加到指定文件中，如果文件不存在包声明则添加包声明
func AddFunction(functionFilepath, content, pkgName string) error {
	// 解析目标文件
	fileSet := token.NewFileSet()
	node, parseErr := parser.ParseFile(fileSet, functionFilepath, nil, parser.ParseComments)
	if parseErr != nil {
		// 如果文件解析失败，认为文件不存在或是空文件，创建新的文件节点
		node = &ast.File{
			Name: &ast.Ident{Name: pkgName},
		}
	}

	// 解析新的函数声明
	newFuncNode, parseFuncErr := parser.ParseFile(fileSet, "", "package "+pkgName+"\n"+content, parser.ParseComments)
	if parseFuncErr != nil {
		return fmt.Errorf("failed to parse new function: %w", parseFuncErr)
	}

	// 查找新的函数声明
	var newFuncDecl *ast.FuncDecl
	for _, decl := range newFuncNode.Decls {
		if funcDecl, ok := decl.(*ast.FuncDecl); ok {
			newFuncDecl = funcDecl
			break
		}
	}
	if newFuncDecl == nil {
		return fmt.Errorf("no function declaration found in content")
	}

	// 检查目标文件中是否有包声明
	hasPackage := node.Name != nil && node.Name.Name != ""

	// 如果目标文件没有包声明，设置包声明
	if !hasPackage {
		node.Name = &ast.Ident{Name: pkgName}
	}

	// 将新的函数声明添加到目标文件的声明列表中
	node.Decls = append(node.Decls, newFuncDecl)

	// 使用 bytes.Buffer 处理修改后的内容
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fileSet, node); err != nil {
		return fmt.Errorf("failed to write updated content: %w", err)
	}

	// 打开目标文件进行写入
	file, openErr := os.OpenFile(functionFilepath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if openErr != nil {
		return fmt.Errorf("failed to open destination file: %v", openErr)
	}
	defer file.Close()

	// 将处理后的代码写回文件
	if _, writeErr := file.Write(buf.Bytes()); writeErr != nil {
		return fmt.Errorf("failed to write content: %w", writeErr)
	}

	return nil
}

// AddMethodToInterface 将指定接收者类型的方法添加到指定文件中的接口中。
func AddMethodToInterface(filePath, receiverType, methodName, interfaceName string) error {
	// 检查接口是否已经包含该方法。
	contains, err := interfaceContainsMethod(filePath, interfaceName, methodName)
	if err != nil {
		return err
	}
	if contains {
		// 接口已包含该方法，直接返回。
		return nil
	}

	// 获取方法声明字符串。
	methodDecl, err := getMethodDeclaration(filePath, receiverType, methodName)
	if err != nil {
		return err
	}

	// 将方法声明添加到接口中。
	err = addContentToInterfaceInFile(filePath, methodDecl, interfaceName)
	if err != nil {
		return err
	}

	return nil
}

// addContentToInterfaceInFile 将给定内容添加到文件中指定的接口中。
func addContentToInterfaceInFile(filePath, content, interfaceName string) error {
	// 读取文件内容。
	lines, err := readLines(filePath)
	if err != nil {
		return err
	}

	// 在接口定义中插入内容。
	inserted, err := insertIntoInterface(lines, content, interfaceName)
	if err != nil {
		return err
	}
	if !inserted {
		return errors.New("接口未找到或内容已存在")
	}

	// 将修改后的内容写回文件。
	return writeLines(filePath, lines)
}

// readLines 读取文件的所有行。
func readLines(filePath string) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

// insertIntoInterface 在接口定义中插入内容。
func insertIntoInterface(lines []string, content, interfaceName string) (bool, error) {
	foundInterface := false
	inserted := false
	for i, line := range lines {
		if strings.Contains(line, fmt.Sprintf("type %s interface {", interfaceName)) {
			foundInterface = true
		} else if foundInterface && strings.Contains(line, "}") {
			lines[i] = "\t" + content + "\n" + line
			inserted = true
			break
		}
	}
	return inserted, nil
}

// writeLines 将所有行写回文件。
func writeLines(filePath string, lines []string) error {
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, line := range lines {
		_, err := writer.WriteString(line + "\n")
		if err != nil {
			return err
		}
	}
	return writer.Flush()
}

// exprToString 将 AST 表达式转换为字符串，只返回类型名称。
func exprToString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident: // 标识符
		return t.Name
	case *ast.SelectorExpr: // 选择表达式，如包名.类型名
		return exprToString(t.X) + "." + t.Sel.Name
	case *ast.StarExpr: // 指针类型
		return "*" + exprToString(t.X)
	case *ast.ArrayType: // 数组类型
		return "[]" + exprToString(t.Elt)
	// 这里可以添加更多的类型处理，如 Map, Chan, Func 等。
	default:
		return ""
	}
}

// AddContentToFuncWithLineNumber 将内容插入到指定文件内指定函数的函数体中的指定位置，并覆盖原函数体。
// filePath: 要操作的文件的路径。
// functionName: 要操作的函数名称。根据函数名称来定位函数体的位置。
// content: 要插入的内容。会插入到指定行号的地方。
// lineNumber: 插入内容的行号。正数表示从函数体起始位置开始计算的行号，负数表示从函数体结束位置开始计算的行号。 例如，1 表示在函数体内的第一行位置插入内容，-1 表示在函数体结束前的一行插入内容。
func AddContentToFuncWithLineNumber(filePath, functionName, content string, lineNumber int) error {
	startLine, endLine, err := GetFunctionLines(filePath, functionName)
	if err != nil {
		return err
	}

	var insertLine int
	if lineNumber > 0 {
		insertLine = startLine + lineNumber - 1
	} else {
		insertLine = endLine + lineNumber + 1
	}

	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	var buf bytes.Buffer
	scanner := bufio.NewScanner(file)
	currentLine := 0
	for scanner.Scan() {
		currentLine++
		if currentLine == insertLine {
			buf.WriteString(content + "\n")
		}
		buf.WriteString(scanner.Text() + "\n")
	}
	if scannerErr := scanner.Err(); scannerErr != nil {
		return fmt.Errorf("failed to read file: %w", scannerErr)
	}

	// 格式化文件内容
	formattedContent, err := format.Source(buf.Bytes())
	if err != nil {
		return fmt.Errorf("failed to format file content: %w", err)
	}

	// 写回文件
	if err := os.WriteFile(filePath, formattedContent, 0644); err != nil {
		return fmt.Errorf("failed to write back to file: %w", err)
	}

	return nil
}

// AddMapKVToFile 在指定文件中添加 map 变量，并向其中添加 kv。
func AddMapKVToFile(filePath, varName, mapType, keyName, value string) error {

	fileSet := token.NewFileSet()
	node, err := parser.ParseFile(fileSet, filePath, nil, parser.AllErrors)
	if err != nil {
		return err
	}

	if cl, found := findMapVar(node, varName); found {
		appendMapKV(cl, keyName, value)
	} else {
		createMapVar(node, varName, mapType, keyName, value)
	}

	// 写回文件
	outFile, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	return format.Node(outFile, fileSet, node)
}

// findMapVar 查找是否存在指定变量名的 map
func findMapVar(node *ast.File, varName string) (*ast.CompositeLit, bool) {
	for _, decl := range node.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.VAR {
			continue
		}
		for _, spec := range genDecl.Specs {
			valSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range valSpec.Names {
				if name.Name == varName {
					cl, ok := valSpec.Values[i].(*ast.CompositeLit)
					if ok {
						return cl, true
					}
				}
			}
		}
	}
	return nil, false
}

// createMapVar 创建新的 map 变量
func createMapVar(node *ast.File, varName, mapType, keyName, value string) {
	decl := &ast.GenDecl{
		Tok: token.VAR,
		Specs: []ast.Spec{
			&ast.ValueSpec{
				Names: []*ast.Ident{ast.NewIdent(varName)},
				Type:  ast.NewIdent(mapType),
				Values: []ast.Expr{
					&ast.CompositeLit{
						Type: ast.NewIdent(mapType),
						Elts: []ast.Expr{
							&ast.KeyValueExpr{
								Key:   ast.NewIdent(keyName),
								Value: &ast.BasicLit{Kind: token.STRING, Value: value},
							},
						},
					},
				},
			},
		},
	}
	node.Decls = append(node.Decls, decl)
}

// appendMapKV 向已有 map 中追加 kv
func appendMapKV(cl *ast.CompositeLit, keyName, value string) {
	kv := &ast.KeyValueExpr{
		Key:   ast.NewIdent(keyName),
		Value: &ast.BasicLit{Kind: token.STRING, Value: value},
	}
	cl.Elts = append(cl.Elts, kv)
}

// AddConstToFile 向 Go 文件中追加常量（若不存在则创建 const 块，自动检查重名，自动 gofmt）
func AddConstToFile(filePath, constName, constValue string) error {
	fileSet := token.NewFileSet()
	node, err := parser.ParseFile(fileSet, filePath, nil, parser.AllErrors)
	if err != nil {
		return err
	}

	// 检查是否已存在该常量定义
	if constExists(node, constName) {
		return nil // 已存在，跳过追加
	}

	added := false

	// 查找已有 const 分组
	for _, decl := range node.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.CONST {
			continue
		}

		// 找到了 const 分组，直接追加
		genDecl.Specs = append(genDecl.Specs, &ast.ValueSpec{
			Names:  []*ast.Ident{ast.NewIdent(constName)},
			Values: []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: constValue}},
		})
		added = true
		break
	}

	// 如果没有 const 分组，创建一个新的
	if !added {
		newDecl := &ast.GenDecl{
			Tok: token.CONST,
			Specs: []ast.Spec{
				&ast.ValueSpec{
					Names:  []*ast.Ident{ast.NewIdent(constName)},
					Values: []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: constValue}},
				},
			},
		}
		// 插入到文件顶部（import 之后）
		insertAt := 0
		for i, decl := range node.Decls {
			if genDecl, ok := decl.(*ast.GenDecl); ok && genDecl.Tok == token.IMPORT {
				insertAt = i + 1
			}
		}
		node.Decls = append(node.Decls[:insertAt], append([]ast.Decl{newDecl}, node.Decls[insertAt:]...)...)
	}

	// 使用 gofmt 格式化写回文件
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fileSet, node); err != nil {
		return err
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, formatted, 0644)
}

// constExists 检查常量是否已存在
func constExists(node *ast.File, constName string) bool {
	for _, decl := range node.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.CONST {
			continue
		}
		for _, spec := range genDecl.Specs {
			valSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range valSpec.Names {
				if name.Name == constName {
					return true
				}
			}
		}
	}
	return false
}
