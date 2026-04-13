# Shannon Python SDK - Packaging Guide

**Version:** 0.7.0

---

## Package Information

- **Name:** `shannon-sdk`
- **Version:** `0.6.0`
- **Size:** ~38KB wheel, ~32KB source
- **Python:** >=3.9
- **License:** MIT

---

## Quick Publish

### TestPyPI (Recommended First)

```bash
# 1. Build (if needed)
make build

# 2. Upload to TestPyPI
make publish-test

# 3. Test install
pip install -i https://test.pypi.org/simple/ shannon-sdk==0.6.0
```

### Production PyPI

```bash
# Upload to PyPI (requires confirmation)
make publish
```

---

## Step-by-Step Publishing

### Prerequisites

```bash
# Install build tools (already in dev deps)
pip install -e ".[dev]"

# Or manually:
pip install build twine
```

### 1. Run Tests

```bash
make test
# Should pass with >30% coverage
```

Live validation is opt-in:

```bash
make test-live
```

The package still ships checked-in generated stubs from `src/shannon/generated/`,
but this SDK package does not currently define a local regeneration target.

### 2. Build Distribution

```bash
make build
```

**Output:**
- `dist/shannon_sdk-0.6.0.tar.gz` (source distribution)
- `dist/shannon_sdk-0.6.0-py3-none-any.whl` (wheel)

### 3. Verify Package

```bash
# Check package metadata
python3 -m twine check dist/*

# Inspect contents
tar -tzf dist/shannon_sdk-0.6.0.tar.gz | head -20
```

**Expected:**
- ✅ Only README.md (no internal docs)
- ✅ All Python source files
- ✅ All generated proto stubs
- ✅ No test files
- ✅ No development files

### 4. Upload to TestPyPI

```bash
# Upload
python3 -m twine upload -r testpypi dist/*

# Test install in clean environment
python3 -m venv test-env
source test-env/bin/activate
pip install -i https://test.pypi.org/simple/ shannon-sdk==0.6.0

# Verify
python3 -c "from shannon import ShannonClient; print('✓ Import works')"
```

### 5. Upload to PyPI (Production)

```bash
# Final checks
make test
make build
python3 -m twine check dist/*

# Upload (with confirmation)
make publish

# Or manually:
python3 -m twine upload dist/*
```

---

## Package Contents

### Included

```
shannon_sdk-0.6.0/
├── README.md                    # User documentation
├── pyproject.toml               # Package metadata
├── src/shannon/
│   ├── __init__.py             # Public API
│   ├── client.py               # Main client
│   ├── models.py               # Data models
│   ├── errors.py               # Exceptions
│   ├── cli.py                  # CLI tool
│   └── generated/              # Checked-in generated stubs
│       ├── common/
│       ├── orchestrator/
│       └── session/
```

### Excluded (via MANIFEST.in)

- ❌ `IMPLEMENTATION_SUMMARY.md`
- ❌ `LIVE_TEST_RESULTS.md`
- ❌ `PHASE2_COMPLETE.md`
- ❌ `REVIEW_FIXES.md`
- ❌ `STATUS.md`
- ❌ `TEST_SUMMARY.md`
- ❌ `tests/`
- ❌ `Makefile`
- ❌ `.gitignore`

---

## PyPI Credentials

### Setup (One-time)

Create `~/.pypirc`:

```ini
[distutils]
index-servers =
    pypi
    testpypi

[pypi]
username = __token__
password = pypi-YOUR-TOKEN-HERE

[testpypi]
repository = https://test.pypi.org/legacy/
username = __token__
password = pypi-YOUR-TESTPYPI-TOKEN-HERE
```

**Get tokens:**
- PyPI: https://pypi.org/manage/account/token/
- TestPyPI: https://test.pypi.org/manage/account/token/

---

## Installation Methods

### From PyPI (after publishing)

```bash
pip install shannon-sdk
```

### From TestPyPI

```bash
pip install -i https://test.pypi.org/simple/ shannon-sdk==0.6.0
```

### From Source

```bash
git clone https://github.com/Kocoro-lab/Shannon.git
cd Shannon/clients/python
pip install -e .
```

---

## Version Bumping

### When a release is planned

```bash
# Edit pyproject.toml
version = "0.6.1"

# Clean, rebuild
make clean
make build
make publish-test
```

### Example minor release

```bash
version = "0.7.0"
```

### Example stable release

```bash
version = "1.0.0"
```

**PEP 440 versioning:**
- `0.7.0a1` - Alpha 1
- `0.7.0b1` - Beta 1
- `0.7.0rc1` - Release candidate 1
- `1.0.0` - Stable release

---

## Troubleshooting

### "No proto stubs in package"

```bash
# Ensure checked-in generated stubs are present before build
make build
```

### "Package too large"

```bash
# Check what's included
tar -tzf dist/shannon_sdk-*.tar.gz | less

# Verify MANIFEST.in excludes are working
```

### "Import errors after install"

```bash
# Check generated stubs are included
tar -tzf dist/*.tar.gz | grep generated

# Verify __init__.py files exist
tar -tzf dist/*.tar.gz | grep __init__
```

### "Twine upload fails"

```bash
# Check credentials
cat ~/.pypirc

# Verify package
python3 -m twine check dist/*

# Try with --verbose
python3 -m twine upload --verbose -r testpypi dist/*
```

---

## Makefile Reference

```bash
make test          # Run tests with coverage
make test-live     # Run live validation tests
make build         # Build distribution packages
make publish-test  # Upload to TestPyPI
make publish       # Upload to PyPI (with confirmation)
make clean         # Remove build artifacts
```

---

## Checklist Before Publishing

- [ ] All tests passing (`make test`)
- [ ] Live validation run when needed (`make test-live`)
- [ ] Version bumped in `pyproject.toml`
- [ ] README.md updated
- [ ] Built packages (`make build`)
- [ ] Verified contents (`tar -tzf dist/*.tar.gz`)
- [ ] Twine check passed (`twine check dist/*`)
- [ ] Tested on TestPyPI
- [ ] Ready for production PyPI

---

## Current Status

⚠️ **0.6.0 development baseline only**

**Package validated:**
- [x] Builds successfully
- [x] Twine check passes
- [x] Only README.md included
- [x] All proto stubs included
- [x] No test files included
- [x] Current version aligned to 0.6.0

**Next step:** Finish parity work, then decide on the next release version before publishing.
