#!/usr/bin/env python3
"""
Create decomposition_patterns collection in Qdrant for storing decomposition history.
This enables semantic search for similar task decomposition patterns.
"""

import os
import sys
from qdrant_client import QdrantClient
from qdrant_client.models import (
    Distance,
    VectorParams,
    PointStruct,
    PayloadSchemaType,
    TextIndexParams,
    TokenizerType,
)

# Get Qdrant URL from environment or use default
QDRANT_URL = os.getenv("QDRANT_URL", "http://localhost:6333")
COLLECTION_NAME = "decomposition_patterns"
VECTOR_SIZE = 1536  # OpenAI embedding size

def create_decomposition_patterns_collection():
    """Create the decomposition_patterns collection with appropriate configuration."""

    print(f"Connecting to Qdrant at {QDRANT_URL}...")
    client = QdrantClient(url=QDRANT_URL)

    # Check if collection exists
    collections = client.get_collections().collections
    existing_collections = [c.name for c in collections]

    if COLLECTION_NAME in existing_collections:
        print(f"Collection '{COLLECTION_NAME}' already exists")

        # Optionally recreate collection (uncomment if needed)
        # response = input("Do you want to recreate it? (y/N): ")
        # if response.lower() == 'y':
        #     print(f"Deleting existing collection '{COLLECTION_NAME}'...")
        #     client.delete_collection(collection_name=COLLECTION_NAME)
        # else:
        #     print("Keeping existing collection")
        #     return
        return

    print(f"Creating collection '{COLLECTION_NAME}'...")

    # Create collection with vector configuration
    client.create_collection(
        collection_name=COLLECTION_NAME,
        vectors_config=VectorParams(
            size=VECTOR_SIZE,
            distance=Distance.COSINE,
        ),
        # Optimize for search performance
        optimizers_config={
            "default_segment_number": 2,
            "indexing_threshold": 10000,
        },
        # Configure write-ahead log
        wal_config={
            "wal_capacity_mb": 32,
            "wal_segments_ahead": 0,
        }
    )

    print(f"Collection '{COLLECTION_NAME}' created successfully")

    # Create payload indexes for efficient filtering
    print("Creating payload indexes...")

    # Index for session_id (exact match filtering)
    client.create_payload_index(
        collection_name=COLLECTION_NAME,
        field_name="session_id",
        field_schema=PayloadSchemaType.KEYWORD,
    )

    # Index for user_id (exact match filtering)
    client.create_payload_index(
        collection_name=COLLECTION_NAME,
        field_name="user_id",
        field_schema=PayloadSchemaType.KEYWORD,
    )

    # Index for strategy (exact match filtering)
    client.create_payload_index(
        collection_name=COLLECTION_NAME,
        field_name="strategy",
        field_schema=PayloadSchemaType.KEYWORD,
    )

    # Index for success_rate (range filtering)
    client.create_payload_index(
        collection_name=COLLECTION_NAME,
        field_name="success_rate",
        field_schema=PayloadSchemaType.FLOAT,
    )

    # Index for timestamp (range filtering and sorting)
    client.create_payload_index(
        collection_name=COLLECTION_NAME,
        field_name="timestamp",
        field_schema=PayloadSchemaType.INTEGER,
    )

    # Index for pattern text search
    client.create_payload_index(
        collection_name=COLLECTION_NAME,
        field_name="pattern",
        field_schema="text",
    )

    print("Payload indexes created successfully")

    # Get collection info to verify
    collection_info = client.get_collection(collection_name=COLLECTION_NAME)
    print(f"\nCollection info:")
    print(f"  Vectors count: {collection_info.vectors_count}")
    print(f"  Points count: {collection_info.points_count}")
    print(f"  Indexed vectors: {collection_info.indexed_vectors_count}")
    print(f"  Status: {collection_info.status}")

    print(f"\n✅ Collection '{COLLECTION_NAME}' setup complete!")

if __name__ == "__main__":
    try:
        create_decomposition_patterns_collection()
    except Exception as e:
        print(f"Error creating collection: {e}", file=sys.stderr)
        sys.exit(1)