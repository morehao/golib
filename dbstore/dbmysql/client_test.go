package dbmysql

import (
	"context"
	"testing"

	"github.com/morehao/golib/glog"
	"github.com/stretchr/testify/assert"
)

func TestInitMysql(t *testing.T) {
	defer glog.Close()
	logCfg := &glog.LogConfig{
		Service:        "test",
		Level:          glog.DebugLevel,
		Writer:         glog.WriterConsole,
		RotateInterval: glog.RotateIntervalTypeDay,
		ExtraKeys:      []string{glog.KeyRequestId},
	}

	initLogErr := glog.InitLogger(logCfg)
	assert.Nil(t, initLogErr)
	cfg := &MysqlConfig{
		Addr:     "127.0.0.1:3306",
		Database: "practice",
		User:     "root",
		Password: "123456",
	}
	mysqlClient, initDbErr := InitMysql(cfg)
	assert.Nil(t, initDbErr)
	ctx := context.Background()
	ctx = context.WithValue(ctx, "requestId", "12312312312312")

	type User struct {
		ID   int
		Name string
		// 其他 user 表字段
	}

	var res []User
	findErr := mysqlClient.WithContext(ctx).Table("user").Find(&res).Error
	assert.Nil(t, findErr)
	glog.Infof(ctx, "find user %s", glog.ToJsonString(res))
	t.Log(glog.ToJsonString(res))
}

func TestInitMysqlWithoutInitLog(t *testing.T) {
	defer glog.Close()
	cfg := &MysqlConfig{
		Addr:     "127.0.0.1:3306",
		Database: "practice",
		User:     "root",
		Password: "123456",
	}
	mysqlClient, initDbErr := InitMysql(cfg)
	assert.Nil(t, initDbErr)
	ctx := context.Background()
	ctx = context.WithValue(ctx, "requestId", "12312312312312")
	type User struct {
		ID   int
		Name string
		// 其他 user 表字段
	}

	var res []User
	findErr := mysqlClient.WithContext(ctx).Table("user").Find(&res).Error
	assert.Nil(t, findErr)
	glog.Infof(ctx, "find user %s", glog.ToJsonString(res))
	t.Log(glog.ToJsonString(res))
}
