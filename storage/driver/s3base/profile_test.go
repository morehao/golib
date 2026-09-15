package s3base

import (
	"errors"
	"testing"

	"github.com/morehao/golib/storage"
)

// 零值 Limits 必须回落到 S3Limits。若把它当成"无限制"，DeleteObjects 就不会
// 分批，>1000 个 key 的请求会被后端直接拒绝 —— 这类漏填在运行期才暴露。
func TestProviderProfile_caps_DefaultsToS3Limits(t *testing.T) {
	caps := ProviderProfile{Name: "x", ForcePathStyle: true}.caps()
	if caps.Limits != S3Limits {
		t.Fatalf("zero Limits must fall back to S3Limits, got %+v", caps.Limits)
	}
	if caps.Limits.MaxDeleteBatch != 1000 {
		t.Errorf("MaxDeleteBatch = %d, want 1000", caps.Limits.MaxDeleteBatch)
	}
}

func TestProviderProfile_caps_ExplicitLimitsWin(t *testing.T) {
	custom := storage.Limits{MaxDeleteBatch: 500, MaxListPage: 250}
	caps := ProviderProfile{Limits: custom}.caps()
	if caps.Limits != custom {
		t.Fatalf("explicit Limits must be used as-is, got %+v", caps.Limits)
	}
}

func TestProviderProfile_caps_DoesNotOverclaim(t *testing.T) {
	caps := ProviderProfile{}.caps()
	if caps.Versioning {
		t.Error("Versioning must stay false:本期不开放按 VersionId 选择读")
	}
	// ListParts 已随 Multipart 契约落地并在 Driver 上实现，因此声明为 true。
	if !caps.ListParts {
		t.Error("ListParts 已实现，应声明为 true")
	}
	// 声明了子能力就必须声明父能力。
	if !caps.Multipart {
		t.Error("声明了 ListParts/PresignPart 就必须声明 Multipart")
	}
}

func TestS3Limits_MatchProtocolFacts(t *testing.T) {
	cases := []struct {
		name string
		got  int64
		want int64
	}{
		{"MaxSinglePut", S3Limits.MaxSinglePut, 5 << 30},
		{"MinPartSize", S3Limits.MinPartSize, 5 << 20},
		{"MaxParts", int64(S3Limits.MaxParts), 10000},
		{"MaxDeleteBatch", int64(S3Limits.MaxDeleteBatch), 1000},
		{"MaxListPage", int64(S3Limits.MaxListPage), 1000},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
}

func TestNew_RequiresProfile(t *testing.T) {
	_, err := New(storage.Config{Endpoint: "http://127.0.0.1:9000", Region: "us-east-1", AccessKey: "k"}, nil)
	if !errors.Is(err, storage.ErrInvalidConfig) {
		t.Fatalf("New without a ProviderProfile: err = %v, want ErrInvalidConfig", err)
	}
}

// VendorHeader 模式漏配 ConditionalWriteOption 会让条件写静默退化为覆盖写，
// 因此必须在构造期就拒绝，而不是等到并发去重出错才发现。
func TestNew_RejectsVendorHeaderWithoutOption(t *testing.T) {
	_, err := New(
		storage.Config{Endpoint: "http://127.0.0.1:9000", Region: "us-east-1", AccessKey: "k"},
		nil,
		WithProfile(ProviderProfile{Name: "x", ConditionalWrite: storage.ConditionalWriteVendorHeader}),
	)
	if !errors.Is(err, storage.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}
