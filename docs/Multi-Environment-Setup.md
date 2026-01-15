# Multi-Environment Setup Guide

**Orkestra Professional Management System**

## Overview

This guide outlines the implementation of separate **development**, **staging**, and **production** environments for Orkestra with OAuth callback compatibility across all environments.

---

## Environment Architecture

| Environment | Domain | Purpose | OAuth Apps | Database |
|------------|--------|---------|------------|----------|
| **Development** | `localhost:3000/8080` | Local development | Shared dev credentials | Local Docker |
| **Staging** | `staging.orkestra.cc` | QA testing | Shared dev credentials | Cloud/isolated |
| **Production** | `orkestra.cc` | Live system | Separate prod credentials | Dedicated with HA |

---

## Proxmox Deployment Architecture

### Recommended: 3 Separate VMs

For **Proxmox deployments with abundant resources** (>64GB RAM, >16 cores), the recommended architecture is **3 separate VMs** for complete environment isolation.

```
┌──────────────────────────────────────────────────────────────────┐
│                        Proxmox Host                               │
├──────────────────┬──────────────────┬────────────────────────────┤
│   Dev VM         │   Staging VM     │   Production VM            │
│   VM ID: 100     │   VM ID: 101     │   VM ID: 102               │
├──────────────────┼──────────────────┼────────────────────────────┤
│ CPU: 4 cores     │ CPU: 6 cores     │ CPU: 8 cores               │
│ RAM: 8GB         │ RAM: 12GB        │ RAM: 20GB                  │
│ Disk: 80GB       │ Disk: 100GB      │ Disk: 200GB                │
│ Network: vmbr1   │ Network: vmbr1   │ Network: vmbr0 + vmbr2     │
│ (internal LAN)   │ (internal LAN)   │ (public + isolated)        │
│                  │                  │                            │
│ Auto-start: No   │ Auto-start: Yes  │ Auto-start: Yes            │
│ Backup: Weekly   │ Backup: Weekly   │ Backup: Daily + Offsite    │
└──────────────────┴──────────────────┴────────────────────────────┘
  dev.orkestra.local  staging.orkestra.cc  orkestra.cc
```

**Total Resources:** 18 cores, 40GB RAM, 380GB disk

### Why 3 VMs?

✅ **Complete Production Isolation** - Production never affected by dev/staging
✅ **Resource Guarantees** - Each environment gets dedicated CPU/RAM
✅ **Security** - Separate network zones, firewall rules, and secrets
✅ **Realistic Staging** - Staging matches production architecture exactly
✅ **Independent Scaling** - Scale production without affecting others
✅ **Proxmox Features** - Easy snapshots, backups, and live migration per VM
✅ **Compliance Ready** - Clean separation for auditing and regulations

### VM Configuration Scripts

#### Create Development VM (VM 100)

```bash
# Create from Ubuntu 24.04 template (assumes template ID: 9000)
qm clone 9000 100 --name orkestra-dev --full

# Configure resources
qm set 100 \
  --cores 4 \
  --memory 8192 \
  --balloon 0 \
  --scsihw virtio-scsi-pci \
  --scsi0 local-lvm:80 \
  --net0 virtio,bridge=vmbr1,firewall=1 \
  --ipconfig0 ip=10.0.1.100/24,gw=10.0.1.1 \
  --nameserver 10.0.1.1 \
  --onboot 0 \
  --description "Orkestra Development Environment"

# Start VM
qm start 100
```

**Purpose:** Local development and experimentation
**Access:** Internal network only (VPN or jumphost required)
**Domain:** `dev.orkestra.local` (internal DNS or `/etc/hosts`)
**Backups:** Weekly snapshot (data not critical)

#### Create Staging VM (VM 101)

