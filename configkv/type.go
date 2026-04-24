package configkv

type ConfigInfo struct {
	ID             uint
	GroupName      string
	Key            string
	ValueType      ValueType
	Value          string
	EncryptionMode EncryptionMode
	Description    string
	Status         Status
	CreatedAt      int64
	UpdatedAt      int64
}

type ConfigListResp struct {
	List  []*ConfigInfo
	Total int64
}

type CreateReq struct {
	Group       string
	Key         string
	ValueType   string
	Value       string
	Encrypted   bool
	Status      string
	Description string
}

type UpdateReq struct {
	Value       string
	Encrypted   bool
	Status      string
	Description string
}
