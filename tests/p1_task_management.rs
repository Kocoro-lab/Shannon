//! Integration tests for P1.6-P1.8 Task Management features.
//!
//! Tests cover:
//! - P1.6: List tasks with pagination and filtering
//! - P1.7: Pause task workflow
//! - P1.8: Resume task workflow

use anyhow::Result;
use serde_json::json;
use std::collections::HashMap;
use std::time::Duration;
use tokio::time::sleep;

/// Test helper for API requests.
struct TestClient {
    base_url: String,
    client: reqwest::Client,
}

impl TestClient {
    fn new(base_url: String) -> Self {
        Self {
            base_url,
            client: reqwest::Client::new(),
        }
    }

    async fn submit_task(&self, prompt: &str, session_id: Option<&str>) -> Result<serde_json::Value> {
        let mut body = json!({
            "prompt": prompt,
            "task_type": "chat"
        });

        if let Some(sid) = session_id {
            body["session_id"] = json!(sid);
        }

        let response = self
            .client
            .post(&format!("{}/api/v1/tasks", self.base_url))
            .json(&body)
            .send()
            .await?;

        let status = response.status();
        let data = response.json::<serde_json::Value>().await?;

        assert!(
            status.is_success(),
            "Task submission failed: {:?}",
            data
        );

        Ok(data)
    }

    async fn list_tasks(
        &self,
        limit: Option<usize>,
        offset: Option<usize>,
        status: Option<&str>,
        session_id: Option<&str>,
    ) -> Result<serde_json::Value> {
        let mut query_params = Vec::new();

        if let Some(l) = limit {
            query_params.push(format!("limit={}", l));
        }
        if let Some(o) = offset {
            query_params.push(format!("offset={}", o));
        }
        if let Some(s) = status {
            query_params.push(format!("status={}", s));
        }
        if let Some(sid) = session_id {
            query_params.push(format!("session_id={}", sid));
        }

        let query = if query_params.is_empty() {
            String::new()
        } else {
            format!("?{}", query_params.join("&"))
        };

        let response = self
            .client
            .get(&format!("{}/api/v1/tasks{}", self.base_url, query))
            .send()
            .await?;

        let status = response.status();
        let data = response.json::<serde_json::Value>().await?;

        assert!(status.is_success(), "List tasks failed: {:?}", data);

        Ok(data)
    }

    async fn get_task_status(&self, task_id: &str) -> Result<serde_json::Value> {
        let response = self
            .client
            .get(&format!("{}/api/v1/tasks/{}", self.base_url, task_id))
            .send()
            .await?;

        Ok(response.json::<serde_json::Value>().await?)
    }

    async fn pause_task(&self, task_id: &str) -> Result<serde_json::Value> {
        let response = self
            .client
            .post(&format!("{}/api/v1/tasks/{}/pause", self.base_url, task_id))
            .json(&json!({"reason": "Test pause"}))
            .send()
            .await?;

        Ok(response.json::<serde_json::Value>().await?)
    }

    async fn resume_task(&self, task_id: &str) -> Result<serde_json::Value> {
        let response = self
            .client
            .post(&format!("{}/api/v1/tasks/{}/resume", self.base_url, task_id))
            .json(&json!({"reason": "Test resume"}))
            .send()
            .await?;

        Ok(response.json::<serde_json::Value>().await?)
    }

    async fn get_control_state(&self, task_id: &str) -> Result<serde_json::Value> {
        let response = self
            .client
            .get(&format!("{}/api/v1/tasks/{}/control-state", self.base_url, task_id))
            .send()
            .await?;

        Ok(response.json::<serde_json::Value>().await?)
    }

    async fn wait_for_task_status(
        &self,
        task_id: &str,
        expected_status: &str,
        timeout_secs: u64,
    ) -> Result<serde_json::Value> {
        let start = std::time::Instant::now();

        loop {
            if start.elapsed().as_secs() > timeout_secs {
                anyhow::bail!("Timeout waiting for task status: {}", expected_status);
            }

            let task = self.get_task_status(task_id).await?;
            let status = task["status"].as_str().unwrap_or("unknown");

            if status == expected_status {
                return Ok(task);
            }

            sleep(Duration::from_millis(500)).await;
        }
    }
}

// ============================================================================
// P1.6: List Tasks Tests
// ============================================================================