```bash
# Create from template
qm clone 9000 101 --name orkestra-staging --full

# Configure resources (mirrors production architecture)
qm set 101 \
  --cores 6 \
  --memory 12288 \
  --balloon 0 \
  --scsihw virtio-scsi-pci \
  --scsi0 local-lvm:100 \
  --net0 virtio,bridge=vmbr1,firewall=1 \
  --ipconfig0 ip=10.0.1.101/24,gw=10.0.1.1 \
  --nameserver 1.1.1.1,8.8.8.8 \
  --onboot 1 \
  --description "Orkestra Staging Environment (Pre-Production Testing)"

# Optional: Add public interface for external QA testing
# qm set 101 --net1 virtio,bridge=vmbr0,firewall=1

qm start 101
```

**Purpose:** Pre-production testing and QA validation
**Access:** Internal + optional public DNS (`staging.orkestra.cc`)
**SSL:** Let's Encrypt certificate
**Backups:** Weekly full backup + monthly archival

#### Create Production VM (VM 102)

```bash
# Create from template
qm clone 9000 102 --name orkestra-prod --full

# Configure resources (prioritized and protected)
qm set 102 \
  --cores 8 \
  --memory 20480 \
  --balloon 0 \
  --cpu host \
  --cpuunits 2048 \
  --scsihw virtio-scsi-pci \
  --scsi0 local-lvm:200 \
  --net0 virtio,bridge=vmbr0,firewall=1 \
  --net1 virtio,bridge=vmbr2,firewall=1 \
  --nameserver 1.1.1.1,8.8.8.8 \
  --onboot 1 \
  --protection 1 \
  --description "Orkestra Production Environment - PROTECTED"

# Enable in Proxmox HA (if cluster available)
# ha-manager add vm:102 --state started --max_relocate 1

qm start 102
```

**Purpose:** Live production application serving real users
**Access:**
- `vmbr0` (public internet) - Ports 80/443 only
- `vmbr2` (isolated backend) - Database/cache communication
**SSL:** Commercial certificate or Let's Encrypt
**Protection:** VM deletion protection enabled
**Backups:** Daily automated + weekly offsite replication

### Network Architecture

```
                    Internet
                       │
                       │
              ┌────────▼────────┐
              │  Proxmox Firewall│
              │  or Load Balancer│
              └────────┬─────────┘
                       │
          ┌────────────┼────────────┐
          │            │            │
    ┌─────▼────┐  ┌───▼────┐  ┌───▼─────────┐
    │  Dev VM  │  │ Stage  │  │    Prod     │
    │  (vmbr1) │  │ VM     │  │    VM       │
    │          │  │(vmbr1) │  │  (vmbr0 +   │
    │          │  │        │  │   vmbr2)    │
    └──────────┘  └────────┘  └─────┬───────┘
                                     │
                              ┌──────▼─────┐
                              │  Isolated  │
                              │  Backend   │
                              │  Network   │
                              └────────────┘
```

#### VLAN Configuration

**vmbr0 (WAN/Public)** - Production frontend only
- Proxmox firewall: Allow 80, 443, 22 (SSH from admin IPs)
- Direct internet access
- Public IP address or NAT

**vmbr1 (LAN/Internal)** - Dev, Staging, Management
- Internal network: `10.0.1.0/24`
- Accessible via VPN or jumphost
- No direct internet routing
- Can access internet via Proxmox NAT (outbound only)

**vmbr2 (Backend/Isolated)** - Production databases only
- Internal network: `10.0.2.0/24`
- No internet access (fully isolated)
- Only Production VM connected
- MongoDB, Redis, internal services

#### Firewall Rules (Proxmox)

```bash
# Production VM (102) - Public interface (vmbr0)
# Allow HTTPS
IN ACCEPT -p tcp -dport 443 -source <cloudflare-ips>
# Allow HTTP (redirect to HTTPS)
IN ACCEPT -p tcp -dport 80
# Allow SSH from admin IPs only
IN ACCEPT -p tcp -dport 22 -source 203.0.113.0/24
# Drop all other inbound
IN DROP

# Staging VM (101) - Internal only
# Allow all from internal network
IN ACCEPT -source 10.0.1.0/24
# Drop all external
IN DROP

# Dev VM (100) - Internal only
# Allow all from internal network
IN ACCEPT -source 10.0.1.0/24
# Drop all external
IN DROP
```

