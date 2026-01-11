//! Phase 4 (P2) Integration Tests
//!
//! Tests for enhanced features including context parameters, event streaming,
//! control signals, and response enhancements.

use shannon_api::domain::tasks::TaskContext;
use shannon_api::events::NormalizedEvent;
use shannon_api::workflow::control::{
    emit_workflow_cancelled, emit_workflow_cancelling, emit_workflow_paused,
    emit_workflow_pausing, emit_workflow_resumed,
};
use shannon_api::workflow::tracking::{TaskResultWithMetadata, UsageTracker};
use shannon_api::workflow::task::{TaskResult, TaskState, TokenUsage};
use tokio::sync::broadcast;

/// T130: Context parameters parsing and validation test.
#[test]
fn test_context_parameters_parsing() {
    // Test basic context creation
    let ctx = TaskContext::new()
        .with_role("researcher")
        .with_system_prompt("You are a helpful assistant")
        .with_model_tier("large")
        .with_model_override("gpt-4o")
        .with_provider_override("openai")
        .with_research_strategy("deep");

    assert_eq!(ctx.role, Some("researcher".to_string()));
    assert_eq!(ctx.system_prompt, Some("You are a helpful assistant".to_string()));
    assert_eq!(ctx.model_tier, Some("large".to_string()));
    assert_eq!(ctx.model_override, Some("gpt-4o".to_string()));
    assert_eq!(ctx.provider_override, Some("openai".to_string()));
    assert_eq!(ctx.research_strategy, Some("deep".to_string()));

    // Test effective getters
    assert_eq!(ctx.effective_tier(), "large");
    assert_eq!(ctx.effective_research_strategy(), "deep");

    // Test validation
    assert!(ctx.validate().is_ok());
}

/// T130: Context parameters validation tests.
#[test]
fn test_context_validation() {
    // Invalid model tier
    let mut ctx = TaskContext::new();
    ctx.model_tier = Some("invalid".to_string());
    assert!(ctx.validate().is_err());

    // Invalid research strategy
    let mut ctx = TaskContext::new();
    ctx.research_strategy = Some("invalid".to_string());
    assert!(ctx.validate().is_err());

    // Invalid max_concurrent_agents
    let mut ctx = TaskContext::new();
    ctx.max_concurrent_agents = Some(25); // > 20
    assert!(ctx.validate().is_err());

    // Invalid compression ratio
    let mut ctx = TaskContext::new();
    ctx.compression_trigger_ratio = Some(1.5); // > 1.0
    assert!(ctx.validate().is_err());

    // Valid context with research parameters
    let mut ctx = TaskContext::new();
    ctx.force_research = true;
    ctx.research_strategy = Some("academic".to_string());
    ctx.max_concurrent_agents = Some(15);
    ctx.enable_verification = true;
    ctx.iterative_research_enabled = true;
    ctx.iterative_max_iterations = Some(3);
    assert!(ctx.validate().is_ok());
}

/// T131: Event streaming integration test.
#[tokio::test]
async fn test_event_streaming() {
    let (tx, mut rx) = broadcast::channel::<NormalizedEvent>(16);

    // Test LLM events (T114-T116)
    let llm_prompt = NormalizedEvent::LlmPrompt {
        prompt: "Test prompt".to_string(),
        model: "gpt-4o".to_string(),
        provider: Some("openai".to_string()),
    };
    tx.send(llm_prompt.clone()).unwrap();

    let llm_delta = NormalizedEvent::MessageDelta {
        content: "Hello".to_string(),
        role: Some("assistant".to_string()),
    };
    tx.send(llm_delta.clone()).unwrap();

    let llm_complete = NormalizedEvent::MessageComplete {
        content: "Hello, how can I help you?".to_string(),
        role: "assistant".to_string(),
        finish_reason: Some("stop".to_string()),
    };
    tx.send(llm_complete.clone()).unwrap();

    // Test tool events (T117-T119)
    let tool_invoked = NormalizedEvent::ToolCallComplete {
        id: "call_123".to_string(),
        name: "search_web".to_string(),
        arguments: r#"{"query":"test"}"#.to_string(),
    };
    tx.send(tool_invoked.clone()).unwrap();

    let tool_result = NormalizedEvent::ToolResult {
        tool_call_id: "call_123".to_string(),
        name: "search_web".to_string(),
        content: "Search results...".to_string(),
        success: true,
    };
    tx.send(tool_result.clone()).unwrap();

    // Verify events received
    assert!(matches!(rx.recv().await.unwrap(), NormalizedEvent::LlmPrompt { .. }));
    assert!(matches!(rx.recv().await.unwrap(), NormalizedEvent::MessageDelta { .. }));
    assert!(matches!(rx.recv().await.unwrap(), NormalizedEvent::MessageComplete { .. }));
    assert!(matches!(rx.recv().await.unwrap(), NormalizedEvent::ToolCallComplete { .. }));
    assert!(matches!(rx.recv().await.unwrap(), NormalizedEvent::ToolResult { .. }));
}

