package testkit

import (
	"fmt"
)

type BaseInitializer struct {
	appName string
}

func NewBaseInitializer(appName string) (*BaseInitializer, error) {
	if appName == "" {
		return nil, fmt.Errorf("app name is empty")
	}
	return &BaseInitializer{appName: appName}, nil
}

func (b *BaseInitializer) AppName() string {
	return b.appName
}
