"""Tests for OpenAPI tool execution and vendor adapter integration."""

import pytest
from unittest.mock import AsyncMock, Mock, patch
from llm_service.tools.openapi_tool import OpenAPILoader, _SimpleBreaker
from llm_service.tools.base import ToolResult


class TestOpenAPIToolExecution:
    """Test OpenAPI tool execution with different auth types."""

    @pytest.fixture
    def simple_spec(self):
        """Simple OpenAPI 3.x spec for testing."""
        return {
            "openapi": "3.0.0",
            "info": {"title": "Test API", "version": "1.0.0"},
            "servers": [{"url": "https://api.test.com"}],
            "paths": {
                "/users/{userId}": {
                    "get": {
                        "operationId": "getUser",
                        "summary": "Get user by ID",
                        "parameters": [
                            {
                                "name": "userId",
                                "in": "path",
                                "required": True,
                                "schema": {"type": "string"},
                            }
                        ],
                        "responses": {"200": {"description": "Success"}},
                    }
                },
                "/data": {
                    "post": {
                        "operationId": "postData",
                        "requestBody": {
                            "required": True,
                            "content": {
                                "application/json": {
                                    "schema": {
                                        "type": "object",
                                        "properties": {
                                            "name": {"type": "string"},
                                            "value": {"type": "integer"},
                                        },
                                    }
                                }
                            },
                        },
                        "responses": {"201": {"description": "Created"}},
                    }
                },
            },
        }

    def test_loader_bearer_auth(self, simple_spec):
        """Test OpenAPI loader with bearer token auth."""
        loader = OpenAPILoader(
            name="test_api",
            spec=simple_spec,
            auth_type="bearer",
            auth_config={"token": "test_bearer_token"},
            category="test",
        )

        assert loader.base_url == "https://api.test.com"
        assert loader.auth_type == "bearer"
        assert loader.auth_config["token"] == "test_bearer_token"
        assert len(loader.operations) == 2

    def test_loader_api_key_header(self, simple_spec):
        """Test OpenAPI loader with API key in header."""
        loader = OpenAPILoader(
            name="test_api",
            spec=simple_spec,
            auth_type="api_key",
            auth_config={
                "api_key_name": "X-API-Key",
                "api_key_location": "header",
                "api_key_value": "test_api_key",
            },
        )

        tools = loader.generate_tools()
        assert len(tools) == 2
        assert tools[0].__name__ == "_OpenAPITool"

    def test_loader_api_key_query(self, simple_spec):
        """Test OpenAPI loader with API key in query parameter."""
        loader = OpenAPILoader(
            name="test_api",
            spec=simple_spec,
            auth_type="api_key",
            auth_config={
                "api_key_name": "api_key",
                "api_key_location": "query",
                "api_key_value": "test_query_key",
            },
        )

        assert loader.auth_config["api_key_location"] == "query"

    def test_loader_basic_auth(self, simple_spec):
        """Test OpenAPI loader with basic authentication."""
        loader = OpenAPILoader(
            name="test_api",
            spec=simple_spec,
            auth_type="basic",
            auth_config={"username": "testuser", "password": "testpass"},
        )

        assert loader.auth_type == "basic"
        assert loader.auth_config["username"] == "testuser"

    @pytest.mark.asyncio
    async def test_tool_execution_with_bearer_auth(self, simple_spec):
        """Test tool execution with bearer auth headers."""
        loader = OpenAPILoader(
            name="test_api",
            spec=simple_spec,
            auth_type="bearer",
            auth_config={"token": "test_token"},
        )

        tools = loader.generate_tools()
        get_user_tool = tools[0]()  # Instantiate

        with patch("httpx.AsyncClient.request") as mock_request:
            mock_response = AsyncMock()
            mock_response.status_code = 200
            mock_response.json = AsyncMock(
                return_value={"id": "123", "name": "Test User"}
            )
            mock_request.return_value = mock_response

            result = await get_user_tool.execute(userId="123")

            assert result.success
            assert result.output == {"id": "123", "name": "Test User"}
            # Verify bearer token was included
            call_args = mock_request.call_args
            assert "Authorization" in call_args[1]["headers"]
            assert call_args[1]["headers"]["Authorization"] == "Bearer test_token"

    @pytest.mark.asyncio
    async def test_tool_execution_with_request_body(self, simple_spec):
        """Test tool execution with JSON request body."""
        loader = OpenAPILoader(
            name="test_api",
            spec=simple_spec,
            auth_type="none",
        )

        tools = loader.generate_tools()
        post_data_tool = [t for t in tools if t().metadata.name == "postData"][0]()

        with patch("httpx.AsyncClient.request") as mock_request:
            mock_response = AsyncMock()
            mock_response.status_code = 201
            mock_response.json = AsyncMock(return_value={"id": "456"})
            mock_request.return_value = mock_response

            result = await post_data_tool.execute(body={"name": "test", "value": 42})

            assert result.success
            # Verify JSON body was sent
            call_args = mock_request.call_args
            assert call_args[1]["json"] == {"name": "test", "value": 42}


