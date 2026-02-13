"""Shannon LLM Service - Provider-agnostic LLM integration"""

import sys
import logging
from pathlib import Path

logger = logging.getLogger(__name__)

# Add grpc_gen to sys.path for protobuf imports
_grpc_gen = Path(__file__).parent / "grpc_gen"
if _grpc_gen.exists():
    sys.path.insert(0, str(_grpc_gen))
    logger.debug(f"Added grpc_gen to sys.path: {_grpc_gen}")
else:
    logger.warning(
        f"grpc_gen directory not found at {_grpc_gen}, protobuf imports may fail"
    )

__version__ = "0.2.0"
