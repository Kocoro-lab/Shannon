#!/usr/bin/env bash
set -euo pipefail

echo "=== Docker Cache Cleanup Script ==="
echo ""
echo "Current Docker disk usage:"
docker system df
echo ""

echo "Options for cleanup:"
echo "1. Remove stopped containers, unused networks, dangling images, and build cache"
echo "2. Remove all unused containers, networks, images (both dangling and unreferenced)"
echo "3. Remove everything (WARNING: This will remove ALL Docker data)"
echo "4. Cancel"
echo ""
read -p "Choose option (1-4): " choice

case $choice in
    1)
        echo "Cleaning up dangling resources..."
        docker system prune -f
        ;;
    2)
        echo "Cleaning up all unused resources..."
        docker system prune -a -f
        ;;
    3)
        echo "WARNING: This will remove ALL Docker data!"
        read -p "Are you sure? (yes/no): " confirm
        if [ "$confirm" = "yes" ]; then
            docker system prune -a --volumes -f
            echo "All Docker data removed."
        else
            echo "Cancelled."
        fi
        ;;
    4)
        echo "Cleanup cancelled."
        ;;
    *)
        echo "Invalid option."
        ;;
esac

echo ""
echo "New Docker disk usage:"
docker system df