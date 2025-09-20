package personas

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

// TestPersonasCore 测试personas系统的核心功能
func TestPersonasCore(t *testing.T) {
	// 创建临时配置文件
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "personas.yaml")

	configContent := `
personas:
  generalist:
    id: "generalist"
    description: "General-purpose assistant"
    keywords: ["general", "help", "assist"]
    max_tokens: 5000
    temperature: 0.7
    priority: 1
  researcher:
    id: "researcher"
    description: "Research expert"
    keywords: ["research", "search", "find", "investigate", "analyze"]
    max_tokens: 10000
    temperature: 0.3
    priority: 2
  coder:
    id: "coder"
    description: "Programming expert"
    keywords: ["code", "program", "debug", "implement", "develop"]
    max_tokens: 7000
    temperature: 0.2
    priority: 2

selection:
  method: "enhanced"
  complexity_threshold: 0.3
  max_concurrent_selections: 10
  cache_ttl: "1h"
  fallback_strategy: "generalist"
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	// 创建manager
	logger, _ := zap.NewDevelopment()
	manager, err := NewManager(configPath, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer manager.Close()

	ctx := context.Background()

	// 测试核心功能
	t.Run("低复杂度任务选择generalist", func(t *testing.T) {
		req := &SelectionRequest{
			Description:     "Simple help request",
			ComplexityScore: 0.2, // 低于阈值0.3
		}

		result, err := manager.SelectPersona(ctx, req)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if result.PersonaID != "generalist" {
			t.Errorf("Expected generalist for low complexity, got %s", result.PersonaID)
		}

		if result.Method != "complexity_threshold" {
			t.Errorf("Expected complexity_threshold method, got %s", result.Method)
		}
	})

	t.Run("研究任务选择researcher", func(t *testing.T) {
		req := &SelectionRequest{
			Description:     "I need to research market trends for AI startups",
			ComplexityScore: 0.8,
			TaskType:        "research",
		}

		result, err := manager.SelectPersona(ctx, req)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if result.PersonaID != "researcher" {
			t.Errorf("Expected researcher for research task, got %s", result.PersonaID)
		}
	})

	t.Run("编程任务选择coder", func(t *testing.T) {
		req := &SelectionRequest{
			Description:     "Help me implement a binary search algorithm",
			ComplexityScore: 0.7,
			TaskType:        "coding",
		}

		result, err := manager.SelectPersona(ctx, req)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if result.PersonaID != "coder" {
			t.Errorf("Expected coder for coding task, got %s", result.PersonaID)
		}
	})

	t.Run("用户偏好排除功能", func(t *testing.T) {
		req := &SelectionRequest{
			Description:     "Research some programming topics",
			ComplexityScore: 0.6,
			Preferences: &UserPreferences{
				ExcludedPersonas: []string{"researcher"},
			},
		}

		result, err := manager.SelectPersona(ctx, req)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if result.PersonaID == "researcher" {
			t.Error("Should not select excluded persona")
		}

		// 应该选择coder，因为researcher被排除了
		if result.PersonaID != "coder" {
			t.Errorf("Expected coder after excluding researcher, got %s", result.PersonaID)
		}
	})

	t.Run("无效请求处理", func(t *testing.T) {
		req := &SelectionRequest{
			Description:     "", // 空描述
			ComplexityScore: 0.5,
		}

		_, err := manager.SelectPersona(ctx, req)
		if err == nil {
			t.Error("Expected error for invalid request")
		}
	})

	t.Run("缓存功能测试", func(t *testing.T) {
		req := &SelectionRequest{
			Description:     "Help me with general assistance tasks",
			ComplexityScore: 0.5,
		}

		// 第一次调用
		result1, err := manager.SelectPersona(ctx, req)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if result1.CacheHit {
			t.Error("First call should not be cache hit")
		}

		// 第二次调用应该命中缓存
		result2, err := manager.SelectPersona(ctx, req)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !result2.CacheHit {
			t.Error("Second call should be cache hit")
		}

		// 结果应该一致
		if result1.PersonaID != result2.PersonaID {
			t.Errorf("Cache should return same persona: %s vs %s", result1.PersonaID, result2.PersonaID)
		}
	})
}

// TestKeywordMatching 测试关键词匹配算法
func TestKeywordMatching(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	matcher := NewKeywordMatcher(logger)

	tests := []struct {
		name        string
		description string
		keywords    []string
		expectScore bool // 是否期望有分数(>0)
	}{
		{
			name:        "精确匹配",
			description: "I need help with coding",
			keywords:    []string{"coding", "help"},
			expectScore: true,
		},
		{
			name:        "无匹配",
			description: "Weather forecast for tomorrow",
			keywords:    []string{"coding", "research"},
			expectScore: false,
		},
		{
			name:        "部分匹配",
			description: "Help me research some data",
			keywords:    []string{"research", "programming"},
			expectScore: true,
		},
		{
			name:        "否定词处理",
			description: "I don't want to do any coding",
			keywords:    []string{"coding"},
			expectScore: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := matcher.CalculateScore(tt.description, tt.keywords)

			if tt.expectScore && score <= 0 {
				t.Errorf("Expected positive score, got %f", score)
			}
			if !tt.expectScore && score > 0 {
				t.Errorf("Expected zero score, got %f", score)
			}

			// 分数应该在合理范围内
			if score < 0 || score > 1 {
				t.Errorf("Score should be between 0 and 1, got %f", score)
			}
		})
	}
}

// TestResultValidation 测试选择结果的有效性
func TestResultValidation(t *testing.T) {
	// 创建临时配置文件
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "personas.yaml")

	configContent := `
personas:
  generalist:
    id: "generalist"
    description: "General-purpose assistant"
    keywords: ["general", "help"]
    max_tokens: 5000
    temperature: 0.7

selection:
  complexity_threshold: 0.3
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	logger, _ := zap.NewDevelopment()
	manager, err := NewManager(configPath, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer manager.Close()

	req := &SelectionRequest{
		Description:     "Test request",
		ComplexityScore: 0.5,
	}

	result, err := manager.SelectPersona(context.Background(), req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// 验证结果的基本有效性
	if result.PersonaID == "" {
		t.Error("PersonaID should not be empty")
	}

	if result.Confidence < 0 || result.Confidence > 1 {
		t.Errorf("Confidence should be between 0 and 1, got %f", result.Confidence)
	}

	if result.SelectionTime <= 0 {
		t.Error("SelectionTime should be positive")
	}

	if result.Method == "" {
		t.Error("Method should not be empty")
	}
}
