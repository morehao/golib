# ginupload Package Design

## Background

在 `biz/gserver/` 下新增 `ginupload` 包，提供通用上传相关路由能力。业务服务只需将 `gin.RouterGroup` 和 `filestore.FileStore` 实例传入注册函数，即可获得一套完整的文件上传/管理接口。

底层依赖：
- `filestore/` — 文件上传、秒传、分片上传、下载、删除的 core 逻辑
- `storage/` — 对象存储抽象层（S3/MinIO/OSS/COS/TOS）

## Package Structure

```
biz/gserver/ginupload/
├── register.go      # Register 入口函数
├── upload.go        # 上传相关 handler（简单上传 + 分片上传）
├── file.go          # 文件信息/下载/删除 handler
├── reqres.go        # 请求/响应 DTO
```

## Register Function

```go
package ginupload

import (
    "github.com/gin-gonic/gin"
    "github.com/morehao/golib/filestore"
)

func Register(group *gin.RouterGroup, fs *filestore.FileStore)
```

- 所有路由一次性注册
- 路由路径相对于传入的 group（调用方控制前缀）
- 采用 `POST /file/{action}` RPC 风格

## Route Specification

### Simple Upload

```
POST /file/upload
```

Request: `multipart/form-data`
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| file | file | Y | 上传文件 |
| fingerprint | string | N | SHA256 指纹；未传时服务端自动计算 |

Response (`gincontext.Success`):
```json
{
    "code": 0,
    "requestID": "xxx",
    "msg": "success",
    "data": {
        "id": 1,
        "fingerprint": "abc123...",
        "name": "photo.jpg",
        "size": 102400,
        "mime_type": "image/jpeg",
        "storage_path": "prefix/2026/05/28/photo.jpg"
    }
}
```

### Check Exist (秒传检测)

```
POST /file/checkExist
```

Request (JSON):
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| fingerprint | string | Y | SHA256 指纹 |

Response:
```json
{
    "code": 0,
    "requestID": "xxx",
    "msg": "success",
    "data": {
        "exists": true,
        "file": { "id": 1, "fingerprint": "...", "name": "...", "size": 102400, "mime_type": "image/jpeg" }
    }
}
```

### Init Multipart Upload

```
POST /file/initMultipartUpload
```

Request (JSON):
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| fingerprint | string | Y | SHA256 指纹 |
| name | string | Y | 原始文件名 |
| size | int64 | Y | 文件总大小 |
| mime_type | string | N | MIME 类型 |
| storage_path | string | Y | 存储路径（由调用方通过 KeyBuilder 构建） |

Response:
```json
{
    "code": 0,
    "requestID": "xxx",
    "msg": "success",
    "data": {
        "id": 1,
        "upload_id": "s3-upload-id-xxx",
        "fingerprint": "abc123..."
    }
}
```

### Presign Upload Part URL

```
POST /file/presignUploadPartURL
```

Request (JSON):
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| id | uint | Y | FileRecord ID |
| part_number | int32 | Y | 分片编号（从 1 开始） |
| expires | string | N | 过期时间，如 "1h"（默认 1h） |

Response:
```json
{
    "code": 0,
    "requestID": "xxx",
    "msg": "success",
    "data": {
        "url": "https://presign.example.com/...",
        "expires_in": 3600
    }
}
```

### Complete Multipart Upload

```
POST /file/completeMultipartUpload
```

Request (JSON):
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| id | uint | Y | FileRecord ID |
| parts | []Part | Y | 已上传分片列表 |

```go
type Part struct {
    PartNumber int32  `json:"part_number"`
    ETag       string `json:"etag"`
}
```

Response: 同 Simple Upload 的 data 结构（FileRecord，status=completed）

### Abort Multipart Upload

```
POST /file/abortMultipartUpload
```

Request (JSON):
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| id | uint | Y | FileRecord ID |

Response: data 为 null（仅 success）

### Get File Detail

```
POST /file/getFileDetail
```

Request (JSON):
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| id | uint | Y | FileRecord ID |

Response: data = FileRecord

### Presign Get File URL

```
POST /file/presignGetFileURL
```

Request (JSON):
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| id | uint | Y | FileRecord ID |
| expires | string | N | 过期时间（默认 1h） |

Response:
```json
{
    "code": 0,
    "requestID": "xxx",
    "msg": "success",
    "data": {
        "url": "https://presign.example.com/...",
        "expires_in": 3600
    }
}
```

### Delete File

```
POST /file/deleteFile
```

Request (JSON):
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| id | uint | Y | FileRecord ID |

Response: data 为 null（仅 success）

## Response & Error Handling

成功响应：`gincontext.Success(ctx, data)` → `{code: 0, requestID, msg, data}`

错误响应：`gincontext.Fail(ctx, err)` → `{code: -1, requestID, msg: err.Error(), data: null}`

filestore 返回的 error 通过 `Fail` 自动映射到 HTTP 响应中：
- `ErrFileNotFound` → 404
- `ErrInvalidArgument` → 400
- `ErrNotMultipartUpload` → 400

## Swagger Annotations

每个 handler 函数需添加标准 swagger 注释：

```go
// Upload
// @Summary      simple upload
// @Description  upload file with fingerprint dedup
// @Tags         file
// @Accept       multipart/form-data
// @Produce      json
// @Param        file formData file true "file to upload"
// @Param        fingerprint formData string false "SHA256 fingerprint"
// @Success      200 {object} gincontext.DtoRender{data=model.FileRecord}
// @Router       /file/upload [post]
func handleUpload(fs *filestore.FileStore) gin.HandlerFunc { ... }
```

## Usage Example

```go
package main

import (
    "github.com/gin-gonic/gin"
    "github.com/morehao/golib/biz/gserver/ginupload"
    "github.com/morehao/golib/biz/gserver/ginserver"
    "github.com/morehao/golib/filestore"
    "github.com/morehao/golib/storage"
)

func main() {
    engine := gin.Default()

    // 1. 创建 ginserver 路由分组
    rg := ginserver.NewRouterGroups(engine, "myapp",
        ginserver.Version{Name: "v1"},
    )
    group := rg.MustGetGroup("v1")  // -> /v1/myapp

    // 2. 初始化 filestore
    st, _ := storage.New(spec.Config{...})
    db, _ := gorm.Open(...)
    fs, _ := filestore.New(db, st)

    // 3. 注册上传路由 -> /v1/myapp/file/upload, /v1/myapp/file/checkExist ...
    ginupload.Register(group, fs)

    engine.Run()
}
```