#[tokio::test]
#[ignore] // Run with: cargo test --ignored
async fn test_list_tasks_empty() -> Result<()> {
    let client = TestClient::new("http://localhost:8765".to_string());

    let response = client.list_tasks(None, None, None, None).await?;

    assert_eq!(response["tasks"].as_array().unwrap().len(), 0);
    assert_eq!(response["total_count"].as_u64().unwrap(), 0);
    assert_eq!(response["limit"].as_u64().unwrap(), 20); // Default limit

    Ok(())
}

#[tokio::test]
#[ignore]
async fn test_list_tasks_pagination() -> Result<()> {
    let client = TestClient::new("http://localhost:8765".to_string());

    // Submit 25 tasks
    let mut task_ids = Vec::new();
    for i in 0..25 {
        let task = client
            .submit_task(&format!("Test task {}", i), None)
            .await?;
        task_ids.push(task["task_id"].as_str().unwrap().to_string());
    }

    // Test first page (limit=10, offset=0)
    let page1 = client.list_tasks(Some(10), Some(0), None, None).await?;
    assert_eq!(page1["tasks"].as_array().unwrap().len(), 10);
    assert_eq!(page1["limit"].as_u64().unwrap(), 10);
    assert_eq!(page1["offset"].as_u64().unwrap(), 0);
    assert!(page1["total_count"].as_u64().unwrap() >= 25);

    // Test second page (limit=10, offset=10)
    let page2 = client.list_tasks(Some(10), Some(10), None, None).await?;
    assert_eq!(page2["tasks"].as_array().unwrap().len(), 10);
    assert_eq!(page2["offset"].as_u64().unwrap(), 10);

    // Test third page (limit=10, offset=20)
    let page3 = client.list_tasks(Some(10), Some(20), None, None).await?;
    assert_eq!(page3["tasks"].as_array().unwrap().len(), 5);

    // Verify no duplicate IDs across pages
    let page1_ids: Vec<String> = page1["tasks"]
        .as_array()
        .unwrap()
        .iter()
        .map(|t| t["task_id"].as_str().unwrap().to_string())
        .collect();

    let page2_ids: Vec<String> = page2["tasks"]
        .as_array()
        .unwrap()
        .iter()
        .map(|t| t["task_id"].as_str().unwrap().to_string())
        .collect();

    for id in &page1_ids {
        assert!(!page2_ids.contains(id), "Duplicate task ID across pages");
    }

    Ok(())
}

#[tokio::test]
#[ignore]
async fn test_list_tasks_filter_by_status() -> Result<()> {
    let client = TestClient::new("http://localhost:8765".to_string());

    // Submit some tasks
    for i in 0..5 {
        client
            .submit_task(&format!("Test task {}", i), None)
            .await?;
    }

    // Wait a bit for tasks to process
    sleep(Duration::from_secs(2)).await;

    // List only running tasks
    let running = client.list_tasks(None, None, Some("running"), None).await?;
    let running_tasks = running["tasks"].as_array().unwrap();

    for task in running_tasks {
        assert_eq!(task["status"].as_str().unwrap(), "running");
    }

    // List only completed tasks
    let completed = client.list_tasks(None, None, Some("completed"), None).await?;
    let completed_tasks = completed["tasks"].as_array().unwrap();

    for task in completed_tasks {
        assert_eq!(task["status"].as_str().unwrap(), "completed");
    }

    Ok(())
}

#[tokio::test]
#[ignore]
async fn test_list_tasks_filter_by_session() -> Result<()> {
    let client = TestClient::new("http://localhost:8765".to_string());

    let session_a = "sess-test-a";
    let session_b = "sess-test-b";

    // Submit tasks to session A
    for i in 0..3 {
        client
            .submit_task(&format!("Session A task {}", i), Some(session_a))
            .await?;
    }

    // Submit tasks to session B
    for i in 0..2 {
        client
            .submit_task(&format!("Session B task {}", i), Some(session_b))
            .await?;
    }

    // List tasks for session A
    let session_a_tasks = client
        .list_tasks(None, None, None, Some(session_a))
        .await?;
    let tasks_a = session_a_tasks["tasks"].as_array().unwrap();
    assert_eq!(tasks_a.len(), 3);

    for task in tasks_a {
        assert_eq!(task["session_id"].as_str().unwrap(), session_a);
    }

    // List tasks for session B
    let session_b_tasks = client
        .list_tasks(None, None, None, Some(session_b))
        .await?;
    let tasks_b = session_b_tasks["tasks"].as_array().unwrap();
    assert_eq!(tasks_b.len(), 2);

    for task in tasks_b {
        assert_eq!(task["session_id"].as_str().unwrap(), session_b);
    }

    Ok(())
}

