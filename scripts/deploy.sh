#!/bin/bash
# scripts/deploy.sh - Automated deployment script

set -e

# Local configuration
DOCKER_REGISTRY=${DOCKER_REGISTRY:-"your-registry.com"}
IMAGE_TAG=${IMAGE_TAG:-"latest"}
ENVIRONMENT=${ENVIRONMENT:-"production"}

echo "🚀 Deploying IdenaAuthGo - Environment: $ENVIRONMENT"

# Pre-deployment checks
echo "📋 Pre-deployment checks..."
command -v docker >/dev/null 2>&1 || { echo "❌ Docker required"; exit 1; }
command -v docker-compose >/dev/null 2>&1 || { echo "❌ Docker Compose required"; exit 1; }

# Build Docker images
echo "🔨 Building Docker images..."
docker build -t $DOCKER_REGISTRY/idena-auth:$IMAGE_TAG .
docker build -t $DOCKER_REGISTRY/idena-indexer:$IMAGE_TAG -f Dockerfile.indexer .

# Push to registry (if specified)
if [[ "$DOCKER_REGISTRY" != "your-registry.com" ]]; then
    echo "📤 Pushing to registry..."
    docker push $DOCKER_REGISTRY/idena-auth:$IMAGE_TAG
    docker push $DOCKER_REGISTRY/idena-indexer:$IMAGE_TAG
fi

# Deploy
echo "🎯 Deploying..."
if [[ "$ENVIRONMENT" == "production" ]]; then
    docker-compose -f docker-compose.prod.yml up -d
else
    docker-compose up -d
fi

# Deployment verification
echo "🔍 Verifying deployment..."
sleep 10

# Health check
if curl -f http://localhost:3030/health >/dev/null 2>&1; then
    echo "✅ Main backend: OK"
else
    echo "❌ Main backend: FAILED"
    exit 1
fi

if curl -f http://localhost:8080/identities/latest >/dev/null 2>&1; then
    echo "✅ Indexer: OK"
else
    echo "❌ Indexer: FAILED"
    exit 1
fi

echo "🎉 Deployment successful!"
echo "Backend: http://localhost:3030"
echo "Indexer API: http://localhost:8080"