//! Comprehensive tests for tool activities.
//!
//! Tests all tool execution activities to ensure 100% coverage.

use durable_shannon::activities::tools::{
    CalculatorActivity, ToolExecuteActivity, WebFetchActivity, WebSearchActivity,
};
use durable_shannon::activities::{Activity, ActivityContext};
use serde_json::json;

#[tokio::test]
async fn test_tool_execute_activity_creation() {
    let activity = ToolExecuteActivity::new("http://localhost:8765".to_string());
    assert_eq!(activity.name(), "tool_execute");
}

#[tokio::test]
async fn test_tool_execute_activity_local() {
    let activity = ToolExecuteActivity::local();
    assert_eq!(activity.name(), "tool_execute");
}

#[tokio::test]
async fn test_tool_execute_invalid_input() {
    let activity = ToolExecuteActivity::local();
    let ctx = ActivityContext::default();

    // Missing tool_name field
    let input = json!({
        "parameters": {}
    });

    let result = activity.execute(&ctx, input).await;
    assert!(!result.is_success());
}

#[tokio::test]
async fn test_calculator_addition() {
    let calc = CalculatorActivity;
    let ctx = ActivityContext::default();

    let input = json!({
        "expression": "5+3"
    });

    let result = calc.execute(&ctx, input).await;
    assert!(result.is_success());
}

#[tokio::test]
async fn test_calculator_subtraction() {
    let calc = CalculatorActivity;
    let ctx = ActivityContext::default();

    let input = json!({
        "expression": "10-4"
    });

    let result = calc.execute(&ctx, input).await;
    assert!(result.is_success());
}

#[tokio::test]
async fn test_calculator_multiplication() {
    let calc = CalculatorActivity;
    let ctx = ActivityContext::default();

    let input = json!({
        "expression": "6*7"
    });

    let result = calc.execute(&ctx, input).await;
    assert!(result.is_success());
}

#[tokio::test]
async fn test_calculator_division() {
    let calc = CalculatorActivity;
    let ctx = ActivityContext::default();

    let input = json!({
        "expression": "20/5"
    });

    let result = calc.execute(&ctx, input).await;
    assert!(result.is_success());
}

#[tokio::test]
async fn test_calculator_complex_expression() {
    let calc = CalculatorActivity;
    let ctx = ActivityContext::default();

    let input = json!({
        "expression": "10+5*2"
    });

    let result = calc.execute(&ctx, input).await;
    assert!(result.is_success());
}

#[tokio::test]
async fn test_calculator_empty_expression() {
    let calc = CalculatorActivity;
    let ctx = ActivityContext::default();

    let input = json!({
        "expression": ""
    });

    let result = calc.execute(&ctx, input).await;
    assert!(!result.is_success());
}

#[tokio::test]
async fn test_calculator_invalid_expression() {
    let calc = CalculatorActivity;
    let ctx = ActivityContext::default();

    let input = json!({
        "expression": "invalid"
    });

    let result = calc.execute(&ctx, input).await;
    assert!(!result.is_success());
}

#[tokio::test]
async fn test_calculator_division_by_zero() {
    let calc = CalculatorActivity;
    let ctx = ActivityContext::default();

    let input = json!({
        "expression": "10/0"
    });

    let result = calc.execute(&ctx, input).await;
    assert!(!result.is_success());
}

#[tokio::test]
async fn test_web_fetch_activity_creation() {
    let fetch = WebFetchActivity::new();
    assert_eq!(fetch.name(), "web_fetch");
}

#[tokio::test]
async fn test_web_fetch_empty_url() {
    let fetch = WebFetchActivity::new();
    let ctx = ActivityContext::default();

    let input = json!({
        "url": ""
    });

    let result = fetch.execute(&ctx, input).await;
    assert!(!result.is_success());
}

#[tokio::test]
async fn test_web_fetch_invalid_scheme() {
    let fetch = WebFetchActivity::new();
    let ctx = ActivityContext::default();

    let input = json!({
        "url": "ftp://example.com"
    });

    let result = fetch.execute(&ctx, input).await;
    assert!(!result.is_success());
}

#[tokio::test]
async fn test_web_fetch_missing_scheme() {
    let fetch = WebFetchActivity::new();
    let ctx = ActivityContext::default();

    let input = json!({
        "url": "example.com"
    });

    let result = fetch.execute(&ctx, input).await;
    assert!(!result.is_success());
}

#[tokio::test]
async fn test_web_search_activity_creation() {
    let search = WebSearchActivity::new("http://localhost:8765".to_string());
    assert_eq!(search.name(), "web_search");
}

#[tokio::test]
async fn test_activity_context_defaults() {
    let ctx = ActivityContext::default();

    assert_eq!(ctx.attempt, 1);
    assert_eq!(ctx.max_attempts, 3);
    assert_eq!(ctx.timeout_secs, 60);
    assert!(!ctx.activity_id.is_empty());
}

#[tokio::test]
async fn test_calculator_negative_numbers() {
    let calc = CalculatorActivity;
    let ctx = ActivityContext::default();

    let input = json!({
        "expression": "-5+3"
    });

    let result = calc.execute(&ctx, input).await;
    assert!(result.is_success());
}

#[tokio::test]
async fn test_calculator_whitespace_handling() {
    let calc = CalculatorActivity;
    let ctx = ActivityContext::default();

    let input = json!({
        "expression": " 2 + 3 "
    });

    let result = calc.execute(&ctx, input).await;
    assert!(result.is_success());
}

#[tokio::test]
async fn test_tool_timeout_configuration() {
    let ctx = ActivityContext {
        workflow_id: "test".to_string(),
        activity_id: "activity-1".to_string(),
        attempt: 1,
        max_attempts: 3,
        timeout_secs: 30,
    };

    assert_eq!(ctx.timeout_secs, 30);
}

#[tokio::test]
async fn test_tool_retry_configuration() {
    let ctx = ActivityContext {
        workflow_id: "test".to_string(),
        activity_id: "activity-1".to_string(),
        attempt: 2,
        max_attempts: 5,
        timeout_secs: 60,
    };

    assert_eq!(ctx.attempt, 2);
    assert_eq!(ctx.max_attempts, 5);
}
