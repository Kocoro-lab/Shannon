"""Vendor adapters for domain-specific API transformations.

This module provides TWO separate adapter registries for different tool types:

1. get_vendor_adapter() - For OpenAPI-based tools
   - Purpose: Transform request/response bodies for OpenAPI specs
   - Example: PTEngine analytics API
   - Security: Whitelist-based (must manually add vendors to ALLOWED_VENDORS)
   - Usage: OpenAPITool uses these adapters to modify API calls

2. get_ga4_adapter() - For GA4 (Google Analytics 4) tools
   - Purpose: Transform dimension filters for GA4 queries
   - Example: Vendor-specific filtering rules, domain restrictions
   - Security: Convention-based dynamic import (safe because GA4 adapters only transform filters)
   - Usage: GA4RunReportTool and GA4 client use these adapters

WHY TWO SEPARATE SYSTEMS?

- OpenAPI adapters are generic and can modify arbitrary API requests, so we use
  a strict whitelist for security

- GA4 adapters follow a predictable pattern (filter transformations only) and
  are vendor-specific, so convention-based loading is safe and more flexible

ADDING NEW ADAPTERS:

For OpenAPI tools:
  1. Create vendor_adapters/myvendor/adapter.py with MyVendorAdapter class
  2. Add "myvendor" to ALLOWED_VENDORS whitelist (see get_vendor_adapter)
  3. Add explicit import in get_vendor_adapter() function

  Example:
    class MyVendorAdapter:
        def transform_body(self, body, operation_id, prompt_params=None):
            # Apply vendor-specific transformations
            return body

For GA4 tools:
  1. Create vendor_adapters/myvendor.py with MyvendorGA4Adapter class
  2. Adapter will be automatically discovered via naming convention
  3. No whitelist needed - just follow the pattern

  Example:
    class MyvendorGA4Adapter:
        def __init__(self, config):
            self.config = config

        def transform_dimension_filter(self, base_filter, realtime=False):
            # Apply vendor-specific filtering rules
            return modified_filter

See docs/vendor-adapters.md for complete guide.
"""

import re
import threading

# Thread-safe lock for dynamic vendor adapter imports
_import_lock = threading.Lock()


def get_vendor_adapter(name: str):
    """Return a vendor adapter for OpenAPI-based tools.
    
    This adapter transforms request/response bodies for generic OpenAPI specifications.
    Uses whitelist-based security - vendors must be explicitly registered in ALLOWED_VENDORS.

    Args:
        name: Vendor identifier (e.g., "ptengine", "datainsight")

    Returns:
        Vendor adapter instance or None if vendor not found/not whitelisted

    Example:
        adapter = get_vendor_adapter("ptengine")
        if adapter:
            body = adapter.transform_body(body, "queryData", prompt_params)
    
    Security:
        - Validates vendor name to prevent code injection and path traversal
        - Only allows alphanumeric characters, underscores, and hyphens
        - Requires vendor to be in ALLOWED_VENDORS whitelist
        - Must explicitly import vendor module (no dynamic imports)
    """
    if not name:
        return None

    # Security: Validate vendor name to prevent code injection and path traversal
    # Block path traversal attempts
    if ".." in name or "/" in name or "\\" in name:
        # Path traversal attempt detected - reject
        return None

    # Only allow alphanumeric characters, underscores, and hyphens
    if not name.replace("_", "").replace("-", "").isalnum():
        # Invalid vendor name format - reject
        return None

    # Convert to lowercase for case-insensitive matching
    vendor_name = name.lower()

    # Security: Whitelist of allowed vendor adapters
    # This prevents arbitrary module imports that could be a security risk
    ALLOWED_VENDORS = {
        # Add your vendor names here as you implement them
        # "myvendor",
        "ptengine",  # PTEngine analytics adapter (OpenAPI-based)
        # Note: GA4 uses custom Python tools (not OpenAPI adapter)
        # "datainsight",
    }

    # If vendor not in whitelist, return None (graceful fallback)
    if vendor_name not in ALLOWED_VENDORS:
        return None

    try:
        # PTEngine vendor adapter
        if vendor_name == "ptengine":
            from .ptengine.adapter import PTEngineAdapter
            return PTEngineAdapter()

        # Example vendor adapter registration:
        # if vendor_name == "myvendor":
        #     from .myvendor.adapter import MyVendorAdapter
        #     return MyVendorAdapter()

        # Add your vendor adapters here
        pass

    except ImportError:
        # Graceful fallback if vendor module not available
        return None
    except Exception:
        # Log but don't crash if adapter loading fails
        return None

    return None


