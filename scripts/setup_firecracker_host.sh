#!/usr/bin/env bash
# Setup Firecracker host environment
# Run this on a KVM-enabled host (EC2 bare-metal, dedicated host, etc.)

set -euo pipefail

echo "Setting up Firecracker host environment..."

# Check KVM availability
if [ ! -e /dev/kvm ]; then
    echo "ERROR: /dev/kvm not found. This host does not support KVM."
    echo "Use a bare-metal instance or enable nested virtualization."
    exit 1
fi

# Create jailer user
if ! id -u firecracker > /dev/null 2>&1; then
    sudo useradd --system --no-create-home --shell /usr/sbin/nologin firecracker
    echo "Created firecracker user"
fi

# Set KVM permissions
sudo setfacl -m u:firecracker:rw /dev/kvm
echo "Set KVM permissions for firecracker user"

# Create jailer directories
sudo mkdir -p /srv/jailer
sudo chown firecracker:firecracker /srv/jailer
echo "Created jailer directory"

# Create session workspaces directory
sudo mkdir -p /tmp/shannon-sessions
sudo chmod 1777 /tmp/shannon-sessions
echo "Created session workspaces directory"

echo ""
echo "Firecracker host setup complete!"
echo ""
echo "Verify with: ls -la /dev/kvm"
