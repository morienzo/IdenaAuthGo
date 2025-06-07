#!/bin/bash
# scripts/setup-production.sh

set -e

echo "🏭 Setting up for production..."

# Create necessary directories
mkdir -p data logs backups

# Generate secure keys
API_KEY=$(openssl rand -hex 32)
JWT_SECRET=$(openssl rand -hex 64)

# Create production .env file
cat > .env.prod << EOF
# Production Configuration
BASE_URL=https://your-domain.com
IDENA_RPC_KEY=your_production_rpc_key
PORT=3030

# Indexer
RPC_URL=https://your-idena-node.com:9009
FETCH_INTERVAL_MINUTES=5
DB_PATH=/app/data/identities.db

# Security
API_KEY=$API_KEY
JWT_SECRET=$JWT_SECRET

# Monitoring
LOG_LEVEL=info
METRICS_ENABLED=true

# Performance
MAX_CONNECTIONS=25
CACHE_TTL=300
EOF

echo "✅ .env.prod file created"

# Configuration Nginx
cat > nginx.conf << 'EOF'
upstream idena_backend {
    server localhost:3030;
}

upstream idena_indexer {
    server localhost:8080;
}

server {
    listen 80;
    server_name your-domain.com;
    return 301 https://$server_name$request_uri;
}

server {
    listen 443 ssl http2;
    server_name your-domain.com;

    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    # Security headers
    add_header X-Frame-Options DENY;
    add_header X-Content-Type-Options nosniff;
    add_header X-XSS-Protection "1; mode=block";

    # Rate limiting
    limit_req_zone $binary_remote_addr zone=api:10m rate=10r/s;

    location / {
        limit_req zone=api burst=20 nodelay;
        proxy_pass http://idena_backend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location /api/indexer/ {
        proxy_pass http://idena_indexer/;
        proxy_set_header Host $host;
    }
}
EOF

echo "✅ Nginx configuration created"

# systemd script
cat > idena-auth.service << 'EOF'
[Unit]
Description=Idena Auth Service
After=network.target

[Service]
Type=simple
User=idena
WorkingDirectory=/opt/idena-auth
ExecStart=/opt/idena-auth/main
Restart=always
RestartSec=10
Environment=ENVIRONMENT=production

[Install]
WantedBy=multi-user.target
EOF

echo "✅ systemd service created"

# Crontab for backups
echo "0 2 * * * /opt/idena-auth/scripts/backup.sh" > idena-crontab
echo "✅ Backup crontab created"

# Final instructions
cat << 'EOF'

🎯 Production setup completed!

Next steps:
1. Edit .env.prod with your actual values
2. Configure SSL/TLS for Nginx
3. Copy idena-auth.service to /etc/systemd/system/
4. Install crontab: crontab idena-crontab
5. Configure monitoring (Prometheus/Grafana)

To deploy:
    sudo systemctl enable idena-auth
    sudo systemctl start idena-auth
    sudo nginx -t && sudo systemctl reload nginx

EOF