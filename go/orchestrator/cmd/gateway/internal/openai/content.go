package openai

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/attachments"
)


// RawAttachment holds extracted binary data from a content block.
type RawAttachment struct {
	MediaType  string // e.g. "image/png", "application/pdf"
	Data       []byte // decoded binary (for base64 sources)
	URL        string // original URL (for URL sources)
	SourceType string // "base64" or "url"
	Filename   string // optional original filename
}

// ParsedContent is the result of parsing a content field.
type ParsedContent struct {
	Text        string
	Attachments []RawAttachment
}

// ContentBlock represents one element of an OpenAI-compatible content array.
type ContentBlock struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
	File     *FileData `json:"file,omitempty"`
}

// ImageURL represents an image_url content block.
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

// FileData represents a file content block.
type FileData struct {
	FileData string `json:"file_data"`
	Filename string `json:"filename,omitempty"`
}

// ParseContent handles both string and []ContentBlock formats for the OpenAI content field.
func ParseContent(raw json.RawMessage) (*ParsedContent, error) {
	if len(raw) == 0 {
		return &ParsedContent{}, nil
	}

	// Try string first
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return &ParsedContent{Text: s}, nil
	}

	// Try array of content blocks
	var blocks []ContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, fmt.Errorf("content must be string or array of content blocks: %w", err)
	}

	result := &ParsedContent{}
	var textParts []string
	var totalBytes int

	for _, block := range blocks {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)

		case "image_url":
			if block.ImageURL == nil {
				continue
			}
			att, size, err := parseImageURL(block.ImageURL.URL)
			if err != nil {
				return nil, fmt.Errorf("parse image_url: %w", err)
			}
			totalBytes += size
			if totalBytes > attachments.MaxDecodedAttachmentBytes {
				return nil, fmt.Errorf("total attachment size %d bytes exceeds %d byte limit", totalBytes, attachments.MaxDecodedAttachmentBytes)
			}
			result.Attachments = append(result.Attachments, *att)

		case "file":
			if block.File == nil || block.File.FileData == "" {
				continue
			}
			att, size, err := parseDataURI(block.File.FileData)
			if err != nil {
				return nil, fmt.Errorf("parse file data: %w", err)
			}
			if block.File.Filename != "" {
				att.Filename = block.File.Filename
			}
			totalBytes += size
			if totalBytes > attachments.MaxDecodedAttachmentBytes {
				return nil, fmt.Errorf("total attachment size %d bytes exceeds %d byte limit", totalBytes, attachments.MaxDecodedAttachmentBytes)
			}
			result.Attachments = append(result.Attachments, *att)
		}
	}

	result.Text = strings.Join(textParts, "\n")
	return result, nil
}

// parseImageURL parses an image_url value, which can be a data URI or an external URL.
func parseImageURL(url string) (*RawAttachment, int, error) {
	if strings.HasPrefix(url, "data:") {
		return parseDataURI(url)
	}
	mt := guessMediaType(url)
	// URL-based path: only reject dangerous types (executables / installers).
	// Unknown extensions fall through to a URL-only ref — the daemon fetches
	// the URL and uses local tools (bash, archive_extract) to make sense of
	// it. See attachment-format-parity §2 P0.
	if attachments.IsDangerous(mt) {
		return nil, 0, fmt.Errorf("rejected attachment type from URL: %s", mt)
	}
	return &RawAttachment{
		URL:        url,
		SourceType: "url",
		MediaType:  mt,
	}, 0, nil
}

// parseDataURI decodes a data URI (data:<mediatype>;base64,<data>) into a RawAttachment.
func parseDataURI(uri string) (*RawAttachment, int, error) {
	parts := strings.SplitN(uri, ",", 2)
	if len(parts) != 2 {
		return nil, 0, fmt.Errorf("invalid data URI format")
	}
	header := parts[0]
	b64Data := parts[1]

	// Extract media type from header like "data:image/png;base64"
	mediaType := ""
	if strings.HasPrefix(header, "data:") {
		mediaType = strings.TrimPrefix(header, "data:")
		mediaType = strings.TrimSuffix(mediaType, ";base64")
	}

	// Reject empty MIME types early (e.g. "data:;base64,..."). Base64 inline
	// uploads must be IsExtractable — passthrough types (archives / a/v /
	// octet-stream) should arrive as URL-only refs, not inlined. Dangerous
	// types are always rejected.
	if mediaType == "" {
		return nil, 0, fmt.Errorf("missing media type in data URI (expected data:<type>;base64,...)")
	}
	if attachments.IsDangerous(mediaType) {
		return nil, 0, fmt.Errorf("rejected attachment type: %s (executables / installers are not accepted)", mediaType)
	}
	if !attachments.IsExtractable(mediaType) {
		return nil, 0, fmt.Errorf("unsupported attachment type for inline upload: %s (use URL-based attachment instead)", mediaType)
	}

	// Try standard base64 first, then raw (no padding) variant
	data, err := base64.StdEncoding.DecodeString(b64Data)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(b64Data)
		if err != nil {
			return nil, 0, fmt.Errorf("base64 decode: %w", err)
		}
	}

	return &RawAttachment{
		MediaType:  mediaType,
		Data:       data,
		SourceType: "base64",
	}, len(data), nil
}

// guessMediaType infers a media type from a URL's file extension.
func guessMediaType(url string) string {
	lower := strings.ToLower(url)
	// Strip query string and fragment for suffix matching
	if idx := strings.IndexAny(lower, "?#"); idx != -1 {
		lower = lower[:idx]
	}
	switch {
	case strings.HasSuffix(lower, ".png"):
		return "image/png"
	case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(lower, ".gif"):
		return "image/gif"
	case strings.HasSuffix(lower, ".webp"):
		return "image/webp"
	case strings.HasSuffix(lower, ".pdf"):
		return "application/pdf"
	default:
		return "" // Unknown extension; caller should reject or require explicit media_type
	}
}
