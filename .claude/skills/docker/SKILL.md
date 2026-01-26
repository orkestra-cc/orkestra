---
name: docker
description: Execute Docker Compose operations with correct environment detection. Use for restart, logs, up, down, and other docker compose commands.
---

# Docker Operations Skill

This skill ensures correct Docker Compose operations by ALWAYS checking the environment first.

## MANDATORY First Step

**BEFORE ANY docker command, you MUST run this:**

```bash
grep "^ENV=" /home/tore/orkestra/docker/.env
```

This returns the current environment: `development`, `staging`, or `production`.

## Environment to Compose File Mapping

| ENV Value     | Compose File                  |
|---------------|-------------------------------|
| `development` | `docker-compose.dev.yml`      |
| `staging`     | `docker-compose.staging.yml`  |
| `production`  | `docker-compose.prod.yml`     |

## Command Format

**ALL docker compose commands MUST use this format:**

```bash
docker compose -f docker-compose.{ENV}.yml --env-file .env <command> <service>
```

Working directory: `/home/tore/orkestra/docker`

## Instructions

### Step 1: Check Environment (MANDATORY)

```bash
grep "^ENV=" /home/tore/orkestra/docker/.env
```

### Step 2: Execute Command

Based on the ENV value, run the appropriate command:

**For staging:**
```bash
docker compose -f docker-compose.staging.yml --env-file .env <command> <service>
```

**For development:**
```bash
docker compose -f docker-compose.dev.yml --env-file .env <command> <service>
```

**For production:**
```bash
docker compose -f docker-compose.prod.yml --env-file .env <command> <service>
```

## Common Operations

### Restart a service
```bash
docker compose -f docker-compose.{ENV}.yml --env-file .env restart <service>
```

### View logs
```bash
docker compose -f docker-compose.{ENV}.yml --env-file .env logs --tail 50 <service>
```

### Follow logs
```bash
docker compose -f docker-compose.{ENV}.yml --env-file .env logs -f <service>
```

### Check status
```bash
docker compose -f docker-compose.{ENV}.yml --env-file .env ps
```

### Rebuild and restart
```bash
docker compose -f docker-compose.{ENV}.yml --env-file .env up -d --build <service>
```

## Service Names

- `backend` - Go API server
- `frontend` - React web app

## Infrastructure Services (separate compose file)

Infrastructure uses `docker-compose.infra.yml`:
```bash
docker compose -f docker-compose.infra.yml --env-file .env <command> <service>
```

Services: `orkestra-mongodb`, `orkestra-redis`, `orkestra-gotenberg`

## NEVER Do This

- NEVER run `docker restart <container>` directly
- NEVER guess the compose file without checking ENV first
- NEVER omit `--env-file .env`
- NEVER run docker compose from outside `/home/tore/orkestra/docker`
