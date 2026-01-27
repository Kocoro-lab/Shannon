#!/usr/bin/env bash
# Build Firecracker rootfs image with Python data science packages

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
IMAGES_DIR="${PROJECT_ROOT}/infra/firecracker/images"
OUTPUT_DIR="/var/lib/firecracker/images"
ROOTFS_SIZE_MB="${ROOTFS_SIZE_MB:-2048}"  # 2GB default

MOUNT_DIR=""
CONTAINER_ID=""

cleanup() {
    echo "Cleaning up..."
    if [ -n "${MOUNT_DIR}" ] && mountpoint -q "${MOUNT_DIR}" 2>/dev/null; then
        sudo umount "${MOUNT_DIR}" || true
    fi
    if [ -n "${MOUNT_DIR}" ] && [ -d "${MOUNT_DIR}" ]; then
        rmdir "${MOUNT_DIR}" || true
    fi
    if [ -n "${CONTAINER_ID}" ]; then
        docker rm "${CONTAINER_ID}" 2>/dev/null || true
    fi
}
trap cleanup EXIT

echo "Building Firecracker Python rootfs..."

# Ensure output directory exists
sudo mkdir -p "${OUTPUT_DIR}"

# Build Docker image
docker build -t fc-python-rootfs -f "${IMAGES_DIR}/Dockerfile.rootfs" "${IMAGES_DIR}"

# Create empty ext4 filesystem image
ROOTFS_PATH="${OUTPUT_DIR}/python-datascience.ext4"
sudo dd if=/dev/zero of="${ROOTFS_PATH}" bs=1M count="${ROOTFS_SIZE_MB}"
sudo mkfs.ext4 "${ROOTFS_PATH}"

# Mount and extract container contents
MOUNT_DIR=$(mktemp -d)
sudo mount -o loop "${ROOTFS_PATH}" "${MOUNT_DIR}"

# Create container and export filesystem
CONTAINER_ID=$(docker create fc-python-rootfs)
docker export "${CONTAINER_ID}" | sudo tar -xf - -C "${MOUNT_DIR}"
docker rm "${CONTAINER_ID}"
CONTAINER_ID=""  # Clear so cleanup doesn't double-rm

# Unmount
sudo umount "${MOUNT_DIR}"
rmdir "${MOUNT_DIR}"
MOUNT_DIR=""  # Clear so cleanup doesn't double-unmount

echo ""
echo "Rootfs created at: ${ROOTFS_PATH}"
echo "Size: $(du -h "${ROOTFS_PATH}" | cut -f1)"
echo ""
echo "To test: mount -o loop ${ROOTFS_PATH} /mnt && ls /mnt"
