package dbredis

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/morehao/golib/glog"
	"github.com/stretchr/testify/assert"
)

func TestInitRedis(t *testing.T) {
	defer glog.Close()
	logCfg := &glog.LogConfig{
		Service:   "app",
		Level:     glog.DebugLevel,
		Writer:    glog.WriterConsole,
		ExtraKeys: []string{glog.KeyRequestId},
	}
	initLogErr := glog.InitLogger(logCfg, glog.WithCallerSkip(1))
	assert.Nil(t, initLogErr)

	cfg := &RedisConfig{
		Service:  "test",
		Addr:     "127.0.0.1:6379",
		Password: "123456",
		DB:       0,
	}
	redisClient, err := InitRedis(cfg)
	assert.Nil(t, err)

	// 创建带有 requestId 的上下文
	ctx := context.Background()
	ctx = context.WithValue(ctx, "requestId", "12312312312312")

	// 测试一个成功的 GET 命令
	key := "test123"
	value := "value123"
	// 设置一个键值对，以便后面获取
	err = redisClient.Set(ctx, key, value, 0).Err()
	assert.Nil(t, err)

	// 获取键值对
	getResult, err := redisClient.Get(ctx, key).Result()
	assert.Nil(t, err)
	assert.Equal(t, value, getResult)

	// 测试一个失败的 GET 命令（键不存在）
	_, err = redisClient.Get(ctx, "nonexistent_key").Result()
	assert.NotNil(t, err)

	// 测试管道命令
	pipe := redisClient.Pipeline()
	pipe.Get(ctx, key)
	pipe.Get(ctx, "nonexistent_key")
	_, err = pipe.Exec(ctx)
	assert.NotNil(t, err) // 因为有一个键不存在，所以这里会有错误

	// 关闭 Redis 客户端
	err = redisClient.Close()
	assert.Nil(t, err)
	glog.Info(ctx, "done")
}

func TestInitRedisWithoutInitLog(t *testing.T) {
	defer glog.Close()

	cfg := &RedisConfig{
		Service:  "test",
		Addr:     "127.0.0.1:6379",
		Password: "",
		DB:       0,
	}
	redisClient, err := InitRedis(cfg)
	assert.Nil(t, err)

	// 创建带有 requestId 的上下文
	ctx := context.Background()
	ctx = context.WithValue(ctx, "requestId", "12312312312312")

	// 测试一个成功的 GET 命令
	key := "test123"
	value := "value123"
	// 设置一个键值对，以便后面获取
	err = redisClient.Set(ctx, key, value, 0).Err()
	assert.Nil(t, err)

	// 获取键值对
	getResult, err := redisClient.Get(ctx, key).Result()
	assert.Nil(t, err)
	assert.Equal(t, value, getResult)

	// 测试一个失败的 GET 命令（键不存在）
	_, err = redisClient.Get(ctx, "nonexistent_key").Result()
	assert.NotNil(t, err)

	// 测试管道命令
	pipe := redisClient.Pipeline()
	pipe.Get(ctx, key)
	pipe.Get(ctx, "nonexistent_key")
	_, err = pipe.Exec(ctx)
	assert.NotNil(t, err) // 因为有一个键不存在，所以这里会有错误

	// 关闭 Redis 客户端
	err = redisClient.Close()
	assert.Nil(t, err)
}

func TestSetNX(t *testing.T) {
	cfg := &RedisConfig{
		Service:  "test",
		Addr:     "127.0.0.1:6379",
		Password: "",
		DB:       0,
	}
	redisClient, err := InitRedis(cfg)
	assert.Nil(t, err)
	key := "test123"
	ok1, setErr1 := redisClient.SetNX(context.Background(), key, "value123", time.Second*2).Result()
	assert.Nil(t, setErr1)
	fmt.Println("ok1:", ok1)
	for {
		ok2, setErr2 := redisClient.SetNX(context.Background(), key, "value123", time.Second*2).Result()
		assert.Nil(t, setErr2)
		if ok2 {
			break
		}
		time.Sleep(time.Millisecond * 500)
	}
}
