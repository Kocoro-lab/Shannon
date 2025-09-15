package activities

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/viper"
)

// WorkflowConfig contains configuration for cognitive workflows
type WorkflowConfig struct {
	// Exploratory workflow config
	ExploratoryMaxIterations      int     `json:"exploratory_max_iterations"`
	ExploratoryConfidenceThreshold float64 `json:"exploratory_confidence_threshold"`
	ExploratoryBranchFactor       int     `json:"exploratory_branch_factor"`
	ExploratoryMaxConcurrentAgents int    `json:"exploratory_max_concurrent_agents"`

	// React workflow config
	ReactMaxIterations     int `json:"react_max_iterations"`
	ReactObservationWindow int `json:"react_observation_window"`

	// Research workflow config
	ResearchDepth            int `json:"research_depth"`
	ResearchSourcesPerRound  int `json:"research_sources_per_round"`
	ResearchMinSources       int `json:"research_min_sources"`
	ResearchMaxConcurrentAgents int `json:"research_max_concurrent_agents"`

	// Scientific workflow config
	ScientificMaxHypotheses        int     `json:"scientific_max_hypotheses"`
	ScientificMaxIterations        int     `json:"scientific_max_iterations"`
	ScientificConfidenceThreshold  float64 `json:"scientific_confidence_threshold"`
	ScientificContradictionThreshold float64 `json:"scientific_contradiction_threshold"`

    // Reflection config
    ReflectionEnabled           bool     `json:"reflection_enabled"`
    ReflectionMaxRetries        int      `json:"reflection_max_retries"`
    ReflectionConfidenceThreshold float64 `json:"reflection_confidence_threshold"`
    ReflectionCriteria          []string `json:"reflection_criteria"`
    ReflectionTimeoutMs         int      `json:"reflection_timeout_ms"`

    // Router/DAG config
    SimpleThreshold     float64 `json:"simple_threshold"`
    MaxParallelAgents   int     `json:"max_parallel_agents"`

    // Approval config
    ApprovalEnabled              bool     `json:"approval_enabled"`
    ApprovalComplexityThreshold  float64  `json:"approval_complexity_threshold"`
    ApprovalDangerousTools       []string `json:"approval_dangerous_tools"`

    // Execution pattern config
    ParallelMaxConcurrency      int `json:"parallel_max_concurrency"`
    HybridDependencyTimeout     int `json:"hybrid_dependency_timeout_seconds"`
    SequentialPassResults       bool `json:"sequential_pass_results"`
    SequentialExtractNumeric    bool `json:"sequential_extract_numeric"`
}