/// T133: Control signal events test.
#[tokio::test]
async fn test_control_signal_events() {
    let (tx, mut rx) = broadcast::channel::<NormalizedEvent>(16);
    let workflow_id = "wf-test-123";

    // Test WORKFLOW_PAUSING (T120)
    emit_workflow_pausing(&tx, workflow_id, Some("User requested")).unwrap();
    match rx.recv().await.unwrap() {
        NormalizedEvent::WorkflowPausing {
            workflow_id: wf_id,
            reason,
        } => {
            assert_eq!(wf_id, workflow_id);
            assert_eq!(reason, Some("User requested".to_string()));
        }
        _ => panic!("Expected WorkflowPausing event"),
    }

    // Test WORKFLOW_PAUSED (T121)
    emit_workflow_paused(&tx, workflow_id, Some("checkpoint-1")).unwrap();
    match rx.recv().await.unwrap() {
        NormalizedEvent::WorkflowPaused {
            workflow_id: wf_id,
            checkpoint_id,
        } => {
            assert_eq!(wf_id, workflow_id);
            assert_eq!(checkpoint_id, Some("checkpoint-1".to_string()));
        }
        _ => panic!("Expected WorkflowPaused event"),
    }

    // Test WORKFLOW_RESUMED (T122)
    emit_workflow_resumed(&tx, workflow_id, Some("checkpoint-1"), Some("User resumed")).unwrap();
    match rx.recv().await.unwrap() {
        NormalizedEvent::WorkflowResumed {
            workflow_id: wf_id,
            checkpoint_id,
            reason,
        } => {
            assert_eq!(wf_id, workflow_id);
            assert_eq!(checkpoint_id, Some("checkpoint-1".to_string()));
            assert_eq!(reason, Some("User resumed".to_string()));
        }
        _ => panic!("Expected WorkflowResumed event"),
    }

    // Test WORKFLOW_CANCELLING (T123)
    emit_workflow_cancelling(&tx, workflow_id, Some("Timeout")).unwrap();
    match rx.recv().await.unwrap() {
        NormalizedEvent::WorkflowCancelling {
            workflow_id: wf_id,
            reason,
        } => {
            assert_eq!(wf_id, workflow_id);
            assert_eq!(reason, Some("Timeout".to_string()));
        }
        _ => panic!("Expected WorkflowCancelling event"),
    }

    // Test WORKFLOW_CANCELLED (T124)
    emit_workflow_cancelled(&tx, workflow_id, Some("checkpoint-final")).unwrap();
    match rx.recv().await.unwrap() {
        NormalizedEvent::WorkflowCancelled {
            workflow_id: wf_id,
            final_checkpoint,
        } => {
            assert_eq!(wf_id, workflow_id);
            assert_eq!(final_checkpoint, Some("checkpoint-final".to_string()));
        }
        _ => panic!("Expected WorkflowCancelled event"),
    }
}

/// T134: Response enhancement test with usage metadata.
#[test]
fn test_response_enhancements() {
    // Create usage tracker
    let mut tracker = UsageTracker::new();

    // Record multiple model calls
    tracker.record_call("gpt-4o", "openai", 100, 50, 0.005);
    tracker.record_call("claude-3-5-sonnet", "anthropic", 200, 100, 0.012);
    tracker.record_call("gpt-4o", "openai", 50, 25, 0.002);

    // Test T125: model_used field
    assert_eq!(tracker.primary_model(), Some("gpt-4o"));

    // Test T126: provider field
    assert_eq!(tracker.primary_provider(), Some("openai"));

    // Test T127: TokenUsage with all counters
    let total = tracker.total_usage();
    assert_eq!(total.prompt_tokens, 350);
    assert_eq!(total.completion_tokens, 175);
    assert_eq!(total.total_tokens, 525);
    assert!((total.cost_usd - 0.019).abs() < 0.001);

    // Test T128: model_breakdown array
    let breakdown = tracker.model_breakdown();
    assert_eq!(breakdown.len(), 2);

    // First should be gpt-4o (2 calls)
    assert_eq!(breakdown[0].model, "gpt-4o");
    assert_eq!(breakdown[0].provider, "openai");
    assert_eq!(breakdown[0].call_count, 2);
    assert_eq!(breakdown[0].prompt_tokens, 150);
    assert_eq!(breakdown[0].completion_tokens, 75);
    assert_eq!(breakdown[0].total_tokens, 225);

    // Second should be claude (1 call)
    assert_eq!(breakdown[1].model, "claude-3-5-sonnet");
    assert_eq!(breakdown[1].provider, "anthropic");
    assert_eq!(breakdown[1].call_count, 1);

    // Test T129: Integration with TaskResult
    let basic_result = TaskResult {
        task_id: "task-123".to_string(),
        state: TaskState::Completed,
        content: Some("Test result".to_string()),
        data: None,
        error: None,
        token_usage: None,
        duration_ms: 5000,
        sources: vec![],
    };

    let enhanced_result = TaskResultWithMetadata::from_result_and_tracker(basic_result, &tracker);

    assert_eq!(enhanced_result.task_id, "task-123");
    assert_eq!(enhanced_result.model_used, Some("gpt-4o".to_string()));
    assert_eq!(enhanced_result.provider, Some("openai".to_string()));
    assert!(enhanced_result.token_usage.is_some());
    assert!(enhanced_result.model_breakdown.is_some());
    assert_eq!(enhanced_result.model_breakdown.as_ref().unwrap().len(), 2);
}

