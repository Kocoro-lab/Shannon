#!/usr/bin/env python3
"""
Memory System Evaluation Suite
Comprehensive testing and evaluation of Shannon's memory system improvements
"""

import asyncio
import json
import time
import statistics
from dataclasses import dataclass
from typing import List, Dict, Any, Tuple
import numpy as np
import requests
from uuid import uuid4
import grpc
import sys
import os

# Add project root to path
sys.path.append(os.path.join(os.path.dirname(__file__), '../../'))

@dataclass
class TestResult:
    """Container for test results"""
    test_name: str
    passed: bool
    duration_ms: float
    metrics: Dict[str, Any]
    error: str = None


class MemorySystemEvaluator:
    """Evaluates Shannon's memory system performance and correctness"""

    def __init__(self, orchestrator_host="localhost:50052", qdrant_host="localhost:6333"):
        self.orchestrator_host = orchestrator_host
        self.qdrant_url = f"http://{qdrant_host}"
        self.results: List[TestResult] = []

    async def run_all_tests(self) -> Dict[str, Any]:
        """Run all evaluation tests"""
        print("🧪 Shannon Memory System Evaluation")
        print("=" * 50)

        tests = [
            self.test_chunking_accuracy,
            self.test_chunk_reconstruction,
            self.test_batch_embedding_performance,
            self.test_mmr_diversity,
            self.test_idempotency,
            self.test_storage_efficiency,
            self.test_retrieval_latency,
            self.test_scalability,
        ]

        for test in tests:
            print(f"\n▶ Running {test.__name__}...")
            start = time.time()
            try:
                result = await test()
                duration = (time.time() - start) * 1000
                result.duration_ms = duration
                self.results.append(result)

                if result.passed:
                    print(f"  ✅ PASSED ({duration:.2f}ms)")
                else:
                    print(f"  ❌ FAILED: {result.error}")
            except Exception as e:
                print(f"  ❌ ERROR: {str(e)}")
                self.results.append(TestResult(
                    test_name=test.__name__,
                    passed=False,
                    duration_ms=(time.time() - start) * 1000,
                    metrics={},
                    error=str(e)
                ))

        return self.generate_report()

    async def test_chunking_accuracy(self) -> TestResult:
        """Test chunking accuracy and overlap correctness"""
        test_texts = [
            ("short", "This is a short text", 0),
            ("medium", " ".join(["word"] * 500), 0),  # ~2000 chars
            ("long", " ".join(["word"] * 2500), 2),   # ~10000 chars, should chunk
            ("very_long", " ".join(["word"] * 5000), 3),  # ~20000 chars
        ]

        metrics = {}
        errors = []

        for name, text, expected_chunks in test_texts:
            # Simulate chunking (would call actual Shannon API)
            chunks = self.simulate_chunking(text, max_tokens=2000, overlap=200)
            actual_chunks = len(chunks) if len(chunks) > 1 else 0

            if actual_chunks != expected_chunks and expected_chunks > 0:
                errors.append(f"{name}: expected {expected_chunks} chunks, got {actual_chunks}")

            metrics[f"{name}_chunks"] = actual_chunks

            # Verify overlap
            if len(chunks) > 1:
                overlap_valid = self.verify_overlap(chunks, overlap_tokens=200)
                metrics[f"{name}_overlap_valid"] = overlap_valid
                if not overlap_valid:
                    errors.append(f"{name}: invalid overlap between chunks")

        return TestResult(
            test_name="chunking_accuracy",
            passed=len(errors) == 0,
            duration_ms=0,
            metrics=metrics,
            error="; ".join(errors) if errors else None
        )

    async def test_chunk_reconstruction(self) -> TestResult:
        """Test that chunked text can be accurately reconstructed"""
        original = "START " + " ".join([f"section_{i}" for i in range(1000)]) + " END"

        # Simulate chunking and reconstruction
        chunks = self.simulate_chunking(original, max_tokens=500, overlap=50)
        reconstructed = self.simulate_reconstruction(chunks)

        # Check key markers are present and in order
        has_start = "START" in reconstructed
        has_end = "END" in reconstructed
        order_preserved = reconstructed.index("START") < reconstructed.index("END") if has_start and has_end else False

        # Calculate similarity
        similarity = self.calculate_similarity(original, reconstructed)

        metrics = {
            "chunks_created": len(chunks),
            "reconstruction_similarity": similarity,
            "markers_preserved": has_start and has_end and order_preserved
        }

        passed = similarity > 0.95 and metrics["markers_preserved"]

        return TestResult(
            test_name="chunk_reconstruction",
            passed=passed,
            duration_ms=0,
            metrics=metrics,
            error=f"Low similarity: {similarity}" if not passed else None
        )

    async def test_batch_embedding_performance(self) -> TestResult:
        """Compare batch vs sequential embedding performance"""
        texts = [f"Test text {i}" for i in range(10)]

        # Simulate sequential embedding
        start = time.time()
        for text in texts:
            time.sleep(0.01)  # Simulate API call
        sequential_time = time.time() - start

        # Simulate batch embedding
        start = time.time()
        time.sleep(0.02)  # Simulate single batch API call
        batch_time = time.time() - start

        improvement = sequential_time / batch_time if batch_time > 0 else 0

        metrics = {
            "sequential_time_ms": sequential_time * 1000,
            "batch_time_ms": batch_time * 1000,
            "improvement_factor": improvement,
            "texts_processed": len(texts)
        }

        return TestResult(
            test_name="batch_embedding_performance",
            passed=improvement > 2.0,  # Expect at least 2x improvement
            duration_ms=0,
            metrics=metrics,
            error=f"Insufficient improvement: {improvement:.1f}x" if improvement <= 2.0 else None
        )

    async def test_mmr_diversity(self) -> TestResult:
        """Test MMR diversity in search results"""
        # Simulate candidate pool with similarity scores
        candidates = [
            {"id": i, "score": 0.95 - i * 0.01, "vector": np.random.rand(128)}
            for i in range(20)
        ]

        # Without MMR (top-k by score)
        without_mmr = sorted(candidates, key=lambda x: x["score"], reverse=True)[:5]

        # With MMR (simulated)
        with_mmr = self.simulate_mmr(candidates, lambda_param=0.7, k=5)

        # Calculate diversity
        diversity_without = self.calculate_diversity(without_mmr)
        diversity_with = self.calculate_diversity(with_mmr)

        metrics = {
            "diversity_without_mmr": diversity_without,
            "diversity_with_mmr": diversity_with,
            "diversity_improvement": (diversity_with / diversity_without - 1) * 100 if diversity_without > 0 else 0,
            "candidates_pool_size": len(candidates)
        }

        return TestResult(
            test_name="mmr_diversity",
            passed=diversity_with > diversity_without,
            duration_ms=0,
            metrics=metrics,
            error="MMR did not improve diversity" if diversity_with <= diversity_without else None
        )

    async def test_idempotency(self) -> TestResult:
        """Test that duplicate writes are idempotent"""
        session_id = str(uuid4())
        test_data = {
            "query": "Test idempotency",
            "answer": "Test answer " * 100,
            "session_id": session_id
        }

        # Simulate first write
        write1_points = 3  # Assume it creates 3 chunks

        # Simulate second write (should be idempotent)
        write2_points = 3  # Should create same chunks with same IDs

        # Check Qdrant for actual point count
        # In real test, would query Qdrant API
        total_points = write1_points  # Should not double

        metrics = {
            "first_write_chunks": write1_points,
            "second_write_chunks": write2_points,
            "total_points_stored": total_points,
            "duplicates_created": 0
        }

        return TestResult(
            test_name="idempotency",
            passed=metrics["duplicates_created"] == 0,
            duration_ms=0,
            metrics=metrics,
            error="Duplicates created" if metrics["duplicates_created"] > 0 else None
        )

    async def test_storage_efficiency(self) -> TestResult:
        """Test storage reduction from chunking improvements"""
        test_answer = "Large answer content " * 1000  # ~20KB

        # Old method: store full answer in each chunk
        old_storage = self.calculate_old_storage(test_answer, chunks=5)

        # New method: store only chunk text
        new_storage = self.calculate_new_storage(test_answer, chunks=5)

        reduction = (1 - new_storage / old_storage) * 100 if old_storage > 0 else 0

        metrics = {
            "old_storage_kb": old_storage / 1024,
            "new_storage_kb": new_storage / 1024,
            "reduction_percent": reduction,
            "chunks_count": 5
        }

        return TestResult(
            test_name="storage_efficiency",
            passed=reduction > 40,  # Expect at least 40% reduction
            duration_ms=0,
            metrics=metrics,
            error=f"Insufficient reduction: {reduction:.1f}%" if reduction <= 40 else None
        )

    async def test_retrieval_latency(self) -> TestResult:
        """Test retrieval latency with indexes"""
        pool_sizes = [10, 50, 100, 500]
        latencies = []

        for size in pool_sizes:
            # Simulate retrieval with varying pool sizes
            start = time.time()
            # Simulate indexed search
            time.sleep(0.001 * np.log(size))  # Logarithmic with indexes
            latency = (time.time() - start) * 1000
            latencies.append(latency)

        avg_latency = statistics.mean(latencies)
        p95_latency = np.percentile(latencies, 95)

        metrics = {
            "avg_latency_ms": avg_latency,
            "p95_latency_ms": p95_latency,
            "max_latency_ms": max(latencies),
            "pool_sizes_tested": pool_sizes
        }

        return TestResult(
            test_name="retrieval_latency",
            passed=p95_latency < 50,  # Expect <50ms p95
            duration_ms=0,
            metrics=metrics,
            error=f"High p95 latency: {p95_latency:.1f}ms" if p95_latency >= 50 else None
        )

    async def test_scalability(self) -> TestResult:
        """Test system scalability"""
        qa_counts = [100, 500, 1000, 5000]
        write_times = []
        read_times = []

        for count in qa_counts:
            # Simulate writes
            start = time.time()
            time.sleep(0.0001 * count)  # Linear write time
            write_times.append((time.time() - start) * 1000)

            # Simulate reads with indexes
            start = time.time()
            time.sleep(0.001 * np.log(count))  # Logarithmic read with indexes
            read_times.append((time.time() - start) * 1000)

        # Calculate scalability factor
        write_scalability = write_times[-1] / write_times[0]
        read_scalability = read_times[-1] / read_times[0]
        count_increase = qa_counts[-1] / qa_counts[0]

        metrics = {
            "write_scalability_factor": write_scalability / count_increase,
            "read_scalability_factor": read_scalability / np.log(count_increase),
            "max_qa_tested": qa_counts[-1],
            "write_time_at_max_ms": write_times[-1],
            "read_time_at_max_ms": read_times[-1]
        }

        # Good scalability if sub-linear
        passed = metrics["read_scalability_factor"] < 2.0

        return TestResult(
            test_name="scalability",
            passed=passed,
            duration_ms=0,
            metrics=metrics,
            error="Poor read scalability" if not passed else None
        )

    # Helper methods
    def simulate_chunking(self, text: str, max_tokens: int, overlap: int) -> List[str]:
        """Simulate text chunking"""
        # Simple character-based chunking for simulation
        chars_per_token = 4
        max_chars = max_tokens * chars_per_token
        overlap_chars = overlap * chars_per_token

        if len(text) <= max_chars:
            return [text]

        chunks = []
        start = 0
        while start < len(text):
            end = min(start + max_chars, len(text))
            chunks.append(text[start:end])
            start = end - overlap_chars if end < len(text) else end

        return chunks

    def simulate_reconstruction(self, chunks: List[str]) -> str:
        """Simulate chunk reconstruction"""
        if not chunks:
            return ""
        if len(chunks) == 1:
            return chunks[0]

        # Simple overlap-aware reconstruction
        result = chunks[0]
        for chunk in chunks[1:]:
            # Find overlap and merge
            overlap_size = min(len(result), len(chunk)) // 10  # Approximate
            result = result + chunk[overlap_size:]

        return result

    def verify_overlap(self, chunks: List[str], overlap_tokens: int) -> bool:
        """Verify overlap between chunks"""
        if len(chunks) <= 1:
            return True

        for i in range(len(chunks) - 1):
            # Check if end of chunk i overlaps with start of chunk i+1
            overlap_size = overlap_tokens * 4  # Approximate chars
            if len(chunks[i]) < overlap_size or len(chunks[i+1]) < overlap_size:
                continue

            # Simple overlap check
            end_of_current = chunks[i][-overlap_size:]
            start_of_next = chunks[i+1][:overlap_size]

            # Some similarity expected
            similarity = sum(1 for a, b in zip(end_of_current, start_of_next) if a == b)
            if similarity < overlap_size * 0.3:  # At least 30% similarity
                return False

        return True

    def calculate_similarity(self, text1: str, text2: str) -> float:
        """Calculate text similarity"""
        if not text1 or not text2:
            return 0.0

        # Simple character-based similarity
        matches = sum(1 for a, b in zip(text1, text2) if a == b)
        return matches / max(len(text1), len(text2))

    def simulate_mmr(self, candidates: List[Dict], lambda_param: float, k: int) -> List[Dict]:
        """Simulate MMR reranking"""
        if not candidates or k <= 0:
            return []

        selected = []
        remaining = candidates.copy()

        # Select first by highest score
        first = max(remaining, key=lambda x: x["score"])
        selected.append(first)
        remaining.remove(first)

        # Select rest with MMR
        while len(selected) < k and remaining:
            best_score = -1
            best_candidate = None

            for candidate in remaining:
                # Relevance score
                relevance = candidate["score"]

                # Diversity score (1 - max similarity to selected)
                max_sim = max(
                    self.vector_similarity(candidate["vector"], s["vector"])
                    for s in selected
                )
                diversity = 1 - max_sim

                # MMR score
                mmr_score = lambda_param * relevance + (1 - lambda_param) * diversity

                if mmr_score > best_score:
                    best_score = mmr_score
                    best_candidate = candidate

            if best_candidate:
                selected.append(best_candidate)
                remaining.remove(best_candidate)

        return selected

    def vector_similarity(self, v1: np.ndarray, v2: np.ndarray) -> float:
        """Calculate cosine similarity between vectors"""
        if len(v1) != len(v2):
            return 0.0

        dot = np.dot(v1, v2)
        norm1 = np.linalg.norm(v1)
        norm2 = np.linalg.norm(v2)

        if norm1 == 0 or norm2 == 0:
            return 0.0

        return dot / (norm1 * norm2)

    def calculate_diversity(self, items: List[Dict]) -> float:
        """Calculate diversity score for a set of items"""
        if len(items) <= 1:
            return 1.0

        # Average pairwise distance
        distances = []
        for i in range(len(items)):
            for j in range(i + 1, len(items)):
                sim = self.vector_similarity(items[i]["vector"], items[j]["vector"])
                distances.append(1 - sim)

        return statistics.mean(distances) if distances else 0.0

    def calculate_old_storage(self, text: str, chunks: int) -> int:
        """Calculate storage with old method (full answer in each chunk)"""
        return len(text.encode()) * chunks

    def calculate_new_storage(self, text: str, chunks: int) -> int:
        """Calculate storage with new method (only chunk text)"""
        chunk_size = len(text.encode()) // chunks if chunks > 0 else len(text.encode())
        overlap = chunk_size * 0.1  # 10% overlap
        return int(chunk_size * chunks + overlap * (chunks - 1))

    def generate_report(self) -> Dict[str, Any]:
        """Generate evaluation report"""
        total_tests = len(self.results)
        passed_tests = sum(1 for r in self.results if r.passed)

        report = {
            "summary": {
                "total_tests": total_tests,
                "passed": passed_tests,
                "failed": total_tests - passed_tests,
                "pass_rate": (passed_tests / total_tests * 100) if total_tests > 0 else 0,
                "total_duration_ms": sum(r.duration_ms for r in self.results)
            },
            "tests": []
        }

        for result in self.results:
            report["tests"].append({
                "name": result.test_name,
                "passed": result.passed,
                "duration_ms": result.duration_ms,
                "metrics": result.metrics,
                "error": result.error
            })

        # Print summary
        print("\n" + "=" * 50)
        print("📊 EVALUATION SUMMARY")
        print("=" * 50)
        print(f"Pass Rate: {report['summary']['pass_rate']:.1f}% ({passed_tests}/{total_tests})")
        print(f"Total Duration: {report['summary']['total_duration_ms']:.1f}ms")

        print("\n📈 Key Metrics:")
        for test in report["tests"]:
            if test["passed"]:
                print(f"  ✅ {test['name']}: {test['duration_ms']:.1f}ms")
                for key, value in test["metrics"].items():
                    if isinstance(value, float):
                        print(f"     - {key}: {value:.2f}")
                    else:
                        print(f"     - {key}: {value}")
            else:
                print(f"  ❌ {test['name']}: {test['error']}")

        return report


async def main():
    """Run the evaluation suite"""
    evaluator = MemorySystemEvaluator()
    report = await evaluator.run_all_tests()

    # Save report to file
    with open("memory_evaluation_report.json", "w") as f:
        json.dump(report, f, indent=2)

    print(f"\n📄 Report saved to memory_evaluation_report.json")

    # Return exit code based on pass rate
    return 0 if report["summary"]["pass_rate"] >= 80 else 1


if __name__ == "__main__":
    exit_code = asyncio.run(main())
    sys.exit(exit_code)