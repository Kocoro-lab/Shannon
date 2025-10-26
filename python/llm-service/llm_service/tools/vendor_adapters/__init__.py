"""Vendor adapters for domain-specific API transformations.

This module provides a registry for vendor-specific request/response transformations
that can be applied to OpenAPI tools. Vendor adapters allow clean separation of
generic Shannon infrastructure from domain-specific API requirements.

Example vendor adapter:

    class MyVendorAdapter:
        def transform_body(
            self,
            body: Dict[str, Any],
            operation_id: str,
            prompt_params: Optional[Dict[str, Any]] = None,
        ) -> Dict[str, Any]:
            # Apply vendor-specific transformations
            # - Field aliasing (e.g., "users" → "vendor:users")
            # - Inject session context from prompt_params
            # - Normalize time ranges, sort formats, etc.
            return body

To register a vendor adapter:

    def get_vendor_adapter(name: str):
        if name.lower() == "myvendor":
            from .myvendor import MyVendorAdapter
            return MyVendorAdapter()
        return None

See docs/vendor-adapters.md for complete guide.
"""
from typing import Optional


def get_vendor_adapter(name: str):
    """
    Return a vendor adapter instance by name, or None if not available.

    Args:
        name: Vendor identifier (e.g., "ptengine", "datainsight")

    Returns:
        Vendor adapter instance or None if vendor not found

    Example:
        adapter = get_vendor_adapter("myvendor")
        if adapter:
            body = adapter.transform_body(body, "queryData", prompt_params)
    """
    if not name:
        return None

    try:
        # Example vendor adapter registration:
        # if name.lower() == "myvendor":
        #     from .myvendor import MyVendorAdapter
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
