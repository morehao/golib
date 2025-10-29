package ghttp

import (
	"context"
	"fmt"
	"time"

	"github.com/morehao/golib/protocol"
)

// ExampleUsage 展示改进后的HTTP客户端使用方法
func ExampleUsage() {
	// 创建客户端配置
	cfg := &protocol.HttpClientConfig{
		Module:   "example-service",
		Host:     "https://api.example.com",
		Timeout:  10 * time.Second,
		MaxRetry: 3,
	}
	
	client := NewClient(cfg)
	ctx := context.Background()

	// 示例1: 基本GET请求
	fmt.Println("=== 基本GET请求 ===")
	result, err := client.Get(ctx, "/users/1", RequestOption{})
	if err != nil {
		fmt.Printf("请求失败: %v\n", err)
		return
	}
	
	// 检查响应状态
	if result.IsSuccess() {
		fmt.Printf("请求成功，状态码: %d\n", result.HttpCode)
		fmt.Printf("响应内容: %s\n", result.String())
	}

	// 示例2: 直接映射到结构体 - GET请求
	fmt.Println("\n=== GET请求映射到结构体 ===")
	type User struct {
		ID    int    `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	
	var user User
	err = client.GetJSON(ctx, "/users/1", &user, RequestOption{})
	if err != nil {
		fmt.Printf("请求失败: %v\n", err)
		return
	}
	fmt.Printf("用户信息: %+v\n", user)

	// 示例3: POST请求映射到结构体
	fmt.Println("\n=== POST请求映射到结构体 ===")
	type CreateUserRequest struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	
	type CreateUserResponse struct {
		ID      int    `json:"id"`
		Message string `json:"message"`
	}
	
	requestData := CreateUserRequest{
		Name:  "张三",
		Email: "zhangsan@example.com",
	}
	
	var response CreateUserResponse
	err = client.PostJSON(ctx, "/users", &response, RequestOption{
		RequestBody: requestData,
	})
	if err != nil {
		fmt.Printf("请求失败: %v\n", err)
		return
	}
	fmt.Printf("创建用户响应: %+v\n", response)

	// 示例4: 使用自定义请求选项
	fmt.Println("\n=== 使用自定义请求选项 ===")
	opt := RequestOption{
		Headers: map[string]string{
			"Authorization": "Bearer token123",
			"X-Custom-Header": "custom-value",
		},
		Cookies: map[string]string{
			"session_id": "abc123",
		},
		ContentType: "application/json",
		Timeout:     5 * time.Second,
	}
	
	result, err = client.Get(ctx, "/protected-resource", opt)
	if err != nil {
		fmt.Printf("请求失败: %v\n", err)
		return
	}
	
	if result.IsSuccess() {
		fmt.Printf("受保护资源访问成功\n")
	}

	// 示例5: 手动处理响应
	fmt.Println("\n=== 手动处理响应 ===")
	result, err = client.Get(ctx, "/data", RequestOption{})
	if err != nil {
		fmt.Printf("请求失败: %v\n", err)
		return
	}
	
	// 检查响应状态
	if result.IsError() {
		fmt.Printf("请求出错，状态码: %d\n", result.HttpCode)
		return
	}
	
	// 获取响应头信息
	if contentType := result.Header.Get("Content-Type"); contentType != "" {
		fmt.Printf("响应类型: %s\n", contentType)
	}
	
	// 手动反序列化
	var data map[string]interface{}
	err = result.JSON(&data)
	if err != nil {
		fmt.Printf("反序列化失败: %v\n", err)
		return
	}
	fmt.Printf("数据: %+v\n", data)
}
