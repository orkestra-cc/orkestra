# Orkestra Backend

## Overview

Monolithic Go backend server for orkestra with OAuth 2.1 authentication via Google and Apple Sign In.

## Prerequisites

- Go 1.25.1+
- MongoDB 8.2
- Redis 8.2
- Google OAuth credentials
- Apple Developer account for Sign In (optional)

## Setup

### 1. Environment Configuration

Copy the example environment file and configure:

```bash
cp .env.example .env
```

Edit `.env` with your configuration:

- MongoDB connection string
- Redis connection string
- JWT secrets (generate secure random strings)
- Google OAuth credentials
- Apple Sign In credentials (if using)

### 2. Google OAuth Setup

1. Go to [Google Cloud Console](https://console.cloud.google.com)
2. Create a new project or select existing
3. Enable Google+ API
4. Create OAuth 2.0 credentials:
   - Application type: Web application
   - Authorized redirect URI: `http://localhost:3000/auth/oauth/google/callback`
5. Copy Client ID and Client Secret to `.env`

### 3. Apple Sign In Setup (Optional)

1. Register your app in Apple Developer account
2. Create a Service ID for Sign In with Apple
3. Generate a private key for Sign In with Apple
4. Configure redirect URL: `http://localhost:3000/auth/oauth/apple/callback`
5. Add credentials to `.env`

### 4. Install Dependencies

```bash
make deps
```

## Running the Server

### Development Mode

```bash
make dev
```

This runs the server with hot reload using Air.

### Standard Run

```bash
make run
```

### Build and Run

```bash
make build
./bin/server
```

## API Endpoints

### Authentication

- `GET /auth/oauth/google` - Initiate Google OAuth login
- `GET /auth/oauth/apple` - Initiate Apple Sign In
- `GET /auth/oauth/google/callback` - Google OAuth callback
- `POST /auth/oauth/apple/callback` - Apple Sign In callback
- `POST /auth/refresh` - Refresh access token
- `POST /auth/logout` - Logout user
- `GET /auth/me` - Get current user (requires auth)

### Health Checks

- `GET /health` - Health status
- `GET /ready` - Readiness check

## Authentication Flow

1. **Initiate OAuth**: Frontend redirects to `/auth/oauth/{provider}`
2. **User Authorization**: User authorizes on Google/Apple
3. **Callback**: Provider redirects back with authorization code
4. **Token Exchange**: Backend exchanges code for user info
5. **JWT Generation**: Backend creates JWT tokens
6. **Response**: Frontend receives access & refresh tokens

## JWT Token Structure

### Access Token (15 minutes)

```json
{
  "sub": "user_id",
  "email": "user@example.com",
  "role": "viewer",
  "provider": "google",
  "type": "access",
  "exp": 1234567890,
  "iat": 1234567890
}
```

### Refresh Token (7 days)

```json
{
  "sub": "user_id",
  "type": "refresh",
  "exp": 1234567890,
  "iat": 1234567890
}
```

## User Roles

- `admin` - Full system access
- `manager` - Manage operations and users
- `operator` - Operational tasks
- `viewer` - Read-only access

## Testing

```bash
make test
```

## Docker Support

Build image:

```bash
make docker-build
```

Run container:

```bash
make docker-run
```

## Architecture

The backend follows a clean architecture pattern:

```
internal/
├── auth/           # Authentication module
│   ├── handlers/   # HTTP handlers
│   ├── services/   # Business logic
│   ├── repository/ # Data access
│   └── models/     # Data models
└── shared/         # Shared components
    ├── config/     # Configuration
    ├── database/   # Database connections
    └── middleware/ # HTTP middleware
```

## Security Features

- OAuth 2.0 with Google and Apple
- JWT with refresh token rotation
- Role-based access control (RBAC)
- CORS protection
- Rate limiting
- Input validation
- Secure session storage in Redis

## Monitoring

- OpenTelemetry integration ready
- Structured logging with slog
- Health and readiness endpoints
- Metrics endpoint (coming soon)

## Contributing

See the main project README for contribution guidelines.
