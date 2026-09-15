package storage

import "testing"

func TestConditionalWriteMode_String(t *testing.T) {
	cases := map[ConditionalWriteMode]string{
		ConditionalWriteNone:              "none",
		ConditionalWriteNativeIfNoneMatch: "native_if_none_match",
		ConditionalWriteVendorHeader:      "vendor_header",
		ConditionalWriteProcessLocal:      "process_local",
	}
	for m, want := range cases {
		if got := m.String(); got != want {
			t.Errorf("ConditionalWriteMode(%d).String() = %q, want %q", m, got, want)
		}
	}
	if got := ConditionalWriteMode(99).String(); got != "unknown" {
		t.Errorf("out-of-range String() = %q, want %q", got, "unknown")
	}
}

// ProcessLocal 必须能与 NativeIfNoneMatch 区分开：前者只在单进程内原子，
// 上层若据此做跨实例去重就会静默出错。枚举值的区分是这条语义的前提。
func TestConditionalWriteMode_ProcessLocalIsDistinct(t *testing.T) {
	if ConditionalWriteProcessLocal == ConditionalWriteNativeIfNoneMatch {
		t.Fatal("process-local atomicity must not be conflated with server-side atomicity")
	}
	if ConditionalWriteNone == ConditionalWriteNativeIfNoneMatch {
		t.Fatal("none must be distinguishable from a supported mode")
	}
}