def get_ga4_adapter(name: str, config: dict):
    """Return a vendor adapter for GA4 (Google Analytics 4) tools.
    
    This adapter transforms dimension filters for GA4 queries to enforce vendor-specific
    policies (domain restrictions, ad exclusions, campaign filtering, etc.).
    
    Unlike get_vendor_adapter(), this uses convention-based dynamic loading without a
    whitelist. This is safe because GA4 adapters have a limited scope - they only
    transform dimension filters, not arbitrary API requests.
    
    Vendor adapter modules should be named vendor_adapters/{name}.py and export
    a class named {Name}GA4Adapter (e.g., acme.py exports AcmeGA4Adapter).

    Args:
        name: Vendor identifier (e.g., "acme", "myvendor")
        config: Vendor configuration dict from shannon.yaml, containing:
                - domain: Target domain for filtering
                - exclude_paths_contains: List of path patterns to exclude
                - exclude_session_source_medium_exact: Source/medium pairs to exclude
                - treat_any_campaign_as_ad: Whether to exclude all campaigns

    Returns:
        GA4 vendor adapter instance or None if vendor module not found

    Example:
        config = {
            "domain": "www.example.com",
            "exclude_paths_contains": ["/ad/"],
            "exclude_session_source_medium_exact": ["google / cpc", "facebook / cpc"]
        }
        adapter = get_ga4_adapter("acme", config)
        if adapter:
            effective_filter = adapter.transform_dimension_filter(base_filter)

    Security:
        - Validates vendor name to prevent code injection and path traversal
        - Only allows alphanumeric characters, underscores, and hyphens
        - Gracefully handles ImportError if vendor module not available
        - Safe to use dynamic imports because GA4 adapters only transform filters
          (limited scope compared to OpenAPI adapters)
    """
    if not name:
        return None

    # Security: Strict validation to prevent code injection and path traversal
    # Only allow: starts with letter, alphanumeric with _ or -, max 50 chars
    # This blocks: __init__, _private, path traversal, special chars, excessive length
    if not re.match(r'^[a-zA-Z][a-zA-Z0-9_-]{0,49}$', name):
        return None

    # Convert to lowercase for case-insensitive matching
    vendor_name = name.lower()

    try:
        # Thread-safe dynamic import to prevent race conditions
        # Multiple concurrent requests won't cause import conflicts
        with _import_lock:
            # Dynamically import vendor adapter module
            # Convention: vendor_adapters/{vendor_name}.py exports {VendorName}GA4Adapter
            # Example: acme.py exports AcmeGA4Adapter
            import importlib
            module_name = f"llm_service.tools.vendor_adapters.{vendor_name}"

            # Convert vendor_name to class name (e.g., "acme" -> "Acme")
            class_name = f"{vendor_name.capitalize()}GA4Adapter"

            # Try to import the module
            module = importlib.import_module(module_name)
            adapter_class = getattr(module, class_name)

            return adapter_class(config)

    except (ImportError, AttributeError):
        # Graceful fallback if vendor module or class not available
        # This allows Shannon to work without vendor-specific adapters
        return None
    except Exception as e:
        # Log but don't crash if adapter loading fails
        import logging
        logging.getLogger(__name__).warning(
            f"Failed to load GA4 vendor adapter '{name}': {e}"
        )
        return None
