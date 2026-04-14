# FUJU Backend - Architecture & Implementation Plan

## Overview

FUJU Backend is a SNS (Social Network Service) backend built in Go following Clean Architecture principles. This document outlines the architecture, design patterns, and implementation strategy.

---

## Table of Contents

1. [Clean Architecture](#clean-architecture)
2. [Project Structure](#project-structure)
3. [Authentication Strategy](#authentication-strategy)
4. [Database Design](#database-design)
5. [API Design](#api-design)
6. [Error Handling](#error-handling)
7. [Logging & Monitoring](#logging--monitoring)
8. [Development Guidelines](#development-guidelines)

---

## Clean Architecture

### Layer Structure

```
┌─────────────────────────────────┐
│     Handler (HTTP/gRPC)         │  <- External Interface
├─────────────────────────────────┤
│   Middleware (Auth, Logging)    │  <- Cross-cutting Concerns
├─────────────────────────────────┤
│     Usecase (Business Logic)    │  <- Application Layer
├─────────────────────────────────┤
│  Repository (Data Abstraction)  │  <- Interface Adapter
├─────────────────────────────────┤
│  Domain (Entities & Rules)      │  <- Core Business Logic
├─────────────────────────────────┤
│  External (DB, Cache, OAuth)    │  <- External Systems
└─────────────────────────────────┘
```

### Layer Responsibilities

1. **Domain Layer** (`internal/domain/`)
   - Pure business entities and value objects
   - Domain-specific rules and validations
   - No external dependencies
   - Examples: `User`, `Post`, `Comment` entities

2. **Repository Layer** (`internal/repository/`)
   - Data persistence abstraction
   - Database queries and transactions
   - Implements interfaces defined in domain
   - Examples: `UserRepository`, `PostRepository`

3. **Usecase Layer** (`internal/usecase/`)
   - Business logic orchestration
   - Coordinates between repositories and domain
   - Transaction boundary management
   - Examples: `CreatePostUseCase`, `GetUserTimelineUseCase`

4. **Handler Layer** (`internal/handler/`)
   - HTTP request/response handling
   - Input validation and deserialization
   - Response serialization
   - Examples: `UserHandler`, `PostHandler`

5. **Middleware Layer** (`internal/middleware/`)
   - Cross-cutting concerns (Auth, Logging, CORS)
   - Request/Response interceptors

---

## Project Structure

```
backend/
├── cmd/
│   └── server/
│       └── main.go                    # Application entry point
├── internal/
│   ├── domain/
│   │   ├── user.go                    # User entity & interfaces
│   │   ├── post.go                    # Post entity & interfaces
│   │   ├── comment.go                 # Comment entity & interfaces
│   │   ├── repository.go              # Repository interfaces
│   │   └── errors.go                  # Domain-specific errors
│   ├── usecase/
│   │   ├── user/
│   │   │   ├── create_user.go
│   │   │   ├── get_user.go
│   │   │   └── list_users.go
│   │   ├── post/
│   │   │   ├── create_post.go
│   │   │   ├── get_post.go
│   │   │   └── list_posts.go
│   │   └── auth/
│   │       ├── oauth_callback.go
│   │       ├── create_session.go
│   │       └── refresh_token.go
│   ├── repository/
│   │   ├── user_repository.go         # PostgreSQL implementation
│   │   ├── post_repository.go
│   │   └── comment_repository.go
│   ├── handler/
│   │   ├── user_handler.go
│   │   ├── post_handler.go
│   │   ├── auth_handler.go
│   │   └── health_handler.go
│   └── middleware/
│       ├── auth.go                    # OAuth2 & JWT validation
│       ├── logging.go                 # Request logging
│       ├── cors.go                    # CORS handling
│       └── recovery.go                # Panic recovery
├── pkg/
│   ├── db/
│   │   ├── connection.go              # PostgreSQL connection pool
│   │   ├── transaction.go             # Transaction management
│   │   └── migration.go               # Database migrations
│   ├── logger/
│   │   ├── logger.go                  # Structured JSON logging
│   │   └── context.go                 # Context-aware logging
│   ├── errors/
│   │   ├── errors.go                  # Custom error types
│   │   └── http_errors.go             # HTTP error mapping
│   ├── cache/
│   │   ├── redis.go                   # Redis client wrapper
│   │   └── cache_key.go               # Cache key management
│   └── auth/
│       ├── oauth.go                   # OAuth2 client config
│       ├── jwt.go                     # JWT token handling
│       └── session.go                 # Session (Cookie) management
├── config/
│   └── config.go                      # Configuration management
├── docs/
│   ├── swagger.yaml                   # API specification
│   └── architecture.md                # This file
├── .github/workflows/
│   ├── ci.yml                         # CI/CD pipeline
│   └── pr-review.yml                  # AI-powered PR review
├── Makefile                           # Build & test commands
├── .golangci.yml                      # Linter configuration
├── .agent.md                          # AI agent instructions
├── go.mod / go.sum                    # Go dependencies
└── README.md                          # Project readme
```

---

## Authentication Strategy

### Web Application (Browser-based)

**Flow:**
1. User initiates OAuth2 login
2. Backend redirects to OAuth2 provider (e.g., Google, GitHub)
3. OAuth2 provider redirects back with authorization code
4. Backend exchanges code for access token
5. Backend creates session (HttpOnly, Secure Cookie)
6. Frontend stores nothing sensitive; relies on HTTP-Only cookies

**Security Considerations:**
- CSRF tokens for state validation
- HttpOnly + Secure + SameSite=Strict cookies
- Session timeout and renewal
- Refresh token rotation

### Mobile Application

**Flow:**
1. Mobile app handles OAuth2 login (using embedded browser or deep linking)
2. OAuth2 provider returns authorization code to app
3. App sends code to backend
4. Backend exchanges code for tokens
5. Backend returns JWT (short-lived) + Refresh Token (long-lived)
6. Mobile app stores JWT in Keychain/Keysafe (encrypted storage)
7. Mobile app includes JWT in Authorization header

**Security Considerations:**
- JWT with short expiration (15-30 minutes)
- Refresh token with longer expiration (7-30 days)
- Refresh token invalidation on logout
- Secure storage using OS keychain
- Token rotation on refresh

### Token Validation Middleware

```
For Web:
  - Read session cookie
  - Validate session in Redis
  - Attach user context

For Mobile:
  - Parse JWT from Authorization header
  - Validate JWT signature
  - Check token expiration
  - Attach user context
```

---

## Database Design

### Technology Stack

- **Primary DB**: PostgreSQL 13+
- **Caching**: Redis (sessions, tokens, frequently accessed data)
- **Connection Pool**: `database/sql` with optimized pool settings

### Core Tables

#### users
```sql
CREATE TABLE users (
  id BIGSERIAL PRIMARY KEY,
  username VARCHAR(255) UNIQUE NOT NULL,
  email VARCHAR(255) UNIQUE NOT NULL,
  display_name VARCHAR(255),
  bio TEXT,
  avatar_url VARCHAR(1024),
  oauth_provider VARCHAR(50) NOT NULL, -- 'google', 'github', etc.
  oauth_id VARCHAR(255) NOT NULL,
  UNIQUE(oauth_provider, oauth_id),
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  deleted_at TIMESTAMP NULL -- Soft delete
);
```

#### posts
```sql
CREATE TABLE posts (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  content TEXT NOT NULL,
  image_urls TEXT[], -- JSON array or separate table
  likes_count INT DEFAULT 0,
  comments_count INT DEFAULT 0,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  deleted_at TIMESTAMP NULL
);
```

#### comments
```sql
CREATE TABLE comments (
  id BIGSERIAL PRIMARY KEY,
  post_id BIGINT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  content TEXT NOT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  deleted_at TIMESTAMP NULL
);
```

#### sessions (Redis keys or optional table)
```
Key: session:{session_id}
Value: {user_id, created_at, expires_at}
TTL: Session duration
```

### Indexing Strategy

```sql
CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_oauth ON users(oauth_provider, oauth_id);
CREATE INDEX idx_posts_user_id ON posts(user_id);
CREATE INDEX idx_posts_created_at ON posts(created_at DESC);
CREATE INDEX idx_comments_post_id ON comments(post_id);
CREATE INDEX idx_comments_user_id ON comments(user_id);
```

---

## API Design

### Base URL
```
https://api.fuju.local/v1
```

### Authentication Endpoints

#### POST /auth/oauth/authorize
Initiates OAuth2 authorization flow.

```
Response:
{
  "redirect_url": "https://provider.example.com/oauth/authorize?..."
}
```

#### POST /auth/oauth/callback
Handles OAuth2 callback.

```
Request:
{
  "code": "authorization_code",
  "state": "state_token"
}

Response (Web):
{
  "session_id": "session_xxx"
}
Cookies: session_id (HttpOnly, Secure)

Response (Mobile):
{
  "access_token": "jwt_token",
  "refresh_token": "refresh_token",
  "expires_in": 1800
}
```

#### POST /auth/refresh
Refresh access token (Mobile).

```
Request:
{
  "refresh_token": "refresh_token"
}

Response:
{
  "access_token": "new_jwt_token",
  "expires_in": 1800
}
```

#### POST /auth/logout
Logout and invalidate session/token.

```
Response:
{
  "status": "success"
}
```

### User Endpoints

#### GET /users/{id}
Get user profile.

#### POST /users
Create user profile (requires auth).

#### PUT /users/{id}
Update user profile (requires auth).

### Post Endpoints

#### GET /posts
List posts (timeline) with pagination.

#### POST /posts
Create a new post (requires auth).

#### GET /posts/{id}
Get post details.

#### DELETE /posts/{id}
Delete post (owner only).

### Comment Endpoints

#### POST /posts/{id}/comments
Add comment (requires auth).

#### DELETE /posts/{id}/comments/{comment_id}
Delete comment (owner only).

---

## Error Handling

### Custom Error Type

```go
type AppError struct {
  Code      string // e.g., "INVALID_REQUEST"
  Message   string
  StatusCode int
  Err       error // Underlying error
}
```

### Error Categories

1. **Validation Errors** (400 Bad Request)
   - Invalid input format
   - Missing required fields
   - Constraint violations

2. **Authentication Errors** (401 Unauthorized)
   - Invalid token
   - Session expired
   - Missing credentials

3. **Authorization Errors** (403 Forbidden)
   - Insufficient permissions
   - Resource access denied

4. **Not Found Errors** (404)
   - Resource doesn't exist

5. **Server Errors** (500)
   - Database errors
   - Unexpected exceptions

---

## Logging & Monitoring

### Logging Strategy

- **Format**: Structured JSON logs
- **Levels**: DEBUG, INFO, WARN, ERROR
- **Context**: Request ID, User ID, Trace ID
- **Sampling**: Reduce verbosity in high-traffic scenarios

Example log:
```json
{
  "timestamp": "2026-04-14T10:30:00Z",
  "level": "INFO",
  "request_id": "req_12345",
  "user_id": 1,
  "message": "User login successful",
  "method": "POST",
  "path": "/auth/callback",
  "status": 200,
  "duration_ms": 145
}
```

### Metrics to Track

- Request count and latency
- Error rate by type
- Database query performance
- Cache hit ratio
- Session/token expiration rates

---

## Development Guidelines

### Code Style

1. **Naming Conventions**
   - Interfaces: `UserRepository`, `PostService` (noun-based)
   - Functions: `GetUser()`, `CreatePost()` (verb-first)
   - Constants: `MaxRetries`, `DefaultTimeout` (PascalCase)

2. **Function Signatures**
   - Accept context as first parameter
   - Return value + error as last return values
   - `func (u *UserService) GetUser(ctx context.Context, id int64) (*User, error)`

3. **Error Handling**
   - Use custom `AppError` for domain errors
   - Wrap external errors with context
   - Log errors with sufficient context

4. **Testing**
   - Unit tests for domain logic and usecases
   - Integration tests for repositories
   - 80%+ code coverage target

### Dependency Management

- **Allowed**: Standard library, `encoding/json`, `net/http`, etc.
- **Minimal**: Third-party packages only for critical functionality
- **Rationale**: Reduce footprint, improve maintainability

### Commit Message Convention

```
type: subject

body

footer
```

Types: `feat`, `fix`, `docs`, `test`, `refactor`, `chore`

Example:
```
feat: Add OAuth2 callback handler

Implements OAuth2 authorization code flow for both web and mobile.
- Exchange authorization code for tokens
- Create session for web clients
- Return JWT for mobile clients

Closes #123
```

---

## Deployment & DevOps

### Environment Configuration

Managed via `.env` and environment variables (production uses secrets):
- `DB_HOST`, `DB_PORT`, `DB_NAME`
- `REDIS_URL`
- `OAUTH_CLIENT_ID`, `OAUTH_CLIENT_SECRET`
- `JWT_SECRET`
- `SESSION_SECRET`
- `ENVIRONMENT` (development, staging, production)

### Docker Support

Multi-stage build for minimal image size. See `Dockerfile` (to be added).

### CI/CD Pipeline

- **On PR**: Lint, unit tests, integration tests
- **On Merge**: Build, deploy to staging
- **Manual**: Deploy to production

---

## Future Enhancements

1. **Passkey Authentication**: WebAuthn support in addition to OAuth2
2. **GraphQL API**: Alternative API layer
3. **Real-time Features**: WebSocket support for notifications
4. **Message Queue**: Event-driven architecture with message queue
5. **Microservices**: Potential to split into separate services
6. **Analytics**: User engagement tracking and insights

---

Generated: 2026-04-14
Last Updated: 2026-04-14