/// Test serialization of enhanced task result.
#[test]
fn test_task_result_serialization() {
    let mut tracker = UsageTracker::new();
    tracker.record_call("gpt-4o", "openai", 100, 50, 0.005);

    let basic_result = TaskResult {
        task_id: "task-456".to_string(),
        state: TaskState::Completed,
        content: Some("Success".to_string()),
        data: None,
        error: None,
        token_usage: Some(TokenUsage {
            prompt_tokens: 100,
            completion_tokens: 50,
            total_tokens: 150,
            cost_usd: 0.005,
        }),
        duration_ms: 2000,
        sources: vec![],
    };

    let enhanced = TaskResultWithMetadata::from_result_and_tracker(basic_result, &tracker);

    // Serialize to JSON
    let json = serde_json::to_string(&enhanced).unwrap();
    assert!(json.contains("task-456"));
    assert!(json.contains("gpt-4o"));
    assert!(json.contains("openai"));
    assert!(json.contains("model_breakdown"));

    // Deserialize back
    let deserialized: TaskResultWithMetadata = serde_json::from_str(&json).unwrap();
    assert_eq!(deserialized.task_id, "task-456");
    assert_eq!(deserialized.model_used, Some("gpt-4o".to_string()));
}

/// Test usage tracker with no calls.
#[test]
fn test_empty_usage_tracker() {
    let tracker = UsageTracker::new();

    assert_eq!(tracker.primary_model(), None);
    assert_eq!(tracker.primary_provider(), None);
    assert!(!tracker.has_usage());
    assert_eq!(tracker.model_count(), 0);

    let total = tracker.total_usage();
    assert_eq!(total.prompt_tokens, 0);
    assert_eq!(total.completion_tokens, 0);
    assert_eq!(total.total_tokens, 0);
    assert_eq!(total.cost_usd, 0.0);

    let breakdown = tracker.model_breakdown();
    assert!(breakdown.is_empty());
}

/// Test context parameters with research configuration.
#[test]
fn test_research_context_parameters() {
    let mut ctx = TaskContext::new();
    ctx.force_research = true;
    ctx.research_strategy = Some("academic".to_string());
    ctx.max_concurrent_agents = Some(20);
    ctx.enable_verification = true;
    ctx.iterative_research_enabled = true;
    ctx.iterative_max_iterations = Some(5);
    ctx.enable_fact_extraction = true;
    ctx.enable_citations = true;

    // Validate
    assert!(ctx.validate().is_ok());

    // Check effective values
    assert_eq!(ctx.effective_research_strategy(), "academic");
    assert!(ctx.force_research);
    assert!(ctx.enable_verification);
    assert!(ctx.iterative_research_enabled);
}

/// Test context window management parameters.
#[test]
fn test_context_window_parameters() {
    let mut ctx = TaskContext::new();
    ctx.history_window_size = Some(100);
    ctx.primers_count = Some(5);
    ctx.recents_count = Some(10);
    ctx.compression_trigger_ratio = Some(0.8);
    ctx.compression_target_ratio = Some(0.5);

    // Validate
    assert!(ctx.validate().is_ok());

    assert_eq!(ctx.history_window_size, Some(100));
    assert_eq!(ctx.primers_count, Some(5));
    assert_eq!(ctx.recents_count, Some(10));
    assert_eq!(ctx.compression_trigger_ratio, Some(0.8));
    assert_eq!(ctx.compression_target_ratio, Some(0.5));
}
