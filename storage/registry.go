package storage

import (
	"fmt"
	"sync"
)

type StorageFactory func(Config) (Storage, error)
type PathBuilderFactory func(Config) PathBuilder

var (
	storageMu     sync.RWMutex
	storageReg    = make(map[string]StorageFactory)
	pathBuilderMu sync.RWMutex
	pathBuilderReg = make(map[string]PathBuilderFactory)
)

// RegisterStorage 注册存储驱动构造器。
// 通常由 driver 包的 init() 调用。
func RegisterStorage(name string, factory StorageFactory) {
	if name == "" {
		panic("storage: register storage factory with empty name")
	}
	if factory == nil {
		panic("storage: register nil storage factory")
	}
	storageMu.Lock()
	defer storageMu.Unlock()
	if _, exists := storageReg[name]; exists {
		panic(fmt.Sprintf("storage: storage factory %q already registered", name))
	}
	storageReg[name] = factory
}

// RegisterPathBuilder 注册路径构造器工厂。
// 通常由 driver 包的 init() 调用。
func RegisterPathBuilder(name string, factory PathBuilderFactory) {
	if name == "" {
		panic("storage: register path builder factory with empty name")
	}
	if factory == nil {
		panic("storage: register nil path builder factory")
	}
	pathBuilderMu.Lock()
	defer pathBuilderMu.Unlock()
	if _, exists := pathBuilderReg[name]; exists {
		panic(fmt.Sprintf("storage: path builder factory %q already registered", name))
	}
	pathBuilderReg[name] = factory
}

// New 根据 name 查注册表构建 Storage。
// name 是注册到注册表的驱动名，使用 DriverMinio/DriverCOS 等常量。
// 未注册时返回明确错误提示需 blank import 相应 driver 子包。
func New(name DriverType, cfg Config) (Storage, error) {
	if name == "" {
		return nil, fmt.Errorf("%w: Driver is required", ErrInvalidConfig)
	}

	storageMu.RLock()
	sf, sok := storageReg[string(name)]
	storageMu.RUnlock()

	pathBuilderMu.RLock()
	_, pok := pathBuilderReg[string(name)]
	pathBuilderMu.RUnlock()

	switch {
	case !sok && !pok:
		return nil, fmt.Errorf("%w: driver %q not registered; please blank import _ \"github.com/morehao/golib/storage/driver/%s\"",
			ErrInvalidConfig, name, name)
	case !sok:
		return nil, fmt.Errorf("%w: driver %q storage factory not registered; please check driver package", ErrInvalidConfig, name)
	case !pok:
		return nil, fmt.Errorf("%w: driver %q path builder factory not registered; please check driver package", ErrInvalidConfig, name)
	}

	return sf(cfg)
}

// Drivers 返回所有已注册驱动名称。
func Drivers() []string {
	storageMu.RLock()
	defer storageMu.RUnlock()

	names := make([]string, 0, len(storageReg))
	for name := range storageReg {
		names = append(names, name)
	}
	return names
}
