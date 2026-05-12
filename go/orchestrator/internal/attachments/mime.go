package attachments

import (
	"path/filepath"
	"strings"
)

// dangerousExtensions lists filename extensions for executable / installer
// formats. Channel webhooks (Slack/Feishu/...) MUST check the filename in
// addition to the MIME type — Slack delivers many of these as
// `application/octet-stream`, which would otherwise slip past IsDangerous.
var dangerousExtensions = map[string]struct{}{
	".exe":  {},
	".dll":  {},
	".bat":  {},
	".cmd":  {},
	".com":  {},
	".msi":  {},
	".scr":  {},
	".pif":  {},
	".vbs":  {},
	".vbe":  {},
	".ps1":  {},
	".psm1": {},
	".sh":   {},
	".bash": {},
	".zsh":  {},
	".csh":  {},
	".hta":  {},
	".jar":  {},
	".lnk":  {},
	".reg":  {},
}

// IsDangerousFilename reports whether the filename's extension is on the
// executable / installer denylist. Use alongside IsDangerous(mime) at any
// gateway boundary: a Slack-hosted .exe arrives as octet-stream + .exe
// basename, so the MIME check alone is insufficient.
func IsDangerousFilename(name string) bool {
	if name == "" {
		return false
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		return false
	}
	_, ok := dangerousExtensions[ext]
	return ok
}

// MIME classification for the attachment-format-parity plan (§2 P0):
//
// Three layers, evaluated in order at the gateway:
//
//   1. IsDangerous  — executable / installer types. ALWAYS rejected.
//   2. IsExtractable — Cloud can extract text or transcode (PDF / DOCX /
//      XLSX / PPTX / CSV / TXT / JSON / HTML / HEIC / AVIF / TIFF / BMP /
//      image+text subtypes). Future Cloud extraction service handles these.
//   3. IsPassthrough — Cloud doesn't process but the daemon may still
//      handle (archives, video, audio, octet-stream). URL-only ref is
//      forwarded; the agent uses bash / archive_extract on its end.
//
// Anything that isn't dangerous and isn't on either list falls through
// to passthrough by default — "user can upload anything, daemon decides
// what to do with it". This is the "universal accept" principle from the
// plan.
//
// IsSupportedMediaType is retained as a deprecated thin wrapper for
// callers that haven't migrated to the explicit Extractable / Passthrough
// distinction yet. New code MUST NOT use it.

// IsDangerous reports whether the media type is an executable / installer
// format that should never traverse the cloud → daemon pipeline regardless
// of size or origin. Anything matched here is rejected at the gateway.
//
// Filename-based detection (e.g. ".exe" basename when MIME says
// "application/octet-stream") is the responsibility of the caller — this
// helper only inspects the MIME string.
func IsDangerous(mediaType string) bool {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case
		"application/x-msdownload",       // .exe / .dll
		"application/x-ms-installer",     // .msi
		"application/x-msi",              // .msi alt
		"application/vnd.microsoft.portable-executable",
		"application/x-dosexec",
		"application/x-executable",       // generic ELF / executable
		"application/x-sh",               // .sh — not strictly dangerous but ambiguous; opt out for safety
		"application/x-bat",              // .bat
		"application/x-msdos-program",    // .com / .bat (legacy)
		"application/x-msi-installer":
		return true
	}
	return false
}

// IsExtractable reports whether the cloud extraction service can read text
// out of (or transcode an image format from) this media type. When true,
// the orchestrator may populate FileAttachment.DocumentB64 (PDF) or
// FileAttachment.ExtractedText (everything else). Concrete extraction is
// implemented in Phase 3.
func IsExtractable(mediaType string) bool {
	mt := strings.ToLower(strings.TrimSpace(mediaType))
	// All text/* subtypes are trivially extractable (CSV, TXT, HTML, MD, JSON…).
	if strings.HasPrefix(mt, "text/") {
		return true
	}
	// Image subtypes the cloud may need to transcode (HEIC/AVIF → JPEG,
	// TIFF/BMP → PNG/JPEG) plus the four native Anthropic image MIMEs
	// (jpeg/png/gif/webp). Letting the native four match here is harmless
	// — the extraction path is a no-op for them, and IsExtractable just
	// gates whether the cloud is allowed to inspect.
	if strings.HasPrefix(mt, "image/") {
		switch mt {
		case "image/jpeg", "image/jpg", "image/png", "image/gif", "image/webp",
			"image/heic", "image/heif", "image/avif",
			"image/tiff", "image/bmp", "image/x-bmp",
			"image/svg+xml":
			return true
		}
		// Unknown image/* subtype falls through (passthrough).
		return false
	}
	switch mt {
	case
		"application/pdf",
		"application/json",
		"application/xml",
		"application/x-yaml",
		"application/javascript",
		"application/typescript",
		// Office document family (extraction Phase 3):
		"application/msword",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.ms-excel",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.ms-powerpoint",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
		// OpenDocument family — claude.ai supports ODT.
		"application/vnd.oasis.opendocument.text",
		"application/vnd.oasis.opendocument.spreadsheet",
		"application/vnd.oasis.opendocument.presentation",
		// RTF / EPUB are in the claude.ai supported list.
		"application/rtf",
		"application/epub+zip",
		"text/rtf":
		return true
	}
	return false
}

// IsPassthrough reports whether the media type is one cloud can't extract
// from but the daemon should still receive as a URL-only file_ref. The
// agent then uses local tools (bash, archive_extract) on it.
//
// Anything not dangerous and not extractable defaults to passthrough at
// the caller — IsPassthrough is conservative and only lists the common
// types we explicitly expect (archives, video/audio, octet-stream). The
// gateway fallback should treat "not extractable AND not dangerous" as
// passthrough regardless of this allowlist.
func IsPassthrough(mediaType string) bool {
	mt := strings.ToLower(strings.TrimSpace(mediaType))
	if IsDangerous(mt) {
		return false
	}
	if strings.HasPrefix(mt, "audio/") || strings.HasPrefix(mt, "video/") {
		return true
	}
	switch mt {
	case
		"application/zip",
		"application/x-zip-compressed",
		"application/x-tar",
		"application/gzip",
		"application/x-gzip",
		"application/x-bzip2",
		"application/x-7z-compressed",
		"application/x-rar-compressed",
		"application/vnd.rar",
		"application/x-apple-diskimage", // .dmg
		"application/octet-stream",
		"application/x-iso9660-image":
		return true
	}
	return false
}

// IsSupportedMediaType is retained as a thin wrapper over the explicit
// IsExtractable / IsPassthrough classification for callers that haven't
// migrated yet. New code should call the specific helper that matches
// the intent.
//
// Deprecated: use IsExtractable or IsPassthrough directly. The old
// "binary reject on unknown MIME" gate has been removed — the gateway
// now lets unknown non-dangerous types fall through to URL-only refs
// (see plan §2 P0).
func IsSupportedMediaType(mediaType string) bool {
	return IsExtractable(mediaType) || IsPassthrough(mediaType)
}