// GetWorkflowConfig is an activity that returns workflow configuration
func GetWorkflowConfig(ctx context.Context) (*WorkflowConfig, error) {
	// Create a new viper instance to load features.yaml
	v := viper.New()

	// Determine config path
	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		// Try local development paths first
		if _, err := os.Stat("config/features.yaml"); err == nil {
			cfgPath = "config/features.yaml"
		} else if _, err := os.Stat("../../config/features.yaml"); err == nil {
			cfgPath = "../../config/features.yaml"
		} else {
			cfgPath = "/app/config/features.yaml" // Docker path
		}
	}

	v.SetConfigFile(cfgPath)
	// Try to read config, but don't fail if it doesn't exist - use defaults
	_ = v.ReadInConfig()

    config := &WorkflowConfig{
		// Exploratory defaults
		ExploratoryMaxIterations:       v.GetInt("cognitive_workflows.exploratory.max_iterations"),
		ExploratoryConfidenceThreshold: v.GetFloat64("cognitive_workflows.exploratory.confidence_threshold"),
		ExploratoryBranchFactor:        v.GetInt("cognitive_workflows.exploratory.branch_factor"),
		ExploratoryMaxConcurrentAgents: v.GetInt("cognitive_workflows.exploratory.max_concurrent_agents"),

		// React defaults
		ReactMaxIterations:     v.GetInt("cognitive_workflows.react.max_iterations"),
		ReactObservationWindow: v.GetInt("cognitive_workflows.react.observation_window"),

		// Research defaults
		ResearchDepth:               v.GetInt("cognitive_workflows.research.research_depth"),
		ResearchSourcesPerRound:     v.GetInt("cognitive_workflows.research.sources_per_round"),
		ResearchMinSources:          v.GetInt("cognitive_workflows.research.min_sources"),
		ResearchMaxConcurrentAgents: v.GetInt("cognitive_workflows.research.max_concurrent_agents"),

		// Scientific defaults
		ScientificMaxHypotheses:          v.GetInt("cognitive_workflows.scientific.max_hypotheses"),
		ScientificMaxIterations:          v.GetInt("cognitive_workflows.scientific.max_iterations"),
		ScientificConfidenceThreshold:    v.GetFloat64("cognitive_workflows.scientific.confidence_threshold"),
		ScientificContradictionThreshold: v.GetFloat64("cognitive_workflows.scientific.contradiction_threshold"),

        // Reflection defaults
        ReflectionEnabled:              v.GetBool("workflows.reflection.enabled"),
        ReflectionMaxRetries:           v.GetInt("workflows.reflection.max_retries"),
        ReflectionConfidenceThreshold: v.GetFloat64("workflows.reflection.confidence_threshold"),
        ReflectionCriteria:             v.GetStringSlice("workflows.reflection.criteria"),
        ReflectionTimeoutMs:            v.GetInt("workflows.reflection.timeout_ms"),

        // Router/DAG
        SimpleThreshold:   v.GetFloat64("workflows.dag.simple_threshold"),
        MaxParallelAgents: v.GetInt("workflows.dag.max_parallel_agents"),

        // Approval
        ApprovalEnabled:             v.GetBool("workflows.approval.enabled"),
        ApprovalComplexityThreshold: v.GetFloat64("workflows.approval.complexity_threshold"),
        ApprovalDangerousTools:      v.GetStringSlice("workflows.approval.dangerous_tools"),

        // Execution patterns
        ParallelMaxConcurrency:      v.GetInt("workflows.execution.parallel_max_concurrency"),
        HybridDependencyTimeout:     v.GetInt("workflows.execution.hybrid_dependency_timeout_seconds"),
        SequentialPassResults:       v.GetBool("workflows.execution.sequential_pass_results"),
        SequentialExtractNumeric:    v.GetBool("workflows.execution.sequential_extract_numeric"),
    }

	// Set defaults if not configured
	if config.ExploratoryMaxIterations == 0 {
		config.ExploratoryMaxIterations = 5
	}
	if config.ExploratoryConfidenceThreshold == 0 {
		config.ExploratoryConfidenceThreshold = 0.85
	}
	if config.ExploratoryBranchFactor == 0 {
		config.ExploratoryBranchFactor = 3
	}
	if config.ExploratoryMaxConcurrentAgents == 0 {
		config.ExploratoryMaxConcurrentAgents = 3
	}

	if config.ReactMaxIterations == 0 {
		config.ReactMaxIterations = 10
	}
	if config.ReactObservationWindow == 0 {
		config.ReactObservationWindow = 3
	}

	if config.ResearchDepth == 0 {
		config.ResearchDepth = 3
	}
	if config.ResearchSourcesPerRound == 0 {
		config.ResearchSourcesPerRound = 4
	}
	if config.ResearchMinSources == 0 {
		config.ResearchMinSources = 5
	}
	if config.ResearchMaxConcurrentAgents == 0 {
		config.ResearchMaxConcurrentAgents = 3
	}

	if config.ScientificMaxHypotheses == 0 {
		config.ScientificMaxHypotheses = 3
	}
	if config.ScientificMaxIterations == 0 {
		config.ScientificMaxIterations = 4
	}
	if config.ScientificConfidenceThreshold == 0 {
		config.ScientificConfidenceThreshold = 0.75
	}
	if config.ScientificContradictionThreshold == 0 {
		config.ScientificContradictionThreshold = 0.3
	}

	// Reflection defaults
	if config.ReflectionMaxRetries == 0 {
		config.ReflectionMaxRetries = 2
	}
	if config.ReflectionConfidenceThreshold == 0 {
		config.ReflectionConfidenceThreshold = 0.7
	}
	if len(config.ReflectionCriteria) == 0 {
		config.ReflectionCriteria = []string{"completeness", "accuracy", "relevance"}
	}
    if config.ReflectionTimeoutMs == 0 {
        config.ReflectionTimeoutMs = 5000
    }

    // Router/DAG defaults
    if config.SimpleThreshold == 0 {
        config.SimpleThreshold = 0.3
    }
    if config.MaxParallelAgents == 0 {
        config.MaxParallelAgents = 5
    }

    // Approval defaults
    // Disabled by default unless explicitly enabled
    if config.ApprovalComplexityThreshold == 0 {
        config.ApprovalComplexityThreshold = 0.8
    }
    if len(config.ApprovalDangerousTools) == 0 {
        config.ApprovalDangerousTools = []string{"file_system", "code_execution"}
    }

    // Execution pattern defaults
    if config.ParallelMaxConcurrency == 0 {
        config.ParallelMaxConcurrency = 5
    }
    if config.HybridDependencyTimeout == 0 {
        config.HybridDependencyTimeout = 360 // 6 minutes
    }
    // SequentialPassResults defaults to true if not explicitly set to false
    if !v.IsSet("workflows.execution.sequential_pass_results") {
        config.SequentialPassResults = true
    }
    // SequentialExtractNumeric defaults to true if not explicitly set to false
    if !v.IsSet("workflows.execution.sequential_extract_numeric") {
        config.SequentialExtractNumeric = true
    }

	return config, nil
}