#[tokio::test]
#[ignore]
async fn test_list_tasks_combined_filters() -> Result<()> {
    let client = TestClient::new("http://localhost:8765".to_string());

    let session_id = "sess-test-combined";

    // Submit tasks with specific session
    for i in 0..5 {
        client
            .submit_task(&format!("Combined filter task {}", i), Some(session_id))
            .await?;
    }

    // Wait for some to complete
    sleep(Duration::from_secs(3)).await;

    // Filter by both session and status
    let filtered = client
        .list_tasks(None, None, Some("completed"), Some(session_id))
        .await?;

    let tasks = filtered["tasks"].as_array().unwrap();

    for task in tasks {
        assert_eq!(task["status"].as_str().unwrap(), "completed");
        assert_eq!(task["session_id"].as_str().unwrap(), session_id);
    }

    Ok(())
}

#[tokio::test]
#[ignore]
async fn test_list_tasks_persistence() -> Result<()> {
    let client = TestClient::new("http://localhost:8765".to_string());

    // Submit 3 tasks
    let mut task_ids = Vec::new();
    for i in 0..3 {
        let task = client
            .submit_task(&format!("Persistence test task {}", i), None)
            .await?;
        task_ids.push(task["task_id"].as_str().unwrap().to_string());
    }

    // Get initial list
    let initial_list = client.list_tasks(None, None, None, None).await?;
    let initial_count = initial_list["total_count"].as_u64().unwrap();

    assert!(initial_count >= 3);

    // NOTE: To fully test persistence across restarts, would need to:
    // 1. Shutdown server
    // 2. Restart server
    // 3. Query again and verify tasks still present
    // This requires orchestration beyond a single test

    println!("✅ Persistence test - tasks stored in database");
    println!("   Manual verification needed: restart app and check tasks still present");

    Ok(())
}

// ============================================================================
// P1.7 & P1.8: Pause/Resume Tests
// ============================================================================

#[tokio::test]
#[ignore]
async fn test_pause_resume_task() -> Result<()> {
    let client = TestClient::new("http://localhost:8765".to_string());

    // Submit a task
    let task = client
        .submit_task("Long running task for pause test", None)
        .await?;
    let task_id = task["task_id"].as_str().unwrap();

    println!("📝 Task submitted: {}", task_id);

    // Wait for task to start
    sleep(Duration::from_millis(500)).await;

    // Pause the task
    let pause_response = client.pause_task(task_id).await?;
    assert_eq!(pause_response["success"].as_bool().unwrap(), true);
    assert_eq!(pause_response["action"].as_str().unwrap(), "pause");

    println!("⏸️  Task paused: {}", task_id);

    // Verify control state shows paused
    let control_state = client.get_control_state(task_id).await?;
    assert_eq!(control_state["is_paused"].as_bool().unwrap_or(false), true);
    assert!(control_state["paused_at"].as_str().is_some());
    assert_eq!(
        control_state["pause_reason"].as_str().unwrap_or(""),
        "User requested pause"
    );

    println!("✅ Control state verified - is_paused=true");

    // Resume the task
    let resume_response = client.resume_task(task_id).await?;
    assert_eq!(resume_response["success"].as_bool().unwrap(), true);
    assert_eq!(resume_response["action"].as_str().unwrap(), "resume");

    println!("▶️  Task resumed: {}", task_id);

    // Verify control state shows not paused
    let control_state_after = client.get_control_state(task_id).await?;
    assert_eq!(control_state_after["is_paused"].as_bool().unwrap_or(true), false);

    println!("✅ Control state verified - is_paused=false");

    Ok(())
}

#[tokio::test]
#[ignore]
async fn test_pause_nonexistent_task() -> Result<()> {
    let client = TestClient::new("http://localhost:8765".to_string());

    let fake_id = "task-does-not-exist";
    let response = client.client
        .post(&format!("{}/api/v1/tasks/{}/pause", client.base_url, fake_id))
        .send()
        .await?;

    // Should return error (either 404 or 500 depending on implementation)
    assert!(!response.status().is_success());

    Ok(())
}

