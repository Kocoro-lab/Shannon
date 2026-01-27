#!/usr/bin/env bash
# Download Firecracker binaries and kernel
# Run this on a KVM-enabled host

set -euo pipefail

FIRECRACKER_VERSION="${FIRECRACKER_VERSION:-v1.6.0}"
ARCH=$(uname -m)
ALLOW_UNVERIFIED="${ALLOW_UNVERIFIED:-0}"

# SHA256 checksums for v1.6.0 (update when changing FIRECRACKER_VERSION)
declare -A CHECKSUMS=(
    ["x86_64"]="not-yet-populated"
    ["aarch64"]="not-yet-populated"
)

echo "Downloading Firecracker ${FIRECRACKER_VERSION} for ${ARCH}..."

# Create directories
sudo mkdir -p /usr/local/bin
sudo mkdir -p /var/lib/firecracker/kernel
sudo mkdir -p /var/lib/firecracker/images

# Download Firecracker
RELEASE_URL="https://github.com/firecracker-microvm/firecracker/releases/download"
TGZ_PATH=$(mktemp)
trap 'rm -f "${TGZ_PATH}"' EXIT

curl -L -o "${TGZ_PATH}" "${RELEASE_URL}/${FIRECRACKER_VERSION}/firecracker-${FIRECRACKER_VERSION}-${ARCH}.tgz"

# Verify checksum
EXPECTED="${CHECKSUMS[${ARCH}]:-}"
if [ -n "${EXPECTED}" ] && [ "${EXPECTED}" != "not-yet-populated" ]; then
    ACTUAL=$(sha256sum "${TGZ_PATH}" | awk '{print $1}')
    if [ "${ACTUAL}" != "${EXPECTED}" ]; then
        echo "ERROR: Checksum mismatch!"
        echo "  Expected: ${EXPECTED}"
        echo "  Actual:   ${ACTUAL}"
        exit 1
    fi
    echo "Checksum verified."
else
    if [ "${ALLOW_UNVERIFIED}" = "1" ]; then
        echo "WARNING: No checksum configured for ${ARCH}. Proceeding (ALLOW_UNVERIFIED=1)."
        echo "  Populate CHECKSUMS in this script after verifying the download manually."
    else
        echo "ERROR: No checksum configured for ${ARCH}. Refusing to install unverified binary."
        echo "  To proceed anyway, set ALLOW_UNVERIFIED=1"
        echo "  To add checksums, download manually, run 'sha256sum <file>', and update this script."
        exit 1
    fi
fi

sudo tar -xzf "${TGZ_PATH}" -C /usr/local/bin --strip-components=1

# Download kernel
KERNEL_URL="https://s3.amazonaws.com/spec.ccfc.min/img/quickstart_guide/${ARCH}/kernels/vmlinux.bin"
sudo curl -L -o /var/lib/firecracker/kernel/vmlinux "${KERNEL_URL}"

echo "Firecracker installed:"
/usr/local/bin/firecracker --version

echo ""
echo "Next steps:"
echo "1. Build rootfs: ./scripts/build_firecracker_rootfs.sh"
echo "2. Setup host: ./scripts/setup_firecracker_host.sh"
