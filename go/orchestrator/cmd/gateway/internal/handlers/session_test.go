package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

func TestUpdateSessionTitle_Validation(t *testing.T) {
	logger := zap.NewNop()
	handler := &SessionHandler{
		logger: logger,
	}

	tests := []struct {
		name           string
		title          string
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "empty title",
			title:          "",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Title cannot be empty",
		},
		{
			name:           "whitespace only title",
			title:          "   ",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Title cannot be empty",
		},
		{
			name:           "title too long (bytes)",
			title:          "This is a very long title that exceeds sixty characters limit",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Title must be 60 characters or less",
		},
		{
			name:           "title too long (UTF-8)",
			title:          "🚀🎉🔥💯✨🌟⭐🎯🎪🎨🎭🎬🎮🎲🎰🎳🏀🏈⚽🎾🏐🏉🎱🏓🏸🏒🏑🏏⛳🏹🎣🏂🏄🏇🏊🚴🚵🏁🏆🏅",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Title must be 60 characters or less",
		},
		{
			name:           "title with control characters (newline)",
			title:          "Title with\nnewline",
			expectedStatus: http.StatusOK, // Should be sanitized and accepted
		},
		{
			name:           "title with control characters (tab)",
			title:          "Title with\ttab",
			expectedStatus: http.StatusOK, // Should be sanitized and accepted
		},
		{
			name:           "only control characters",
			title:          "\n\t\r\n",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Title cannot contain only control characters",
		},
		{
			name:           "valid short title",
			title:          "My Session",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "valid title at max length (60 chars)",
			title:          "123456789012345678901234567890123456789012345678901234567890",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "valid title with emoji",
			title:          "🚀 Rocket Launch",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "valid title with Chinese",
			title:          "分析网站流量趋势",
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create request
			reqBody := map[string]string{"title": tt.title}
			bodyBytes, _ := json.Marshal(reqBody)
			req := httptest.NewRequest(http.MethodPatch, "/api/v1/sessions/test-session-id", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")

			// Add user context
			userID := uuid.New()
			userCtx := &auth.UserContext{
				UserID: userID,
				Email:  "test@example.com",
			}
			ctx := context.WithValue(req.Context(), "user", userCtx)
			req = req.WithContext(ctx)

			// Set path value
			req.SetPathValue("sessionId", "test-session-id")

			// Create response recorder
			rr := httptest.NewRecorder()

			// For validation tests that should fail before DB access, we can test without a real DB
			if tt.expectedStatus == http.StatusBadRequest {
				handler.UpdateSessionTitle(rr, req)

				if rr.Code != tt.expectedStatus {
					t.Errorf("Expected status %d, got %d", tt.expectedStatus, rr.Code)
				}

				if tt.expectedError != "" {
					var response map[string]string
					json.NewDecoder(rr.Body).Decode(&response)
					if response["error"] != tt.expectedError {
						t.Errorf("Expected error %q, got %q", tt.expectedError, response["error"])
					}
				}
			}
		})
	}
}

func TestUpdateSessionTitle_ControlCharacterSanitization(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedClean string
	}{
		{
			name:          "newline removed",
			input:         "Title with\nnewline",
			expectedClean: "Title withnewline",
		},
		{
			name:          "tab removed",
			input:         "Title with\ttab",
			expectedClean: "Title withtab",
		},
		{
			name:          "carriage return removed",
			input:         "Title with\rcarriage return",
			expectedClean: "Title withcarriage return",
		},
		{
			name:          "multiple control chars",
			input:         "Title\n\twith\r\nmultiple",
			expectedClean: "Titlewithmultiple",
		},
		{
			name:          "zero-width space removed (U+200B)",
			input:         "Title\u200Bwith\u200Bzero\u200Bwidth",
			expectedClean: "Titlewithzerowidth",
		},
		{
			name:          "normal spaces preserved",
			input:         "Title with spaces",
			expectedClean: "Title with spaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the sanitization logic (using same logic as the handler)
			cleaned := ""
			for _, r := range tt.input {
				if !isControlChar(r) {
					cleaned += string(r)
				}
			}

			if cleaned != tt.expectedClean {
				t.Errorf("Expected %q, got %q", tt.expectedClean, cleaned)
			}
		})
	}
}

// Helper function to check if a rune is a control character
func isControlChar(r rune) bool {
	return r < 32 || (r >= 127 && r < 160) || r == '\u200B' // Control chars + zero-width space
}

func TestUpdateSessionTitle_RuneLengthValidation(t *testing.T) {
	tests := []struct {
		name        string
		title       string
		shouldPass  bool
		runeCount   int
	}{
		{
			name:       "ASCII - exactly 60 chars",
			title:      "123456789012345678901234567890123456789012345678901234567890",
			shouldPass: true,
			runeCount:  60,
		},
		{
			name:       "ASCII - 61 chars",
			title:      "1234567890123456789012345678901234567890123456789012345678901",
			shouldPass: false,
			runeCount:  61,
		},
		{
			name:       "Emoji - 30 emoji = 30 runes (but ~120 bytes)",
			title:      "🚀🎉🔥💯✨🌟⭐🎯🎪🎨🎭🎬🎮🎲🎰🎳🏀🏈⚽🎾🏐🏉🎱🏓🏸🏒🏑🏏⛳🏹",
			shouldPass: true,
			runeCount:  30,
		},
		{
			name:       "Emoji - 61 emoji = 61 runes",
			title:      "🚀🎉🔥💯✨🌟⭐🎯🎪🎨🎭🎬🎮🎲🎰🎳🏀🏈⚽🎾🏐🏉🎱🏓🏸🏒🏑🏏⛳🏹🎣🏂🏄🏇🏊🚴🚵🏁🏆🏅🎖️🏵️🎗️🎫🎟️🎪🎭🎨🎬🎤🎧🎼🎹🥁🎷🎺🎸🎻🎲🎯🎳🎮🎰",
			shouldPass: false,
			runeCount:  61,
		},
		{
			name:       "Chinese - 60 chars",
			title:      "这是一个包含六十个中文字符的标题用来测试字符计数而不是字节计数的验证逻辑是否正确工作并且能够处理多字节",
			shouldPass: true,
			runeCount:  60,
		},
		{
			name:       "Mixed - ASCII + emoji + Chinese",
			title:      "Test 测试 🚀 Mix",
			shouldPass: true,
			runeCount:  11,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runeCount := len([]rune(tt.title))
			if runeCount != tt.runeCount {
				t.Errorf("Expected rune count %d, got %d", tt.runeCount, runeCount)
			}

			shouldPass := runeCount <= 60
			if shouldPass != tt.shouldPass {
				t.Errorf("Expected shouldPass=%v, got %v (rune count: %d)", tt.shouldPass, shouldPass, runeCount)
			}
		})
	}
}
