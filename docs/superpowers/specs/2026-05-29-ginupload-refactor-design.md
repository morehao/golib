# ginupload Refactor Design

## Overview

Refactor `biz/gserver/ginupload/` package with four changes:
1. Add `binding` validation tags to DTOs, remove manual if-checks
2. Add Go comments to all DTO fields
3. Rename `ID` to `FileID` in all DTOs (both Go field and JSON tag)
4. Add a GET redirect endpoint for file URL access

## Change 1: Parameter Validation via binding Tags

Add `binding:"required"` (and `gt=0` where needed) to DTO fields so that
`ShouldBindJSON`/`ShouldBind` automatically performs zero-value checks.
Remove corresponding manual if-checks from handlers.

### Tag Map

| DTO | Field | Tag | Replaces Manual Check |
|---|---|---|---|
| `fileIDRequest` | `FileID` | `binding:"required"` | `req.ID == 0` |
| `presignDownloadRequest` | `FileID` | `binding:"required"` | `req.ID == 0` |
| `presignPartRequest` | `FileID` | `binding:"required"` | `req.ID == 0` |
| `presignPartRequest` | `PartNumber` | `binding:"required,gt=0"` | `req.PartNumber <= 0` |
| `checkExistRequest` | `Fingerprint` | `binding:"required"` | `req.Fingerprint == ""` |
| `completeMultipartRequest` | `FileID` | `binding:"required"` | `req.ID == 0` |
| `createMultipartRequest` | `Fingerprint` | `binding:"required"` | (new) |
| `createMultipartRequest` | `Name` | `binding:"required"` | (new) |
| `createMultipartRequest` | `Size` | `binding:"required"` | (new) |

### Handlers Affected

- `handleGetFileDetail` — remove `req.ID == 0`
- `handlePresignGetFileURL` — remove `req.ID == 0`
- `handleDeleteFile` — remove `req.ID == 0`
- `handleCheckExist` — remove `req.Fingerprint == ""`
- `handlePresignUploadPartURL` — remove `req.ID == 0` and `req.PartNumber <= 0`
- `handleCompleteMultipartUpload` — remove `req.ID == 0`
- `handleAbortMultipartUpload` — remove `req.ID == 0`

## Change 2: DTO Field Comments

Add godoc-style comments to every field in all DTO structs in `dto.go`.

## Change 3: ID → FileID Rename

Rename the `ID` field to `FileID` in all DTO structs, both the Go field name
and the JSON tag:

- `fileIDRequest.FileID` → `json:"file_id" form:"file_id"`
- `fileRecordResponse.FileID` → `json:"file_id"`
- `fileDetailResponse.FileID` → `json:"file_id"`
- `createMultipartResponse.FileID` → `json:"file_id"`
- `presignPartRequest.FileID` → `json:"file_id" form:"file_id"`
- `completeMultipartRequest.FileID` → `json:"file_id"`
- `presignDownloadRequest.FileID` → `json:"file_id" form:"file_id"`

Update all handler references: `req.ID` → `req.FileID`, `resp.ID` → `resp.FileID`.
Update all test references accordingly.

## Change 4: Redirect Endpoint

New handler `handleRedirectGetFileURL`:

- Route: `GET /redirect/:fileID`
- Extracts `fileID` from path param
- Optional `expires` query param (default 1h)
- Calls `fs.PresignGetFileURL(ctx, fileID, expires)` to get the URL
- Returns HTTP 302 redirect to the pre-signed URL

Register in `register.go`:
```go
r.GET("/redirect/:fileID", handleRedirectGetFileURL(fs))
```

## Files Modified

- `dto.go` — binding tags, comments, ID → FileID
- `file.go` — remove manual checks, ID → FileID, add redirect handler
- `upload.go` — remove manual checks, ID → FileID
- `register.go` — add redirect route
- `ginupload_test.go` — update test bodies, add redirect test cases

## API Contract Changes

- JSON field `id` → `file_id` in all request/response DTOs
- New endpoint `GET /file/redirect/:fileID` returns 302
- New validation errors from `binding:"required"` (gin formatted, not `id is required`)
