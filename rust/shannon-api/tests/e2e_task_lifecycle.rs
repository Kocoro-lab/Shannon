//! End-to-end test for complete task lifecycle.
//!
//! This test validates:
//! - Task submission with context parameters
//! - Task status polling
//! - Task completion and result retrieval
//! - Usage tracking and metadata
//! - Context parameter parsing and usage

#[cfg(all(test, feature = "embedded"))]
mod e2e_task_lifecycle_tests {
    use reqwest::Client;
    use serde_json::{json, Value};
    use std::time::Duration;
    use tokio::time::sleep;

    const BASE_URL: &str = "http://localhost:8765/api/v1";
    const HEALTH_URL: &str = "http://localhost:8765/health";

    /// Helper to wait for API to be ready.
    async fn wait_for_api() -> Result<(), Box<dyn std::error::Error>> {
        let client = Client::new();
        for _ in 0..30 {
            if let Ok(resp) = client.get(HEALTH_URL).send().await {
                if resp.status().is_success() {
                    return Ok(());
                }
            }
            sleep(Duration::from_millis(100)).await;
        }
        Err("API did not become healthy in time".into())
    }

    #[tokio::test]
    async fn test_task_submission_and_completion() -> Result<(), Box<dyn std::error::Error>> {
        wait_for_api().await?;

        let client = Client::new();

        // Submit a simple task
        let response = client
            .post(format!("{BASE_URL}/tasks"))
            .json(&json!({
                "prompt": "What is 2 + 2?",
                "context": {
                    "model_tier": "basic",
                    "temperature": 0.7
                }
            }))
            .send()
            .await?;

        assert!(response.status().is_success(), "Task submission failed");

        let task_result: Value = response.json().await?;
        let task_id = task_result["task_id"].as_str().expect("task_id not found");

        println!("✅ Task submitted: {task_id}");

        // Poll for completion (with timeout)
        let mut attempts = 0;
        let max_attempts = 60; // 30 seconds max
        let mut final_status = String::new();

        while attempts < max_attempts {
            let status_resp = client
                .get(format!("{BASE_URL}/tasks/{task_id}"))
                .send()
                .await?;

            assert!(status_resp.status().is_success(), "Status check failed");

            let status_data: Value = status_resp.json().await?;
            final_status = status_data["status"]
                .as_str()
                .unwrap_or("unknown")
                .to_string();

            println!("📊 Status check {attempts}: {final_status}");

            if final_status == "completed" || final_status == "failed" {
                break;
            }

            sleep(Duration::from_millis(500)).await;
            attempts += 1;
        }

        // Verify task completed
        assert_eq!(
            final_status, "completed",
            "Task did not complete successfully"
        );

        println!("✅ Task completed successfully");

        Ok(())
    }

    #[tokio::test]
    async fn test_task_with_context_parameters() -> Result<(), Box<dyn std::error::Error>> {
        wait_for_api().await?;

        let client = Client::new();

        // Submit task with full context parameters
        let response = client
            .post(format!("{BASE_URL}/tasks"))
            .json(&json!({
                "prompt": "Explain machine learning briefly",
                "context": {
                    "role_preset": "teacher",
                    "model_tier": "premium",
                    "temperature": 0.5,
                    "max_tokens": 100,
                    "research_strategy": "quick",
                    "enable_citations": true
                }
            }))
            .send()
            .await?;

        assert!(
            response.status().is_success(),
            "Task submission with context failed"
        );

        let task_result: Value = response.json().await?;
        assert!(task_result["task_id"].is_string());

        println!("✅ Task with context parameters submitted");

        Ok(())
    }

    #[tokio::test]
    async fn test_task_output_retrieval() -> Result<(), Box<dyn std::error::Error>> {
        wait_for_api().await?;

        let client = Client::new();

        // Submit task
        let response = client
            .post(format!("{BASE_URL}/tasks"))
            .json(&json!({
                "prompt": "Say hello",
                "context": {
                    "model_tier": "basic"
                }
            }))
            .send()
            .await?;

        let task_result: Value = response.json().await?;
        let task_id = task_result["task_id"].as_str().unwrap();

        // Wait for completion
        let mut completed = false;
        for _ in 0..60 {
            let status_resp = client
                .get(format!("{BASE_URL}/tasks/{task_id}"))
                .send()
                .await?;

            let status_data: Value = status_resp.json().await?;
            if status_data["status"].as_str() == Some("completed") {
                completed = true;
                break;
            }

            sleep(Duration::from_millis(500)).await;
        }

        assert!(completed, "Task did not complete");

        // Get task output
        let output_resp = client
            .get(format!("{BASE_URL}/tasks/{task_id}/output"))
            .send()
            .await?;

        assert!(output_resp.status().is_success(), "Output retrieval failed");

        let output_data: Value = output_resp.json().await?;
        assert!(output_data["output"].is_string());
        assert!(!output_data["output"].as_str().unwrap().is_empty());

        println!("✅ Task output retrieved successfully");

        Ok(())
    }