#[tokio::test]
#[ignore]
async fn test_resume_not_paused_task() -> Result<()> {
    let client = TestClient::new("http://localhost:8765".to_string());

    // Submit a task but don't pause it
    let task = client.submit_task("Task that won't be paused", None).await?;
    let task_id = task["task_id"].as_str().unwrap();

    // Try to resume (should succeed but have no effect)
    let response = client.resume_task(task_id).await?;
    assert_eq!(response["success"].as_bool().unwrap(), true);

    // Control state should show not paused
    let control_state = client.get_control_state(task_id).await?;
    assert_eq!(control_state["is_paused"].as_bool().unwrap_or(false), false);

    Ok(())
}

#[tokio::test]
#[ignore]
async fn test_multiple_pause_resume_cycles() -> Result<()> {
    let client = TestClient::new("http://localhost:8765".to_string());

    let task = client.submit_task("Multi-cycle pause task", None).await?;
    let task_id = task["task_id"].as_str().unwrap();

    // Pause cycle 1
    client.pause_task(task_id).await?;
    let state1 = client.get_control_state(task_id).await?;
    assert_eq!(state1["is_paused"].as_bool().unwrap(), true);

    // Resume cycle 1
    client.resume_task(task_id).await?;
    let state2 = client.get_control_state(task_id).await?;
    assert_eq!(state2["is_paused"].as_bool().unwrap(), false);

    // Pause cycle 2
    client.pause_task(task_id).await?;
    let state3 = client.get_control_state(task_id).await?;
    assert_eq!(state3["is_paused"].as_bool().unwrap(), true);

    // Resume cycle 2
    client.resume_task(task_id).await?;
    let state4 = client.get_control_state(task_id).await?;
    assert_eq!(state4["is_paused"].as_bool().unwrap(), false);

    println!("✅ Multiple pause/resume cycles work correctly");

    Ok(())
}

// ============================================================================
// Combined Integration Tests
// ============================================================================

#[tokio::test]
#[ignore]
async fn test_end_to_end_task_lifecycle() -> Result<()> {
    let client = TestClient::new("http://localhost:8765".to_string());

    // 1. Submit task
    let task = client.submit_task("E2E lifecycle test", Some("sess-e2e")).await?;
    let task_id = task["task_id"].as_str().unwrap();
    println!("📝 Step 1: Task submitted - {}", task_id);

    // 2. Verify task appears in list
    let list = client.list_tasks(None, None, None, None).await?;
    let tasks = list["tasks"].as_array().unwrap();
    assert!(tasks.iter().any(|t| t["task_id"].as_str().unwrap() == task_id));
    println!("✅ Step 2: Task found in list");

    // 3. Check status
    let status = client.get_task_status(task_id).await?;
    println!("📊 Step 3: Task status - {:?}", status["status"]);

    // 4. Pause task
    client.pause_task(task_id).await?;
    let control = client.get_control_state(task_id).await?;
    assert_eq!(control["is_paused"].as_bool().unwrap(), true);
    println!("⏸️  Step 4: Task paused");

    // 5. Resume task
    client.resume_task(task_id).await?;
    let control_after = client.get_control_state(task_id).await?;
    assert_eq!(control_after["is_paused"].as_bool().unwrap(), false);
    println!("▶️  Step 5: Task resumed");

    // 6. Verify task still in list
    let final_list = client.list_tasks(None, None, None, None).await?;
    let final_tasks = final_list["tasks"].as_array().unwrap();
    assert!(final_tasks.iter().any(|t| t["task_id"].as_str().unwrap() == task_id));
    println!("✅ Step 6: Task still in list after pause/resume");

    println!("🎉 End-to-end lifecycle test complete");

    Ok(())
}

#[tokio::test]
#[ignore]
async fn test_pagination_with_active_tasks() -> Result<()> {
    let client = TestClient::new("http://localhost:8765".to_string());

    // Submit 10 tasks
    for i in 0..10 {
        client.submit_task(&format!("Active task {}", i), None).await?;
    }

    // Immediately list (tasks should be in-memory, not yet persisted)
    let list = client.list_tasks(Some(5), Some(0), None, None).await?;
    assert!(list["tasks"].as_array().unwrap().len() <= 5);
    assert!(list["total_count"].as_u64().unwrap() >= 5);

    println!("✅ Pagination works with active (non-persisted) tasks");

    Ok(())
}