### Backup Strategy

#### Proxmox Backup Schedule

```bash
# Production (VM 102): Daily backups, keep 7 days
# Via Proxmox UI: Datacenter > Backup > Add
# Or via CLI:
vzdump 102 \
  --mode snapshot \
  --storage backup-pool \
  --compress zstd \
  --mailnotification always \
  --maxfiles 7 \
  --notes "Daily production backup"

# Staging (VM 101): Weekly backups, keep 4 weeks
vzdump 101 \
  --mode snapshot \
  --storage backup-pool \
  --compress zstd \
  --maxfiles 4 \
  --notes "Weekly staging backup"

# Dev (VM 100): Weekly snapshots, keep 2
vzdump 100 \
  --mode snapshot \
  --storage backup-pool \
  --compress zstd \
  --maxfiles 2 \
  --notes "Dev environment backup"
```

#### Automated Backup Script

```bash
#!/bin/bash
# /usr/local/bin/orkestra-backup.sh

# Production - Daily at 2 AM
0 2 * * * root vzdump 102 --mode snapshot --storage backup-pool --compress zstd --mailnotification always --maxfiles 7

# Staging - Weekly on Sunday at 3 AM
0 3 * * 0 root vzdump 101 --mode snapshot --storage backup-pool --compress zstd --maxfiles 4

# Dev - Weekly on Sunday at 4 AM
0 4 * * 0 root vzdump 100 --mode snapshot --storage backup-pool --compress zstd --maxfiles 2

# Offsite replication (production only) - Daily at 5 AM
0 5 * * * root rclone sync /var/lib/vz/dump/ remote:orkestra-backups/ --include "vzdump-qemu-102-*"
```

### Pre-Deployment Snapshots

```bash
# Before any deployment, take a snapshot

# Development (before testing risky changes)
qm snapshot 100 "pre-deploy-$(date +%Y%m%d-%H%M)" --description "Before feature X deployment"

# Staging (before deployment)
qm snapshot 101 "pre-deploy-v1.2.0" --description "Before v1.2.0 staging deployment"

# Production (before production deployment)
qm snapshot 102 "pre-deploy-v1.2.0-PROD" --description "Before v1.2.0 production release"

# Rollback if needed
qm rollback 102 "pre-deploy-v1.2.0-PROD"
qm start 102
```

### Resource Monitoring

```bash
# Monitor VM resource usage
qm status 102  # Check if running

# Real-time resource monitoring
watch 'qm status 100 && qm status 101 && qm status 102'

# Detailed resource info
pvesh get /nodes/proxmox-node/qemu/102/status/current

# CPU and memory graphs in Proxmox UI:
# VM 102 > Summary > Resource Usage
```

### Migration from Single Server

If you're currently running all environments on a single server/VM, here's the migration path:

#### Week 1: Create VMs
```bash
# 1. Create all 3 VMs (see commands above)
# 2. Install Docker on each VM
ssh root@10.0.1.100 'curl -fsSL https://get.docker.com | sh'
ssh root@10.0.1.101 'curl -fsSL https://get.docker.com | sh'
ssh root@production-ip 'curl -fsSL https://get.docker.com | sh'

# 3. Clone Orkestra repo to each VM
for vm in 10.0.1.100 10.0.1.101 production-ip; do
  ssh root@$vm 'git clone https://github.com/your-org/orkestra.git /opt/orkestra'
done
```

#### Week 2: Set Up Dev & Staging
```bash
# Dev VM (100)
ssh root@10.0.1.100
cd /opt/orkestra
cp docker/.env.template docker/.env.dev
# Configure dev-specific values
docker compose -f docker/environments/dev/docker-compose.yml up -d

# Staging VM (101)
ssh root@10.0.1.101
cd /opt/orkestra
cp docker/.env.template docker/.env.staging
# Configure staging-specific values
# Copy staging database from current server
mongodump --uri="mongodb://old-server/orkestra_staging" --archive | \
  ssh root@10.0.1.101 'mongorestore --archive --uri="mongodb://localhost/orkestra_staging"'
docker compose -f docker/environments/staging/docker-compose.yml up -d
```