    #[tokio::test]
    async fn test_task_progress_tracking() -> Result<(), Box<dyn std::error::Error>> {
        wait_for_api().await?;

        let client = Client::new();

        // Submit task
        let response = client
            .post(format!("{BASE_URL}/tasks"))
            .json(&json!({
                "prompt": "Research topic",
                "context": {
                    "research_strategy": "balanced"
                }
            }))
            .send()
            .await?;

        let task_result: Value = response.json().await?;
        let task_id = task_result["task_id"].as_str().unwrap();

        // Check progress endpoint
        let progress_resp = client
            .get(format!("{BASE_URL}/tasks/{task_id}/progress"))
            .send()
            .await?;

        assert!(progress_resp.status().is_success(), "Progress check failed");

        let progress_data: Value = progress_resp.json().await?;
        assert!(progress_data["task_id"].is_string());
        assert!(progress_data["status"].is_string());

        println!("✅ Task progress tracked successfully");

        Ok(())
    }

    #[tokio::test]
    async fn test_task_usage_metadata() -> Result<(), Box<dyn std::error::Error>> {
        wait_for_api().await?;

        let client = Client::new();

        // Submit task
        let response = client
            .post(format!("{BASE_URL}/tasks"))
            .json(&json!({
                "prompt": "Short response test",
                "context": {
                    "model_tier": "basic",
                    "max_tokens": 50
                }
            }))
            .send()
            .await?;

        let task_result: Value = response.json().await?;
        let task_id = task_result["task_id"].as_str().unwrap();

        // Wait for completion
        for _ in 0..60 {
            let status_resp = client
                .get(format!("{BASE_URL}/tasks/{task_id}"))
                .send()
                .await?;

            let status_data: Value = status_resp.json().await?;

            if status_data["status"].as_str() == Some("completed") {
                // Verify usage metadata exists
                if let Some(result) = status_data.get("result") {
                    assert!(result.get("usage").is_some(), "Usage data missing");

                    let usage = &result["usage"];
                    assert!(usage["total_tokens"].is_number());
                    assert!(usage["prompt_tokens"].is_number());
                    assert!(usage["completion_tokens"].is_number());

                    println!("✅ Usage metadata validated");
                }
                break;
            }

            sleep(Duration::from_millis(500)).await;
        }

        Ok(())
    }

    #[tokio::test]
    async fn test_task_list_with_pagination() -> Result<(), Box<dyn std::error::Error>> {
        wait_for_api().await?;

        let client = Client::new();

        // Submit multiple tasks
        for i in 0..3 {
            client
                .post(format!("{BASE_URL}/tasks"))
                .json(&json!({
                    "prompt": format!("Test task {i}"),
                    "context": {
                        "model_tier": "basic"
                    }
                }))
                .send()
                .await?;
        }

        // List tasks with pagination
        let list_resp = client
            .get(format!("{BASE_URL}/tasks?limit=2&offset=0"))
            .send()
            .await?;

        assert!(list_resp.status().is_success(), "Task list failed");

        let list_data: Value = list_resp.json().await?;
        assert!(list_data["tasks"].is_array());
        assert!(list_data["total"].is_number());
        assert_eq!(list_data["limit"], 2);
        assert_eq!(list_data["offset"], 0);

        println!("✅ Task list with pagination validated");

        Ok(())
    }

    #[tokio::test]
    async fn test_task_with_model_override() -> Result<(), Box<dyn std::error::Error>> {
        wait_for_api().await?;

        let client = Client::new();

        // Submit task with specific model
        let response = client
            .post(format!("{BASE_URL}/tasks"))
            .json(&json!({
                "prompt": "Hello",
                "context": {
                    "model": "gpt-4-turbo",
                    "provider": "openai"
                }
            }))
            .send()
            .await?;

        assert!(
            response.status().is_success(),
            "Task with model override failed"
        );

        println!("✅ Task with model override submitted");

        Ok(())
    }
}
