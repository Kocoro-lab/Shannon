package attachments

import "testing"

// TestConsts_PlanAlignment pins the byte values to the Phase 2 contract.
// If a future refactor moves these around silently we lose the cloud-wide
// gate. Update the test in the same PR that adjusts the cap.
func TestConsts_PlanAlignment(t *testing.T) {
	t.Parallel()
	if MaxMultimodalBodyBytes != 40*1024*1024 {
		t.Errorf("MaxMultimodalBodyBytes = %d, want 40 MiB (plan §2 P0)", MaxMultimodalBodyBytes)
	}
	if MaxDecodedAttachmentBytes != 25*1024*1024 {
		t.Errorf("MaxDecodedAttachmentBytes = %d, want 25 MiB (plan §2 P0)", MaxDecodedAttachmentBytes)
	}
	if MaxFileRefSizeBytes != 500*1024*1024 {
		t.Errorf("MaxFileRefSizeBytes = %d, want 500 MiB (claude.ai parity)", MaxFileRefSizeBytes)
	}
	if MaxInlineSingleFile != 25*1024*1024 {
		t.Errorf("MaxInlineSingleFile = %d, want 25 MiB", MaxInlineSingleFile)
	}
	// Inline single-file cap must not exceed the aggregate inline budget —
	// otherwise a single inline file could blow the multimodal body cap
	// before the aggregate guard runs.
	if MaxInlineSingleFile > MaxDecodedAttachmentBytes {
		t.Errorf("MaxInlineSingleFile (%d) must not exceed MaxDecodedAttachmentBytes (%d)",
			MaxInlineSingleFile, MaxDecodedAttachmentBytes)
	}
	if MaxDecodedAttachmentBytes > MaxMultimodalBodyBytes {
		t.Errorf("MaxDecodedAttachmentBytes (%d) must not exceed MaxMultimodalBodyBytes (%d)",
			MaxDecodedAttachmentBytes, MaxMultimodalBodyBytes)
	}
}
