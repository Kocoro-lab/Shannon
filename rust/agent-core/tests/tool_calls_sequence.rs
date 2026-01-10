use shannon_agent_core::grpc_server::proto::agent::agent_service_server::AgentService;
use shannon_agent_core::grpc_server::{proto, AgentServiceImpl};
use tonic::Request;

// Check if Python service is available for integration tests
async fn is_python_service_available() -> bool {
    let base =
        std::env::var("LLM_SERVICE_URL").unwrap_or_else(|_| "http://localhost:8000".to_string());
    let url = format!("{}/health", base);
    let client = reqwest::Client::new();
    match client.get(&url).send().await {
        Ok(resp) => resp.status().is_success(),
        Err(_) => false,
    }
}

fn str_val(s: &str) -> prost_types::Value {
    prost_types::Value {
        kind: Some(prost_types::value::Kind::StringValue(s.to_string())),
    }
}

fn tool_call(tool: &str, expr: &str) -> prost_types::Value {
    use prost_types::value::Kind;
    let mut pfields = std::collections::BTreeMap::new();
    pfields.insert("expression".to_string(), str_val(expr));
    let params = prost_types::Struct { fields: pfields };

    let mut fields = std::collections::BTreeMap::new();
    fields.insert("tool".to_string(), str_val(tool));
    fields.insert(
        "parameters".to_string(),
        prost_types::Value {
            kind: Some(Kind::StructValue(params)),
        },
    );
    prost_types::Value {
        kind: Some(Kind::StructValue(prost_types::Struct { fields })),
    }
}

#[tokio::test]
async fn test_multi_tool_sequence_mixed_success() {
    // In restricted CI/sandbox environments, avoid any network or system
    // configuration access that some HTTP clients may perform. Only run
    // this test when explicitly enabled.
    if std::env::var("RUN_NETWORK_TESTS").unwrap_or_default() != "1" {
        eprintln!("Skipping: network-dependent test disabled (set RUN_NETWORK_TESTS=1 to enable)");
        return;
    }
    if !is_python_service_available().await {
        eprintln!("Skipping: Python LLM service not available");
        return;
    }

    // Set the LLM service URL for the test
    // SAFETY: This is a test environment variable
    unsafe {
        std::env::set_var("LLM_SERVICE_URL", "http://localhost:8000");
    }

    let svc = AgentServiceImpl::new().expect("failed to create AgentServiceImpl");

    // Build tool_calls: success (2+2), failure (1/0), success (5*3)
    let tool_calls = prost_types::ListValue {
        values: vec![
            tool_call("calculator", "2 + 2"),
            tool_call("calculator", "1 / 0"),
            tool_call("calculator", "5 * 3"),
        ],
    };

    let context = prost_types::Struct {
        fields: std::collections::BTreeMap::from([(
            "tool_calls".to_string(),
            prost_types::Value {
                kind: Some(prost_types::value::Kind::ListValue(tool_calls)),
            },
        )]),
    };

    let req = proto::agent::ExecuteTaskRequest {
        metadata: None,
        query: "calculator pipeline".to_string(),
        context: Some(context),
        mode: proto::common::ExecutionMode::Standard as i32,
        available_tools: vec!["calculator".to_string()],
        config: None,
        session_context: None,
    };

    eprintln!("Executing task with 3 calculator tool calls");
    let resp = svc
        .execute_task(Request::new(req))
        .await
        .expect("execute_task");
    let body = resp.into_inner();

    eprintln!("Response status: {}", body.status);
    eprintln!("Number of tool results: {}", body.tool_results.len());
    for (i, tr) in body.tool_results.iter().enumerate() {
        eprintln!(
            "Tool result {}: status={}, error={:?}",
            i, tr.status, tr.error_message
        );
    }

    // Order preserved and parameters round-trip
    assert_eq!(body.tool_calls.len(), 3, "should have 3 tool_calls");
    for (i, tc) in body.tool_calls.iter().enumerate() {
        assert_eq!(tc.name, "calculator");
        let params = tc.parameters.as_ref().expect("params");
        let expr = params.fields.get("expression").expect("expression");
        if let Some(prost_types::value::Kind::StringValue(s)) = &expr.kind {
            match i {
                0 => assert_eq!(s, "2 + 2"),
                1 => assert_eq!(s, "1 / 0"),
                2 => assert_eq!(s, "5 * 3"),
                _ => unreachable!(),
            }
        } else {
            panic!("expression should be string");
        }
    }

    // Results and error handling
    assert_eq!(body.tool_results.len(), 3, "should have 3 tool_results");
    assert!(body.metrics.as_ref().map(|m| m.latency_ms).unwrap_or(0) >= 0);

    // Expect overall status error due to division by zero
    assert_eq!(body.status, proto::common::StatusCode::Error as i32);
    assert!(!body.error_message.is_empty(), "should aggregate errors");

    // Check individual results semantics
    let r0 = &body.tool_results[0];
    assert_eq!(r0.status, proto::common::StatusCode::Ok as i32);
    let r1 = &body.tool_results[1];
    assert_eq!(r1.status, proto::common::StatusCode::Error as i32);
    assert!(!r1.error_message.is_empty());
    let r2 = &body.tool_results[2];
    assert_eq!(r2.status, proto::common::StatusCode::Ok as i32);
}
