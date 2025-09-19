"""Shannon LLM Service - Provider-agnostic LLM integration"""

import sys
from pathlib import Path

# Add grpc_gen to sys.path for protobuf imports
_grpc_gen = Path(__file__).parent / "grpc_gen"
if _grpc_gen.exists():
    sys.path.insert(0, str(_grpc_gen))

__version__ = "0.1.0"
