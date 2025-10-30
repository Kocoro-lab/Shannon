.PHONY: help proto clean install dev test test-cov test-fast lint format build publish-test publish

help:
	@echo "Shannon Python SDK - Development Commands"
	@echo ""
	@echo "  make install       - Install package in development mode"
	@echo "  make dev           - Install with dev dependencies"
	@echo "  make test          - Run all tests with coverage"
	@echo "  make test-fast     - Run tests without coverage"
	@echo "  make test-cov      - Run tests with coverage report"
	@echo "  make lint          - Run linting"
	@echo "  make format        - Format code"
	@echo "  make build         - Build distribution packages"
	@echo "  make publish-test  - Upload to TestPyPI"
	@echo "  make publish       - Upload to PyPI (production)"
	@echo "  make clean         - Remove generated files"

install:
	pip install -e .

dev:
	pip install -e ".[dev]"

test: test-cov

test-fast:
	@echo "Running tests (fast mode)..."
	python3 -m pytest tests/ -v

test-cov:
	@echo "Running tests with coverage..."
	python3 -m pytest tests/ --cov=src/shannon --cov-report=term-missing --cov-report=html --cov-fail-under=30
	@echo "✓ Coverage report: htmlcov/index.html"

lint:
	@echo "Running linters..."
	ruff check src tests
	mypy src --ignore-missing-imports

format:
	ruff format src tests

build:
	@echo "Building distribution packages..."
	python3 -m build
	@echo "✓ Built: dist/shannon_sdk-*.tar.gz and dist/shannon_sdk-*.whl"

publish-test:
	@echo "Uploading to TestPyPI..."
	python3 -m twine upload -r testpypi dist/*
	@echo "✓ Published to TestPyPI"
	@echo "  Install: pip install -i https://test.pypi.org/simple/ shannon-sdk==0.1.0a1"

publish:
	@echo "⚠️  WARNING: Publishing to production PyPI!"
	@echo "  Version: 0.1.0a1"
	@read -p "  Are you sure? (yes/no): " confirm && [ "$$confirm" = "yes" ]
	python3 -m twine upload dist/*
	@echo "✓ Published to PyPI"
	@echo "  Install: pip install shannon-sdk"

clean:
	@echo "Cleaning build artifacts..."
	rm -rf build dist *.egg-info
	rm -rf htmlcov .coverage .pytest_cache
	find . -type d -name __pycache__ -exec rm -rf {} + 2>/dev/null || true
	find . -type f -name "*.pyc" -delete
	@echo "✓ Cleaned"