#### Week 3: Migrate Production
```bash
# 1. Schedule maintenance window (2 hours)
# 2. Take final backup of current production
mongodump --uri="mongodb://old-server/orkestra_prod" --out=/backups/final-prod-backup

# 3. Copy to new production VM
scp -r /backups/final-prod-backup root@production-ip:/tmp/

# 4. Deploy to new production VM
ssh root@production-ip
cd /opt/orkestra
cp docker/.env.template docker/.env.prod
# Configure production values with NEW OAuth credentials
mongorestore --uri="mongodb://localhost/orkestra_prod" /tmp/final-prod-backup/orkestra_prod
docker compose -f docker/environments/prod/docker-compose.yml up -d

# 5. Update DNS: orkestra.cc → new production IP
# 6. Monitor for 24 hours
# 7. Decommission old server
```

### Cost Analysis

| Aspect | Single Server | 3 VMs on Proxmox |
|--------|--------------|------------------|
| **CPU Usage** | 12 cores | 18 cores |
| **RAM Usage** | 24GB | 40GB |
| **Disk Usage** | 100GB | 380GB |
| **Management Time/Week** | 2 hours | 4 hours |
| **Security Score** | 3/10 | 9/10 |
| **Production Safety** | Low | Very High |
| **Scalability** | Limited | Excellent |

**Verdict:** With >64GB RAM and >16 cores available, the additional resource usage (18 cores, 40GB) is negligible compared to the massive improvements in security, isolation, and manageability.

---

## Quick Start

### Prerequisites

```bash
# Install required tools
- Docker & Docker Compose
- Go 1.25.1+
- Node.js 18+
- Git

# Clone repository
git clone <repository-url>
cd orkestra
```

### Development Setup

```bash
# 1. Create environment file from template
cp docker/.env.template docker/.env.dev

# 2. Generate JWT keys
mkdir -p backend/keys
ssh-keygen -t rsa -b 4096 -m pem -f backend/keys/jwt-private.pem -N ""
openssl rsa -in backend/keys/jwt-private.pem -pubout -out backend/keys/jwt-public.pem

# 3. Configure OAuth providers (see OAuth Setup section)
# Edit docker/.env.dev with your OAuth credentials

# 4. Start development environment
cd docker/environments/dev
docker compose up -d

# 5. Access applications
# Frontend:  http://localhost:8080
# Backend:   http://localhost:3000
# API Docs:  http://localhost:3000/docs
```

---

## File Structure

```
orkestra/
├── docker/
│   ├── .env.template              # Base template (committed)
│   ├── .env.dev                   # Development config (committed)
│   ├── .env.staging               # Staging config (NOT committed)
│   ├── .env.prod                  # Production config (NOT committed)
│   └── environments/
│       ├── dev/
│       │   └── docker-compose.yml
│       ├── staging/
│       │   ├── docker-compose.yml
│       │   ├── nginx.conf
│       │   └── ssl/
│       └── prod/
│           ├── docker-compose.yml
│           └── k8s/
├── frontend/
│   ├── .env                       # Base defaults (committed)
│   ├── .env.development           # Dev config (committed)
│   ├── .env.staging               # Staging config (NOT committed)
│   ├── .env.production            # Prod config (NOT committed)
│   └── src/
│       └── config/
│           ├── index.ts
│           ├── environment.ts
│           ├── api.config.ts
│           ├── oauth.config.ts
│           └── features.config.ts
└── backend/
    └── internal/
        └── shared/
            └── config/
                └── config.go      # Enhanced with env support
```

---

## OAuth Provider Setup

### Google OAuth

