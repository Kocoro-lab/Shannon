# Changelog

All notable changes to the Shannon Python SDK will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0a2] - 2025-01-07

### Fixed
- Added missing `wait()` method to both `AsyncShannonClient` and `ShannonClient` classes
- Fixed CLI error handling to show clean error messages instead of Python stack traces
- Fixed `TaskHandle` client reference in sync wrapper to use sync client for convenience methods

### Verified
- Context overrides including `system_prompt` parameter
- Template support (`template_name`, `template_version`, `disable_ai`)
- Custom labels for workflow routing and priority

## [0.1.0a1] - 2025-01-06

### Added
- Initial alpha release of Shannon Python SDK
- Support for task submission, status checking, and cancellation
- Streaming support (gRPC and SSE with auto-fallback)
- Session management for multi-turn conversations
- Approval workflow support
- Template-based task execution
- Custom labels and context overrides
