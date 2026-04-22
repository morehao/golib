package configkv

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type AdminEntry struct {
	GroupName   string    `json:"group_name"`
	Key         string    `json:"key"`
	ValueType   string    `json:"value_type"`
	Value       string    `json:"value"`
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateReq struct {
	GroupName   string `json:"group_name"`
	Key         string `json:"key"`
	Value       string `json:"value"`
	ValueType   string `json:"value_type"`
	Encrypted   bool   `json:"encrypted"`
	Description string `json:"description"`
}

type UpdateReq struct {
	GroupName   string `json:"group_name"`
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description"`
}

type Admin interface {
	ListGroups(ctx context.Context) ([]string, error)
	ListByGroup(ctx context.Context, group string) ([]AdminEntry, error)
	Search(ctx context.Context, group, keyword string) ([]AdminEntry, error)
	Create(ctx context.Context, req CreateReq) error
	Update(ctx context.Context, req UpdateReq) error
	Delete(ctx context.Context, group, key string) error
	Export(ctx context.Context, group string, codec Codec) ([]byte, error)
	Import(ctx context.Context, data []byte, codec Codec) error
}

type adminImpl struct {
	db        *gorm.DB
	crypto    Crypto
	codec     Codec
}

func newAdmin(db *gorm.DB, o *options) *adminImpl {
	return &adminImpl{
		db:    db,
		crypto: o.crypto,
		codec:  o.codec,
	}
}

func (a *adminImpl) ListGroups(ctx context.Context) ([]string, error) {
	var groups []string
	err := a.db.WithContext(ctx).Model(&Config{}).
		Distinct("group_name").
		Order("group_name").
		Pluck("group_name", &groups).Error
	return groups, err
}

func (a *adminImpl) ListByGroup(ctx context.Context, group string) ([]AdminEntry, error) {
	var configs []Config
	err := a.db.WithContext(ctx).Where("group_name = ?", group).Order("key").Find(&configs).Error
	if err != nil {
		return nil, err
	}

	entries := make([]AdminEntry, len(configs))
	for i, c := range configs {
		value := c.Value
		if c.ValueType == "secret_string" {
			value = "******"
		}
		entries[i] = AdminEntry{
			GroupName:   c.GroupName,
			Key:         c.Key,
			ValueType:   c.ValueType,
			Value:       value,
			Description: c.Description,
			UpdatedAt:   c.UpdatedAt,
		}
	}
	return entries, nil
}

func (a *adminImpl) Search(ctx context.Context, group, keyword string) ([]AdminEntry, error) {
	var configs []Config
	err := a.db.WithContext(ctx).
		Where("group_name = ? AND (`key` LIKE ? OR description LIKE ?)", group, "%"+keyword+"%", "%"+keyword+"%").
		Find(&configs).Error
	if err != nil {
		return nil, err
	}

	entries := make([]AdminEntry, len(configs))
	for i, c := range configs {
		value := c.Value
		if c.ValueType == "secret_string" {
			value = "******"
		}
		entries[i] = AdminEntry{
			GroupName:   c.GroupName,
			Key:         c.Key,
			ValueType:   c.ValueType,
			Value:       value,
			Description: c.Description,
			UpdatedAt:   c.UpdatedAt,
		}
	}
	return entries, nil
}

func (a *adminImpl) Create(ctx context.Context, req CreateReq) error {
	value := req.Value
	valueType := req.ValueType

	if req.Encrypted {
		if a.crypto == nil {
			return errCryptoNotConfigured
		}
		ciphertext, err := a.crypto.Encrypt(value)
		if err != nil {
			return fmt.Errorf("encrypt failed: %w", err)
		}
		value = ciphertext
		valueType = "secret_string"
	}

	config := Config{
		GroupName:   req.GroupName,
		Key:         req.Key,
		ValueType:   valueType,
		Value:       value,
		Description: req.Description,
	}
	return a.db.WithContext(ctx).Create(&config).Error
}

func (a *adminImpl) Update(ctx context.Context, req UpdateReq) error {
	return a.db.WithContext(ctx).Model(&Config{}).
		Where("group_name = ? AND `key` = ?", req.GroupName, req.Key).
		Updates(map[string]any{
			"value":       req.Value,
			"description": req.Description,
			"updated_at":  time.Now(),
		}).Error
}

func (a *adminImpl) Delete(ctx context.Context, group, key string) error {
	return a.db.WithContext(ctx).Where("group_name = ? AND `key` = ?", group, key).Delete(&Config{}).Error
}

func (a *adminImpl) Export(ctx context.Context, group string, codec Codec) ([]byte, error) {
	entries, err := a.ListByGroup(ctx, group)
	if err != nil {
		return nil, err
	}

	data := make(map[string]string)
	for _, e := range entries {
		data[e.Key] = e.Value
	}

	return codec.Marshal(data)
}

func (a *adminImpl) Import(ctx context.Context, data []byte, codec Codec) error {
	var items map[string]string
	if err := codec.Unmarshal(data, &items); err != nil {
		return fmt.Errorf("unmarshal failed: %w", err)
	}

	for key, value := range items {
		config := Config{
			GroupName: "",
			Key:       key,
			ValueType: "string",
			Value:     value,
		}
		if err := a.db.WithContext(ctx).Create(&config).Error; err != nil {
			return fmt.Errorf("import key %s failed: %w", key, err)
		}
	}
	return nil
}