// LoadWorkflowConfig loads configuration at workflow start
func LoadWorkflowConfig(ctx context.Context) (map[string]interface{}, error) {
	config, err := GetWorkflowConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load workflow config: %w", err)
	}

	// Convert to map for easier access
	return map[string]interface{}{
		"exploratory": map[string]interface{}{
			"max_iterations":        config.ExploratoryMaxIterations,
			"confidence_threshold":  config.ExploratoryConfidenceThreshold,
			"branch_factor":         config.ExploratoryBranchFactor,
			"max_concurrent_agents": config.ExploratoryMaxConcurrentAgents,
		},
		"react": map[string]interface{}{
			"max_iterations":      config.ReactMaxIterations,
			"observation_window":  config.ReactObservationWindow,
		},
		"research": map[string]interface{}{
			"research_depth":        config.ResearchDepth,
			"sources_per_round":     config.ResearchSourcesPerRound,
			"min_sources":           config.ResearchMinSources,
			"max_concurrent_agents": config.ResearchMaxConcurrentAgents,
		},
		"scientific": map[string]interface{}{
			"max_hypotheses":          config.ScientificMaxHypotheses,
			"max_iterations":          config.ScientificMaxIterations,
			"confidence_threshold":    config.ScientificConfidenceThreshold,
			"contradiction_threshold": config.ScientificContradictionThreshold,
		},
		"reflection": map[string]interface{}{
			"enabled":               config.ReflectionEnabled,
			"max_retries":          config.ReflectionMaxRetries,
			"confidence_threshold": config.ReflectionConfidenceThreshold,
			"criteria":             config.ReflectionCriteria,
			"timeout_ms":           config.ReflectionTimeoutMs,
		},
		"dag": map[string]interface{}{
			"simple_threshold":    config.SimpleThreshold,
			"max_parallel_agents": config.MaxParallelAgents,
		},
		"approval": map[string]interface{}{
			"enabled":               config.ApprovalEnabled,
			"complexity_threshold":  config.ApprovalComplexityThreshold,
			"dangerous_tools":       config.ApprovalDangerousTools,
		},
		"execution": map[string]interface{}{
			"parallel_max_concurrency":     config.ParallelMaxConcurrency,
			"hybrid_dependency_timeout":    config.HybridDependencyTimeout,
			"sequential_pass_results":      config.SequentialPassResults,
			"sequential_extract_numeric":   config.SequentialExtractNumeric,
		},
	}, nil
}