**Google Cloud Console:**
1. Create OAuth 2.0 Client ID
2. Add authorized redirect URIs:
   - `http://localhost:3000/api/v1/auth/oauth/google/callback` (dev)
   - `https://staging.orkestra.cc/api/v1/auth/oauth/google/callback` (staging)
   - `https://orkestra.cc/api/v1/auth/oauth/google/callback` (prod)

**Configuration:**
```bash
# .env.dev
OAUTH_GOOGLE_CLIENT_ID=your-client-id.apps.googleusercontent.com
OAUTH_GOOGLE_CLIENT_SECRET=your-client-secret
OAUTH_GOOGLE_REDIRECT_URL=http://localhost:3000/api/v1/auth/oauth/google/callback

# .env.staging
OAUTH_GOOGLE_REDIRECT_URL=https://staging.orkestra.cc/api/v1/auth/oauth/google/callback

# .env.prod (separate OAuth app recommended)
OAUTH_GOOGLE_CLIENT_ID=prod-client-id.apps.googleusercontent.com
OAUTH_GOOGLE_CLIENT_SECRET=prod-secret
OAUTH_GOOGLE_REDIRECT_URL=https://orkestra.cc/api/v1/auth/oauth/google/callback
```

### Apple Sign-In

**Apple Developer Portal:**
1. Create App ID: `cc.orkestra.webapp`
2. Create Service ID: `cc.orkestra.webapp.signin`
3. Add domains and redirect URIs (note: use ngrok for localhost)
4. Generate Sign-in Key, download `.p8` file

**Configuration:**
```bash
OAUTH_APPLE_TEAM_ID=ABC123DEF4
OAUTH_APPLE_CLIENT_ID=cc.orkestra.webapp.signin
OAUTH_APPLE_KEY_ID=XYZ987WVU6
OAUTH_APPLE_PRIVATE_KEY_PATH=/app/keys/AuthKey_XYZ987WVU6.p8
OAUTH_APPLE_REDIRECT_URL=https://staging.orkestra.cc/api/v1/auth/oauth/apple/callback
```

### Discord & GitHub

Similar setup - create OAuth apps in respective developer portals and add redirect URIs for each environment.

---

## Environment-Specific Configuration

### Backend Configuration

**Enhanced `config.go`:**
```go
type Environment string

const (
    EnvironmentDev     Environment = "dev"
    EnvironmentStaging Environment = "staging"
    EnvironmentProd    Environment = "prod"
)

type Config struct {
    Environment Environment
    Server      ServerConfig
    Database    DatabaseConfig
    Redis       RedisConfig
    Auth        AuthConfig
    Features    FeatureFlags    // New: environment-specific features
    Logging     LoggingConfig   // New: environment-specific logging
    Monitoring  MonitoringConfig // New: Sentry, Prometheus
}
```

**Environment Loading Hierarchy:**
1. `.env.template` - Base defaults
2. `.env.{environment}` - Environment-specific
3. `.env.local` - Local overrides (dev only)
4. System environment variables

### Frontend Configuration

**Vite Environment Files:**
- `.env` - Base defaults
- `.env.development` - Dev-specific
- `.env.staging` - Staging-specific
- `.env.production` - Prod-specific

**TypeScript Config Module:**
```typescript
// src/config/index.ts
export const config = {
    environment: getCurrentEnvironment(),
    api: {
        baseURL: import.meta.env.VITE_BACKEND_URL,
        wsURL: import.meta.env.VITE_WS_URL,
    },
    oauth: {
        redirectBase: import.meta.env.VITE_OAUTH_REDIRECT_BASE,
    },
    features: {
        debugTools: import.meta.env.VITE_FEATURE_DEBUG_TOOLS === 'true',
        analytics: import.meta.env.VITE_FEATURE_ANALYTICS === 'true',
    },
};
```

---

## Database Strategy

### Recommended Approach (Hybrid)

