"""
Unit tests for OpenAPI parser with $ref resolution.
"""

import pytest
from llm_service.tools.openapi_parser import (
    OpenAPIParseError,
    resolve_ref,
    resolve_refs_in_schema,
    extract_parameters,
    validate_spec,
)


class TestRefResolution:
    """Test $ref resolution functionality."""

    def test_simple_ref_resolution(self):
        """Test basic $ref pointer resolution."""
        spec = {
            "components": {
                "schemas": {
                    "Pet": {
                        "type": "object",
                        "properties": {
                            "name": {"type": "string"}
                        }
                    }
                }
            }
        }

        result = resolve_ref(spec, "#/components/schemas/Pet")
        assert result["type"] == "object"
        assert "name" in result["properties"]

    def test_nested_ref_resolution(self):
        """Test resolving nested $ref in schema."""
        spec = {
            "components": {
                "schemas": {
                    "Address": {
                        "type": "object",
                        "properties": {
                            "street": {"type": "string"}
                        }
                    },
                    "Person": {
                        "type": "object",
                        "properties": {
                            "name": {"type": "string"},
                            "address": {"$ref": "#/components/schemas/Address"}
                        }
                    }
                }
            }
        }

        schema = {"$ref": "#/components/schemas/Person"}
        result = resolve_refs_in_schema(schema, spec)

        assert result["type"] == "object"
        assert "name" in result["properties"]
        assert "address" in result["properties"]
        assert result["properties"]["address"]["type"] == "object"
        assert "street" in result["properties"]["address"]["properties"]

    def test_circular_ref_detection(self):
        """Test circular reference detection."""
        spec = {
            "components": {
                "schemas": {
                    "Node": {
                        "type": "object",
                        "properties": {
                            "value": {"type": "string"},
                            "next": {"$ref": "#/components/schemas/Node"}
                        }
                    }
                }
            }
        }

        schema = {"$ref": "#/components/schemas/Node"}

        # Should raise error due to circular reference
        with pytest.raises(OpenAPIParseError, match="Circular reference"):
            resolve_refs_in_schema(schema, spec)

    def test_invalid_ref_path(self):
        """Test handling of invalid $ref path."""
        spec = {"components": {"schemas": {}}}

        with pytest.raises(OpenAPIParseError, match="Failed to resolve"):
            resolve_ref(spec, "#/components/schemas/NonExistent")

    def test_external_ref_rejection(self):
        """Test rejection of external $ref (not supported in MVP)."""
        spec = {}

        with pytest.raises(OpenAPIParseError, match="Only local references supported"):
            resolve_ref(spec, "https://example.com/schema.json#/Pet")

    def test_rfc6901_escaping(self):
        """Test RFC 6901 JSON Pointer escaping (~0 for ~, ~1 for /)."""
        spec = {
            "components": {
                "schemas": {
                    "Foo~Bar": {"type": "string"},
                    "Foo/Bar": {"type": "number"}
                }
            }
        }

        # ~0 should decode to ~
        result1 = resolve_ref(spec, "#/components/schemas/Foo~0Bar")
        assert result1["type"] == "string"

        # ~1 should decode to /
        result2 = resolve_ref(spec, "#/components/schemas/Foo~1Bar")
        assert result2["type"] == "number"

    def test_sibling_properties_with_ref(self):
        """Test that sibling properties alongside $ref are merged (OpenAPI 3.1)."""
        spec = {
            "components": {
                "schemas": {
                    "Pet": {
                        "type": "object",
                        "properties": {
                            "name": {"type": "string"}
                        }
                    }
                }
            }
        }

        schema = {
            "$ref": "#/components/schemas/Pet",
            "description": "A pet with custom description"
        }

        result = resolve_refs_in_schema(schema, spec)

        assert result["type"] == "object"
        assert "name" in result["properties"]
        assert result["description"] == "A pet with custom description"

    def test_array_with_ref_items(self):
        """Test resolving $ref in array items."""
        spec = {
            "components": {
                "schemas": {
                    "Tag": {
                        "type": "object",
                        "properties": {
                            "name": {"type": "string"}
                        }
                    }
                }
            }
        }

        schema = {
            "type": "array",
            "items": {"$ref": "#/components/schemas/Tag"}
        }

        result = resolve_refs_in_schema(schema, spec)

        assert result["type"] == "array"
        assert result["items"]["type"] == "object"
        assert "name" in result["items"]["properties"]


class TestParameterExtraction:
    """Test parameter extraction with $ref resolution."""

    def test_parameter_ref_resolution(self):
        """Test extracting parameters that use $ref."""
        spec = {
            "components": {
                "parameters": {
                    "PageParam": {
                        "name": "page",
                        "in": "query",
                        "schema": {"type": "integer", "minimum": 1}
                    }
                }
            }
        }

        operation = {
            "parameters": [
                {"$ref": "#/components/parameters/PageParam"}
            ]
        }

        params = extract_parameters(operation, spec)

        assert len(params) == 1
        assert params[0]["name"] == "page"
        assert params[0]["location"] == "query"
        assert params[0]["type"] == "integer"

    def test_schema_ref_in_parameter(self):
        """Test parameter with schema containing $ref."""
        spec = {
            "components": {
                "schemas": {
                    "Status": {
                        "type": "string",
                        "enum": ["active", "inactive"]
                    }
                }
            }
        }

        operation = {
            "parameters": [
                {
                    "name": "status",
                    "in": "query",
                    "schema": {"$ref": "#/components/schemas/Status"}
                }
            ]
        }

        params = extract_parameters(operation, spec)

        assert len(params) == 1
        assert params[0]["name"] == "status"
        assert params[0]["enum"] == ["active", "inactive"]

    def test_invalid_parameter_ref_skipped(self):
        """Test that invalid parameter $ref is gracefully skipped."""
        spec = {"components": {"parameters": {}}}

        operation = {
            "parameters": [
                {"$ref": "#/components/parameters/NonExistent"},
                {"name": "valid", "in": "query", "schema": {"type": "string"}}
            ]
        }

        params = extract_parameters(operation, spec)

        # Should skip invalid ref but include valid parameter
        assert len(params) == 1
        assert params[0]["name"] == "valid"


class TestSpecValidation:
    """Test OpenAPI spec validation."""

    def test_valid_spec_30(self):
        """Test validation of valid OpenAPI 3.0 spec."""
        spec = {
            "openapi": "3.0.0",
            "info": {"title": "Test API", "version": "1.0.0"},
            "paths": {
                "/test": {"get": {"summary": "Test endpoint"}}
            }
        }

        # Should not raise
        validate_spec(spec)

    def test_valid_spec_31(self):
        """Test validation of valid OpenAPI 3.1 spec."""
        spec = {
            "openapi": "3.1.0",
            "info": {"title": "Test API", "version": "1.0.0"},
            "paths": {
                "/test": {"get": {"summary": "Test endpoint"}}
            }
        }

        # Should not raise
        validate_spec(spec)

    def test_invalid_version(self):
        """Test rejection of unsupported OpenAPI version."""
        spec = {
            "openapi": "2.0",
            "info": {"title": "Test API", "version": "1.0.0"}
        }

        with pytest.raises(OpenAPIParseError, match="Unsupported OpenAPI version.*Only 3"):
            validate_spec(spec)

    def test_missing_required_fields(self):
        """Test rejection of spec missing required fields."""
        spec = {"openapi": "3.0.0"}

        with pytest.raises(OpenAPIParseError):
            validate_spec(spec)
