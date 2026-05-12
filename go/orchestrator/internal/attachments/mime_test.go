package attachments

import "testing"

// TestMimeClassification pins the three-way split documented in
// attachment-format-parity plan §2 P0. Adding a new MIME case here is the
// single source of truth for the gateway gate behavior — keep the table
// dense and grouped so reviewers can spot misclassifications.
func TestMimeClassification(t *testing.T) {
	t.Parallel()

	type expect struct {
		extractable bool
		passthrough bool
		dangerous   bool
	}

	cases := []struct {
		name      string
		mediaType string
		want      expect
	}{
		// ── Native Anthropic image MIMEs: all extractable, never passthrough.
		{"jpeg", "image/jpeg", expect{extractable: true}},
		{"png", "image/png", expect{extractable: true}},
		{"gif", "image/gif", expect{extractable: true}},
		{"webp", "image/webp", expect{extractable: true}},
		// ── Images cloud may transcode (HEIC/AVIF/TIFF/BMP).
		{"heic", "image/heic", expect{extractable: true}},
		{"heif", "image/heif", expect{extractable: true}},
		{"avif", "image/avif", expect{extractable: true}},
		{"tiff", "image/tiff", expect{extractable: true}},
		{"bmp", "image/bmp", expect{extractable: true}},
		{"svg", "image/svg+xml", expect{extractable: true}},
		// ── Unknown image subtype → neither extract nor passthrough; falls
		// through to gateway "default passthrough" treatment.
		{"image-unknown", "image/x-weird-format", expect{}},
		// ── Text family — all extractable.
		{"text-plain", "text/plain", expect{extractable: true}},
		{"text-csv", "text/csv", expect{extractable: true}},
		{"text-html", "text/html", expect{extractable: true}},
		{"text-markdown", "text/markdown", expect{extractable: true}},
		// ── Office / document formats.
		{"pdf", "application/pdf", expect{extractable: true}},
		{"docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", expect{extractable: true}},
		{"xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", expect{extractable: true}},
		{"pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation", expect{extractable: true}},
		{"doc-legacy", "application/msword", expect{extractable: true}},
		{"xls-legacy", "application/vnd.ms-excel", expect{extractable: true}},
		{"odt", "application/vnd.oasis.opendocument.text", expect{extractable: true}},
		{"rtf", "application/rtf", expect{extractable: true}},
		{"epub", "application/epub+zip", expect{extractable: true}},
		{"json", "application/json", expect{extractable: true}},
		{"xml", "application/xml", expect{extractable: true}},
		// ── Archives & a/v → passthrough only.
		{"zip", "application/zip", expect{passthrough: true}},
		{"tar", "application/x-tar", expect{passthrough: true}},
		{"gzip", "application/gzip", expect{passthrough: true}},
		{"7z", "application/x-7z-compressed", expect{passthrough: true}},
		{"rar", "application/vnd.rar", expect{passthrough: true}},
		{"dmg", "application/x-apple-diskimage", expect{passthrough: true}},
		{"octet-stream", "application/octet-stream", expect{passthrough: true}},
		{"audio-mp3", "audio/mpeg", expect{passthrough: true}},
		{"video-mp4", "video/mp4", expect{passthrough: true}},
		{"audio-wav", "audio/wav", expect{passthrough: true}},
		// ── Dangerous executables — rejected outright.
		{"exe", "application/x-msdownload", expect{dangerous: true}},
		{"msi", "application/x-ms-installer", expect{dangerous: true}},
		{"pe", "application/vnd.microsoft.portable-executable", expect{dangerous: true}},
		{"elf", "application/x-executable", expect{dangerous: true}},
		{"shell", "application/x-sh", expect{dangerous: true}},
		{"bat", "application/x-bat", expect{dangerous: true}},
		// ── Whitespace / case-insensitivity (caller may pass raw HTTP headers).
		{"pdf-mixed-case", "Application/PDF", expect{extractable: true}},
		{"zip-whitespace", "  application/zip  ", expect{passthrough: true}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsExtractable(tc.mediaType); got != tc.want.extractable {
				t.Errorf("IsExtractable(%q) = %v, want %v", tc.mediaType, got, tc.want.extractable)
			}
			if got := IsPassthrough(tc.mediaType); got != tc.want.passthrough {
				t.Errorf("IsPassthrough(%q) = %v, want %v", tc.mediaType, got, tc.want.passthrough)
			}
			if got := IsDangerous(tc.mediaType); got != tc.want.dangerous {
				t.Errorf("IsDangerous(%q) = %v, want %v", tc.mediaType, got, tc.want.dangerous)
			}
		})
	}
}