| Environment | MongoDB | Redis | Backup Strategy |
|------------|---------|-------|-----------------|
| **Dev** | Docker `orkestra_dev` | Docker DB 0 | None (local data) |
| **Staging** | Cloud `orkestra_staging` | Cloud DB 0 | Daily snapshots |
| **Production** | Replica set (3 nodes) | Clustered | Real-time + daily |

### Connection Strings

```bash
# Development
MONGO_HOST=mongodb
MONGO_PORT=27017
MONGO_DATABASE=orkestra_dev

# Staging
MONGO_HOST=staging-mongo.orkestra.internal
MONGO_DATABASE=orkestra_staging
MONGO_REPLICA_SET=staging-rs

# Production
MONGO_HOST=prod-mongo-1,prod-mongo-2,prod-mongo-3
MONGO_DATABASE=orkestra_prod
MONGO_REPLICA_SET=prod-rs
```

---

## Deployment Workflows

### Development
- **Trigger:** Local changes
- **Process:** Hot reload (AIR for backend, Vite for frontend)
- **Deployment:** None (runs on developer machine)

### Staging
- **Trigger:** Push to `develop` branch
- **Process:**
  1. Build Docker images
  2. Push to container registry
  3. SSH to staging server
  4. Pull and restart services
  5. Run smoke tests
- **Tool:** GitHub Actions

### Production
- **Trigger:** Git tag `v*.*.*`
- **Process:**
  1. Manual approval required
  2. Build and security scan images
  3. Deploy to Kubernetes with rolling update
  4. Health checks
  5. Automated rollback on failure
- **Tool:** GitHub Actions + Kubernetes

---

## Implementation Checklist

### Phase 1: Configuration Setup
- [ ] Create `.env.template`, `.env.dev`, `.env.staging`, `.env.prod`
- [ ] Update backend `config.go` with environment support
- [ ] Create frontend config module (`src/config/`)
- [ ] Update `.gitignore` to exclude sensitive env files

### Phase 2: OAuth Provider Setup
- [ ] Configure Google OAuth with 3 redirect URIs
- [ ] Configure Apple Sign-In with 3 redirect URIs
- [ ] Configure Discord OAuth
- [ ] Configure GitHub OAuth
- [ ] Test OAuth flows in each environment

