package attachments

// MaxMultimodalBodyBytes is the maximum request body size for endpoints that
// may carry inline base64 attachments (images, PDFs). 40 MB accommodates
// several high-resolution images plus JSON overhead, leaving buffer below
// Anthropic's 32 MB request limit after extraction / re-encoding decisions.
//
// Raised from 30 MB → 40 MB per attachment-format-parity plan §2 P0.
const MaxMultimodalBodyBytes = 40 * 1024 * 1024

// MaxAttachmentThumbnailBytes is the maximum thumbnail data URL size persisted
// in DB metadata. Oversized thumbnails are silently dropped.
const MaxAttachmentThumbnailBytes = 50 * 1024

// MaxDecodedAttachmentBytes is the maximum total decoded size of all
// inline attachments in a single request (25 MB). "Inline" = base64 bytes
// that travel in the request body; URL-based file_ref attachments use
// MaxFileRefSizeBytes instead.
//
// Raised from 20 MB → 25 MB per attachment-format-parity plan §2 P0.
const MaxDecodedAttachmentBytes = 25 * 1024 * 1024

// MaxFileRefSizeBytes is the single-file upper bound on URL-based
// attachments (the daemon downloads via URL + AuthHeader and stages
// locally). 500 MB aligns with claude.ai's published per-file limit.
//
// Added in attachment-format-parity plan §2 P0. Channels (slack /
// feishu / line / etc.) should clamp their own per-file size cap to
// this constant so a single source can't bypass the cloud-wide policy.
const MaxFileRefSizeBytes = 500 * 1024 * 1024

// MaxInlineSingleFile is the per-file cap for inline base64 attachments
// (the actual body that travels through the API). Anything larger should
// either be extracted to text (Phase 3) or stay URL-only.
//
// Added in attachment-format-parity plan §2 P0.
const MaxInlineSingleFile = 25 * 1024 * 1024
