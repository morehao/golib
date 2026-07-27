package tos

import (
	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/driver/s3driver"
)

func init() {
	storage.RegisterStorage(string(storage.DriverTOS), New)
	storage.RegisterPathBuilder(string(storage.DriverTOS), NewPathBuilder)
}

var _ storage.Storage = (*s3driver.Driver)(nil)

func NewPathBuilder(cfg storage.Config) storage.PathBuilder {
	return &storage.S3PathBuilder{
		BaseURL:  cfg.BaseURL,
		Endpoint: cfg.Endpoint,
		Region:   cfg.Region,
		URLStyle: storage.URLStylePath,
	}
}

func New(cfg storage.Config) (storage.Storage, error) {
	return s3driver.New(cfg, NewPathBuilder(cfg))
}