// TestMime_NoOverlap pins the invariant that the three classifiers must be
// mutually exclusive — every MIME falls into at most one bucket. Anything
// that's "neither" is intentional: the gateway treats it as default-
// passthrough (URL-only ref, no extraction attempted).
func TestMime_NoOverlap(t *testing.T) {
	t.Parallel()

	tests := []string{
		"image/jpeg", "image/heic", "image/x-weird",
		"text/plain", "text/csv",
		"application/pdf", "application/zip", "application/x-msdownload",
		"application/octet-stream", "audio/mpeg", "video/mp4",
		"application/x-sh", "application/x-bat",
	}

	for _, mt := range tests {
		mt := mt
		t.Run(mt, func(t *testing.T) {
			t.Parallel()
			count := 0
			if IsExtractable(mt) {
				count++
			}
			if IsPassthrough(mt) {
				count++
			}
			if IsDangerous(mt) {
				count++
			}
			if count > 1 {
				t.Errorf("MIME %q matched %d classifiers (must be at most 1)", mt, count)
			}
		})
	}
}

// TestIsSupportedMediaType_BackCompat keeps the deprecated wrapper aligned
// with the new IsExtractable || IsPassthrough definition so callers that
// haven't migrated still gate the same way.
func TestIsSupportedMediaType_BackCompat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mediaType string
		want      bool
	}{
		// Extractable types accepted.
		{"image/png", true},
		{"application/pdf", true},
		{"text/csv", true},
		{"application/json", true},
		// Passthrough types accepted (URL refs).
		{"application/zip", true},
		{"video/mp4", true},
		{"application/octet-stream", true},
		// Dangerous types rejected (neither extract nor passthrough).
		{"application/x-msdownload", false},
		{"application/x-bat", false},
		// Unknown image subtype → false (neither bucket).
		{"image/x-weird", false},
		// Empty → false.
		{"", false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.mediaType, func(t *testing.T) {
			t.Parallel()
			if got := IsSupportedMediaType(tc.mediaType); got != tc.want {
				t.Errorf("IsSupportedMediaType(%q) = %v, want %v", tc.mediaType, got, tc.want)
			}
		})
	}
}

// TestIsDangerousFilename covers the filename-based denylist used at
// channel webhook layers (Slack delivers .exe as application/octet-stream
// + .exe basename, so MIME-only IsDangerous is insufficient).
func TestIsDangerousFilename(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		// Executable / installer denylist.
		{"setup.exe", true},
		{"runme.bat", true},
		{"task.cmd", true},
		{"install.msi", true},
		{"payload.scr", true},
		{"shortcut.lnk", true},
		{"script.vbs", true},
		{"script.ps1", true},
		{"deploy.sh", true},
		{"profile.bash", true},
		{"profile.zsh", true},
		{"loader.hta", true},
		{"runner.jar", true},
		{"settings.reg", true},
		// Case-insensitive — Slack/Feishu may forward upper-case extensions.
		{"SETUP.EXE", true},
		{"Loader.HtA", true},
		// Safe formats stay safe.
		{"document.pdf", false},
		{"photo.heic", false},
		{"spec.docx", false},
		{"sheet.xlsx", false},
		{"archive.zip", false},
		{"video.mp4", false},
		{"code.go", false},  // .go is code, not executable
		{"hello.py", false}, // .py is code, not executable
		{"src.js", false},   // .js is code, not executable
		// Edge cases.
		{"", false},
		{"noextension", false},
		{".bashrc", false}, // hidden file, no real "ext" before a dot
		{"a.b.exe", true},  // multi-dot, ext is the last token
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsDangerousFilename(tc.name); got != tc.want {
				t.Errorf("IsDangerousFilename(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