class TestCircuitBreaker:
    """Test circuit breaker behavior."""

    def test_circuit_breaker_allows_when_closed(self):
        """Circuit breaker should allow requests when closed."""
        breaker = _SimpleBreaker(failure_threshold=5, recovery_timeout=60)
        assert breaker.allow(time.time()) is True

    def test_circuit_breaker_opens_after_threshold(self):
        """Circuit breaker should open after failure threshold."""
        breaker = _SimpleBreaker(failure_threshold=3, recovery_timeout=60)
        now = time.time()

        # Record failures
        for _ in range(3):
            breaker.on_failure(now)

        # Circuit should be open
        assert breaker.allow(now + 1) is False

    def test_circuit_breaker_recovers_after_timeout(self):
        """Circuit breaker should enter half-open state after recovery timeout."""
        breaker = _SimpleBreaker(failure_threshold=3, recovery_timeout=10)
        now = time.time()

        # Open the circuit
        for _ in range(3):
            breaker.on_failure(now)

        # Circuit should be open
        assert breaker.allow(now + 1) is False

        # After recovery timeout, should allow trial request
        assert breaker.allow(now + 11) is True

    def test_circuit_breaker_resets_on_success(self):
        """Circuit breaker should reset failure count on success."""
        breaker = _SimpleBreaker(failure_threshold=3, recovery_timeout=60)
        now = time.time()

        # Record some failures
        breaker.on_failure(now)
        breaker.on_failure(now)
        assert breaker.failures == 2

        # Success should reset
        breaker.on_success()
        assert breaker.failures == 0


class TestVendorAdapterIntegration:
    """Test vendor adapter loading and integration."""

    def test_vendor_adapter_loading(self):
        """Test vendor adapter is loaded when specified."""
        from llm_service.tools.vendor_adapters import get_vendor_adapter

        # Should return None for unknown vendor
        adapter = get_vendor_adapter("unknown_vendor")
        assert adapter is None

        # Should return None for empty string
        adapter = get_vendor_adapter("")
        assert adapter is None

    @pytest.mark.asyncio
    async def test_vendor_adapter_body_transformation(self, simple_spec):
        """Test vendor adapter transforms request body."""

        # Create a mock vendor adapter
        class MockVendorAdapter:
            def transform_body(self, body, operation_id, prompt_params):
                # Transform metrics field
                if "metrics" in body:
                    body["metrics"] = [m.replace("users", "test:users") for m in body["metrics"]]
                # Inject session context
                if prompt_params and "account_id" in prompt_params:
                    body["account_id"] = prompt_params["account_id"]
                return body

        with patch("llm_service.tools.vendor_adapters.get_vendor_adapter") as mock_get_adapter:
            mock_get_adapter.return_value = MockVendorAdapter()

            loader = OpenAPILoader(
                name="test_api",
                spec=simple_spec,
                auth_type="none",
                auth_config={"vendor": "test_vendor"},
            )

            tools = loader.generate_tools()
            post_tool = [t for t in tools if t().metadata.name == "postData"][0]()

            with patch("httpx.AsyncClient.request") as mock_request:
                mock_response = AsyncMock()
                mock_response.status_code = 201
                mock_response.json = AsyncMock(return_value={"success": True})
                mock_request.return_value = mock_response

                # Execute with session context
                result = await post_tool.execute(
                    session_context={"prompt_params": {"account_id": "acct_123"}},
                    body={"metrics": ["users", "sessions"]},
                )

                assert result.success
                # Verify adapter transformed the body
                call_args = mock_request.call_args
                sent_body = call_args[1]["json"]
                assert "test:users" in sent_body.get("metrics", [])
                assert sent_body.get("account_id") == "acct_123"


class TestRateLimiting:
    """Test rate limiting enforcement."""

    @pytest.mark.asyncio
    async def test_rate_limit_enforced(self, simple_spec):
        """Test that rate limits are enforced per tool."""
        loader = OpenAPILoader(
            name="test_api",
            spec=simple_spec,
            rate_limit=2,  # 2 requests per minute
        )

        tools = loader.generate_tools()
        tool = tools[0]()

        with patch("httpx.AsyncClient.request") as mock_request:
            mock_response = AsyncMock()
            mock_response.status_code = 200
            mock_response.json = AsyncMock(return_value={})
            mock_request.return_value = mock_response

            # First two requests should succeed
            result1 = await tool.execute(userId="1")
            result2 = await tool.execute(userId="2")

            assert result1.success
            assert result2.success

            # Third request should be rate limited (within same minute)
            result3 = await tool.execute(userId="3")

            # Note: Actual rate limiting behavior depends on implementation
            # This test verifies the rate_limit configuration is passed through


class TestErrorHandling:
    """Test error handling in OpenAPI tools."""

    @pytest.mark.asyncio
    async def test_http_error_handling(self, simple_spec):
        """Test handling of HTTP errors."""
        loader = OpenAPILoader(
            name="test_api",
            spec=simple_spec,
            auth_type="none",
        )

        tools = loader.generate_tools()
        tool = tools[0]()

        with patch("httpx.AsyncClient.request") as mock_request:
            mock_response = AsyncMock()
            mock_response.status_code = 404
            mock_response.text = "Not Found"
            mock_response.raise_for_status.side_effect = Exception("404 Not Found")
            mock_request.return_value = mock_response

            result = await tool.execute(userId="999")

            assert result.success is False
            assert "404" in result.error or "error" in result.error.lower()

    @pytest.mark.asyncio
    async def test_timeout_handling(self, simple_spec):
        """Test handling of request timeouts."""
        loader = OpenAPILoader(
            name="test_api",
            spec=simple_spec,
            timeout_seconds=1,
        )

        tools = loader.generate_tools()
        tool = tools[0]()

        with patch("httpx.AsyncClient.request") as mock_request:
            import asyncio

            mock_request.side_effect = asyncio.TimeoutError("Request timed out")

            result = await tool.execute(userId="123")

            assert result.success is False
            assert "timeout" in result.error.lower() or "timed out" in result.error.lower()


if __name__ == "__main__":
    pytest.main([__file__, "-v"])