### Phase 3: Docker Infrastructure
- [ ] Create `docker/environments/dev/docker-compose.yml`
- [ ] Create `docker/environments/staging/docker-compose.yml`
- [ ] Create staging Nginx configuration
- [ ] Set up SSL certificates (Let's Encrypt)
- [ ] Create deployment scripts

### Phase 4: Database Setup
- [ ] Configure local MongoDB/Redis (dev)
- [ ] Set up cloud MongoDB/Redis (staging)
- [ ] Set up production replica set
- [ ] Configure backup automation
- [ ] Test database connectivity

### Phase 5: CI/CD Pipelines
- [ ] Create GitHub Actions workflows
- [ ] Configure GitHub secrets
- [ ] Set up Docker registry
- [ ] Test automated deployments
- [ ] Configure deployment notifications

### Phase 6: Monitoring
- [ ] Set up Sentry for error tracking
- [ ] Configure structured logging
- [ ] Set up Prometheus metrics
- [ ] Create Grafana dashboards
- [ ] Configure uptime monitoring

### Phase 7: Security
- [ ] Implement secret management (Vault)
- [ ] Enforce HTTPS (staging/prod)
- [ ] Add security headers
- [ ] Configure rate limiting
- [ ] Implement secret rotation

### Phase 8: Testing
- [ ] Test all OAuth flows per environment
- [ ] Verify API authentication
- [ ] Test WebSocket connections
- [ ] Run load tests
- [ ] Validate monitoring/alerts

---

## Environment Variables Reference

### Critical Variables

```bash
# Environment
ENVIRONMENT=dev|staging|prod

# Server
BACKEND_URL=http://localhost:3000
FRONTEND_URL=http://localhost:8080

# Database
MONGO_HOST=mongodb
MONGO_DATABASE=orkestra_dev
MONGO_REPLICA_SET=          # Empty for dev/staging, set for prod

# Redis
REDIS_HOST=redis
REDIS_DB=0
REDIS_CLUSTER_MODE=false    # true for production

# OAuth - Google
OAUTH_GOOGLE_CLIENT_ID=
OAUTH_GOOGLE_CLIENT_SECRET=
OAUTH_GOOGLE_REDIRECT_URL=

# OAuth - Apple
OAUTH_APPLE_TEAM_ID=
OAUTH_APPLE_CLIENT_ID=
OAUTH_APPLE_KEY_ID=
OAUTH_APPLE_PRIVATE_KEY_PATH=
OAUTH_APPLE_REDIRECT_URL=

# Feature Flags
FEATURE_DEBUG_ENDPOINTS=false    # true for dev only
FEATURE_METRICS=true
FEATURE_MAINTENANCE_MODE=false

# Monitoring
MONITORING_ENABLE=false          # true for staging/prod
SENTRY_DSN=
SENTRY_ENVIRONMENT=
```

---

## Troubleshooting

### OAuth Callback Not Working

**Symptom:** OAuth provider returns "redirect_uri_mismatch" error

**Solutions:**
1. Verify `OAUTH_*_REDIRECT_URL` matches provider configuration exactly
2. Check protocol (http vs https) - staging/prod must use https
3. For Apple: localhost requires ngrok tunnel
4. Clear browser cookies and retry

### Environment Not Loading

**Symptom:** Application uses wrong configuration

**Solutions:**
1. Check `ENVIRONMENT` variable is set correctly
2. Verify `.env.{environment}` file exists
3. Check file loading order in `config.go`
4. Restart services after env changes

### Database Connection Failed

**Symptom:** "connection refused" or "authentication failed"

**Solutions:**
1. Verify MongoDB/Redis containers are running
2. Check connection strings in environment file
3. For cloud databases, verify network access rules
4. Test connection manually with `mongosh` or `redis-cli`

---

## Security Best Practices

1. **Never commit** `.env.staging` or `.env.prod` to version control
2. **Use separate OAuth apps** for production
3. **Rotate secrets regularly**:
   - OAuth secrets: 90 days
   - Database passwords: 60 days
   - JWT keys: 180 days
4. **Enable 2FA** for all admin accounts
5. **Monitor** failed authentication attempts
6. **Backup** production database daily
7. **Use Vault** for production secrets
8. **Audit** access logs regularly

---

## Useful Commands

### Development
```bash
# Start environment
cd docker/environments/dev && docker compose up -d

# View logs
docker compose logs -f backend
docker compose logs -f frontend

# Restart services
docker compose restart backend

# Stop environment
docker compose down
```

### Staging/Production
```bash
# Deploy to staging
./scripts/staging/deploy.sh

# View logs
docker compose -f docker/environments/staging/docker-compose.yml logs -f

# Rollback
docker compose -f docker/environments/staging/docker-compose.yml down
git checkout <previous-commit>
./scripts/staging/deploy.sh
```

### Database
```bash
# Connect to MongoDB
mongosh "mongodb://admin:password@localhost:27017/orkestra_dev?authSource=admin"

# Connect to Redis
redis-cli -h localhost -p 6379 -a password

# Backup production database
mongodump --uri="mongodb://user:pass@prod-mongo/orkestra_prod" --out=/backups/$(date +%Y%m%d)
```

---

## Additional Resources

- [Authentication Flow Documentation](./Authentication_flow.md)
- [Backend Module Documentation](../backend/CLAUDE.md)
- [Frontend Module Documentation](../frontend/CLAUDE.md)
- [Docker Module Documentation](../docker/CLAUDE.md)

---

## Support

For questions or issues:
1. Check this documentation
2. Review module-specific CLAUDE.md files
3. Check application logs
4. Contact DevOps team

---

**Last Updated:** 2025-11-13
**Version:** 1.0
**Maintained By:** DevOps Team