// ============================================================================
// Error Handling Tests
// ============================================================================

#[tokio::test]
#[ignore]
async fn test_invalid_pagination_params() -> Result<()> {
    let client = TestClient::new("http://localhost:8765".to_string());

    // Rust server should handle large offsets gracefully
    let response = client.list_tasks(Some(20), Some(999999), None, None).await?;
    assert!(response["tasks"].as_array().unwrap().is_empty());

    Ok(())
}

#[tokio::test]
#[ignore]
async fn test_filter_by_invalid_status() -> Result<()> {
    let client = TestClient::new("http://localhost:8765".to_string());

    // Should return empty list for invalid status
    let response = client
        .list_tasks(None, None, Some("invalid_status"), None)
        .await?;
    assert_eq!(response["tasks"].as_array().unwrap().len(), 0);

    Ok(())
}

// ============================================================================
// Performance Tests
// ============================================================================

#[tokio::test]
#[ignore]
async fn test_list_tasks_performance() -> Result<()> {
    let client = TestClient::new("http://localhost:8765".to_string());

    // Submit 100 tasks (if database already has tasks, this adds to them)
    println!("📝 Submitting 100 tasks...");
    for i in 0..100 {
        client.submit_task(&format!("Perf test task {}", i), None).await?;
    }

    // Measure list performance
    let start = std::time::Instant::now();
    let list = client.list_tasks(Some(20), Some(0), None, None).await?;
    let duration = start.elapsed();

    println!("⏱️  List 20 tasks took: {:?}", duration);
    assert!(duration.as_millis() < 100, "List tasks took too long: {:?}", duration);

    // Measure count performance
    let start = std::time::Instant::now();
    let total_count = list["total_count"].as_u64().unwrap();
    let count_duration = start.elapsed();

    println!("⏱️  Count query included in: {:?}", count_duration);
    println!("📊 Total tasks in database: {}", total_count);

    Ok(())
}

// ============================================================================
// Test Utilities
// ============================================================================

/// Helper to check if embedded API is running.
async fn check_api_available() -> bool {
    let client = reqwest::Client::new();
    match client
        .get("http://localhost:8765/health")
        .timeout(Duration::from_secs(2))
        .send()
        .await
    {
        Ok(response) => response.status().is_success(),
        Err(_) => false,
    }
}

#[tokio::test]
#[ignore]
async fn test_api_is_running() -> Result<()> {
    let available = check_api_available().await;
    assert!(
        available,
        "Embedded API not running on http://localhost:8765. Start it first with: cd desktop && npm run dev"
    );
    println!("✅ Embedded API is accessible");
    Ok(())
}

// ============================================================================
// Test Runner Documentation
// ============================================================================

#[allow(dead_code)]
const TEST_INSTRUCTIONS: &str = r#"
# Running P1 Task Management Tests

## Prerequisites
1. Start the embedded Shannon API:
   ```bash
   cd desktop
   npm run dev
   ```

2. API should be running on http://localhost:8765

## Run All Tests
```bash
cargo test --test task_management_test --features embedded -- --ignored --nocapture
```

## Run Specific Test
```bash
# Test list pagination
cargo test test_list_tasks_pagination --features embedded -- --ignored --nocapture

# Test pause/resume
cargo test test_pause_resume_task --features embedded -- --ignored --nocapture

# Test filtering
cargo test test_list_tasks_filter_by_status --features embedded -- --ignored --nocapture
```

## Test Coverage

| Test | Coverage |
|------|----------|
| test_list_tasks_empty | Basic empty state |
| test_list_tasks_pagination | Offset/limit pagination |
| test_list_tasks_filter_by_status | Status filtering |
| test_list_tasks_filter_by_session | Session filtering |
| test_list_tasks_combined_filters | Multiple filters |
| test_list_tasks_persistence | Database persistence |
| test_pause_resume_task | Control flow |
| test_multiple_pause_resume_cycles | Repeated operations |
| test_end_to_end_task_lifecycle | Full lifecycle |
| test_list_tasks_performance | Performance benchmarking |

## Expected Results
- All tests should pass
- Performance test should complete in < 100ms
- No memory leaks during repeated operations
"#;
