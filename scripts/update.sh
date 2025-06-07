#!/bin/bash
# scripts/update.sh

set -e

echo "🔄 Updating IdenaAuthGo..."

# Backup before update
echo "💾 Automatic backup..."
./scripts/backup.sh

# Stop services
echo "⏹️  Stopping services..."
docker-compose down

# Pull latest changes
echo "📥 Fetching updates..."
git pull origin main

# Rebuild images
echo "🔨 Rebuilding images..."
make docker-build

# Restart services
echo "▶️  Restarting services..."
make docker-run

# Verify everything works
echo "🔍 Post-update verification..."
sleep 15
./scripts/monitor.sh

echo "🎉 Update completed!"