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
│  External (DB, Cache, AuthCore) │  <- External Systems
└─────────────────────────────────┘
```

### Layer Responsibilities

1. **Domain Layer** (`internal/domain/`)
   - Pure business entities and value objects
   - Domain-specific rules and validations
   - No external dependencies
   - Examples: `User` (AuthCore mirror + SNS-owned fields), `Post` entities

2. **Repository Layer** (`internal/repository/`)
   - Data persistence abstraction
   - Database queries and transactions
   - Implements interfaces defined alongside the entities they persist
     (`internal/repository/repository.go` is the single source of
     truth for every contract)
   - Two backends live side-by-side:
     - `internal/repository/inmemory/` — fast, volatile, used by unit
       tests and local `go run ./cmd/server` in `ENVIRONMENT=development`
     - `internal/repository/postgres/` — pgx/v5 against the schema in
       `db/migrations/`; used in staging / production and by CI
       integration tests
   - Backend selection is driven by `REPO_BACKEND` (explicit) or
     `ENVIRONMENT` (auto); see `cmd/server/repos.go`
   - `internal/repository/testsupport/` holds contract tests that run
     against **both** backends — if a future repository change
     diverges between in-memory and postgres, the shared contract
     suite fails on at least one side. Cursor encoders that must be
     byte-identical across backends live in
     `internal/repository/sharedcursor/`
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
│   │   ├── user.go                    # User entity (AuthCore mirror + SNS fields)
│   │   ├── post.go                    # Post entity & interfaces
│   │   ├── image.go                   # Image entity & storage interface
│   │   ├── repository.go              # Repository interfaces
│   │   └── errors.go                  # Domain-specific errors
│   ├── usecase/
│   │   ├── user/
│   │   │   ├── get_user.go
│   │   │   ├── get_or_hydrate_user.go
│   │   │   ├── update_user_profile.go
│   │   │   └── list_users.go
│   │   └── post/
│   │       ├── create_post.go
│   │       ├── get_post.go
│   │       └── list_posts.go
│   ├── repository/
│   │   ├── repository.go              # Interface contracts
│   │   ├── inmemory/                  # Map-backed impls for unit tests / dev
│   │   ├── postgres/                  # pgx/v5 impls for staging / prod
│   │   ├── testsupport/               # Contract tests shared by both
│   │   └── sharedcursor/              # Cross-backend cursor encoder
│   ├── handler/
│   │   ├── user_handler.go
│   │   ├── post_handler.go
│   │   └── health_handler.go
│   └── middleware/
│       ├── auth.go                    # AuthCore introspection
│       ├── hydrate.go                 # Lazy-create / TTL-refresh user row
│       ├── logging.go                 # Request logging
│       ├── cors.go                    # CORS handling
│       └── recovery.go                # Panic recovery
├── pkg/
│   ├── db/
│   │   ├── pgx.go                     # pgxpool wrapper (NewPool / Ping)
│   │   └── tx.go                      # WithTx helper
│   ├── logger/
│   │   ├── logger.go                  # Structured JSON logging
│   │   └── context.go                 # Context-aware logging
│   ├── errors/
│   │   ├── errors.go                  # Custom error types
│   │   └── http_errors.go             # HTTP error mapping
│   ├── cache/
│   │   └── cache_key.go               # Cache key management
│   └── authcore/
│       ├── client.go                  # AuthCore HTTP client (Introspect / GetProfile)
│       └── cache.go                   # 30s in-memory introspection cache
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

Identity and session state are owned by **AuthCore**, a separate service.
The SNS backend does not issue, rotate, or validate credentials of its own;
it treats every request as anonymous until AuthCore tells it otherwise. The
`users` table is a mirror keyed by AuthCore's `sub` (ULID) plus a small set
of SNS-owned fields (`bio`, `banner_url`, `is_admin`).

### Middleware chain (current `cmd/server/main.go`)

Authenticated routes pass through, in order:

1. `AuthMiddleware(cachedAuthCore)` — introspects the Bearer token and
   sets `sub` + the raw access token onto the request context.
2. `HydrateUserMiddleware(userHydrateUC)` — resolves `sub` to
   `*domain.User`, lazy-creating or TTL-refreshing via AuthCore
   `GetProfile` as needed.
3. (Admin-only routes) `AdminMiddleware()` — rejects non-admin callers
   with 403.

The global stack wrapping `mux` is (outermost first): context timeout
(30s) -> request logging -> panic recovery -> CORS.

### Request flow

1. The client sends `Authorization: Bearer <access_token>` — an
   AuthCore-issued access token. No session cookie is involved; this
   service does not write `Set-Cookie`.
2. `AuthMiddleware` extracts the Bearer value and calls AuthCore's
   introspection endpoint
   (`POST {AUTHCORE_BASE_URL}{AUTHCORE_INTROSPECT_PATH}`) as an
   HTTP Basic-authenticated confidential client (`AUTHCORE_CLIENT_ID` /
   `AUTHCORE_CLIENT_SECRET`) to resolve `sub` and expiry.
3. Results are held in an in-memory cache (`AUTHCORE_INTROSPECT_CACHE_TTL`,
   default 30s) keyed on the token value so AuthCore is not hit on every
   API call.
4. `HydrateUserMiddleware` looks up the `users` row for `sub`. If missing,
   the row is lazy-created from `GetProfile`. If older than
   `AUTHCORE_PROFILE_TTL` (default 1h) the cached profile fields are
   refreshed. The resolved `*domain.User` is attached to the context.
5. Handlers read the caller via `auth.GetSubFromContext` /
   `auth.GetCurrentUserFromContext`.

### Two layers of AuthCore traffic

| Layer | Purpose | Frequency | TTL |
|---|---|---|---|
| Introspection | Is the access token active? Who is `sub`? | Every authenticated request | 30s in-memory cache |
| Profile hydration | Pull DisplayName / DisplayID / IconURL | On lazy-create or when the mirror row is older than 1h | 1h (tracked via `profile_refreshed_at`) |

### Failure modes

- **Introspection**: fail-closed. An unreachable AuthCore returns 503;
  an `active=false` response returns 401. A fresh (<=30s) cache entry
  absorbs short upstream blips.
- **Profile hydration**: fail-open. On error the existing `*_cached` fields
  are returned and `profile_refreshed_at` is left stale so the next request
  retries. The first hydrate after lazy-create may insert blank cached
  fields; subsequent requests fix them.

### Context helpers

```
ctx := auth.SetSubInContext(r.Context(), session.Sub)           // AuthMiddleware
ctx  = auth.SetCurrentUserInContext(ctx, user)                  // HydrateUserMiddleware
sub, ok   := auth.GetSubFromContext(r.Context())
user, ok  := auth.GetCurrentUserFromContext(r.Context())
```

---

## Database Design

### Technology Stack

- **Primary DB**: PostgreSQL 13+
- **Caching**: 現状なし。AuthCore introspection は in-memory キャッシュ（`pkg/authcore/cache.go`、TTL=30s）。
- **Connection Pool**: `database/sql` with optimized pool settings

### Identifiers

All primary keys and foreign keys are ULIDs stored as `CHAR(26)`
(Crockford Base32). `users.sub` comes from AuthCore; every other ULID is
generated in the application layer (`ulid.Make().String()`) before the
INSERT.

### Core Tables

#### users

Mirror of AuthCore identity plus SNS-owned profile fields. The `*_cached`
columns are refreshed with a 1h TTL via `profile_refreshed_at`. `bio`,
`banner_url` and `is_admin` are owned by this service.

```sql
CREATE TABLE users (
  sub                   CHAR(26)      PRIMARY KEY,
  display_name_cached   VARCHAR(255)  NOT NULL DEFAULT '',
  display_id_cached     VARCHAR(64)   NOT NULL DEFAULT '',
  icon_url_cached       VARCHAR(1024) NOT NULL DEFAULT '',
  profile_refreshed_at  TIMESTAMPTZ   NOT NULL DEFAULT '1970-01-01',
  bio                   TEXT          NOT NULL DEFAULT '',
  banner_url            VARCHAR(1024) NOT NULL DEFAULT '',
  is_admin              BOOLEAN       NOT NULL DEFAULT false,
  created_at            TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
  updated_at            TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
  deleted_at            TIMESTAMPTZ   NULL
);
```

#### posts
```sql
CREATE TABLE posts (
  id          CHAR(26)    PRIMARY KEY,
  user_id     CHAR(26)    NOT NULL REFERENCES users(sub) ON DELETE CASCADE,
  content     TEXT        NOT NULL,
  image_urls  JSONB       NOT NULL DEFAULT '[]'::jsonb,
  likes_count INT         NOT NULL DEFAULT 0,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at  TIMESTAMPTZ NULL
);
```

#### comments (transitional)

Retained with ULID-typed keys only as a bridge. The post feature task
(`02-implement-post-feature.md`) folds comments into post replies and
drops this table.

```sql
CREATE TABLE comments (
  id         CHAR(26)    PRIMARY KEY,
  post_id    CHAR(26)    NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  user_id    CHAR(26)    NOT NULL REFERENCES users(sub) ON DELETE CASCADE,
  content    TEXT        NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ NULL
);
```

### Indexing Strategy

```sql
CREATE INDEX idx_users_profile_refreshed_at ON users(profile_refreshed_at);
CREATE INDEX idx_users_display_id_cached    ON users(display_id_cached);
CREATE INDEX idx_posts_user_id              ON posts(user_id);
CREATE INDEX idx_posts_created_at           ON posts(created_at DESC);
CREATE INDEX idx_comments_post_id           ON comments(post_id);
CREATE INDEX idx_comments_user_id           ON comments(user_id);
```

---

## API Design

### Base URL
```
https://api.fuju.local/v1
```

Authentication is handled by AuthCore; this backend exposes no
`/auth/*` endpoints. Authenticated requests carry an AuthCore-issued
access token in the `Authorization: Bearer <token>` header, which the
middleware introspects on every call.

### Me Endpoint

#### GET /me
Return the authenticated caller's own user record. The row is
lazy-created on first call and its cached profile fields are refreshed
from AuthCore when older than 1h. Requires auth.

### User Endpoints

#### GET /users
List users with pagination.

#### GET /users/{sub}
Get a user profile by AuthCore `sub` (ULID).

#### PUT /users/{sub}
Update SNS-owned profile fields. Only `bio` and `banner_url` are
accepted; identity and AuthCore-cached fields cannot be mutated here.
Callers can only update their own profile. Requires auth.

### Post Endpoints

#### GET /posts
List posts (timeline) with pagination.

#### POST /posts
Create a new post (requires auth).

#### GET /posts/{id}
Get post details.

#### DELETE /posts/{id}
Delete post (owner only).

### Comment Endpoints (transitional)

`POST /posts/{id}/comments` and `DELETE /posts/{post_id}/comments/{comment_id}`
exist as a bridge and will be replaced by post replies in the post
feature task (`02-implement-post-feature.md`). Treat them as deprecated
in new work.

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
   - Missing `Authorization` header
   - AuthCore introspection returned `active=false` (expired or revoked token)

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
  "sub": "01HX4Y6Q9M8T3F2B1C0D7R5A9K",
  "message": "User loaded from hydrate",
  "method": "GET",
  "path": "/me",
  "status": 200,
  "duration_ms": 145
}
```

### Metrics to Track

- Request count and latency
- Error rate by type
- Database query performance
- Cache hit ratio
- Access token expiration rates (from AuthCore introspection responses)

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
   - `func (u *UserService) GetUser(ctx context.Context, sub string) (*User, error)`

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
feat: Add AuthCore introspection middleware

Validates the AuthCore-issued Bearer access token on every
authenticated request and attaches the resolved sub to the request
context. Results are memoised in a 30s in-memory cache keyed on the
token value.

Closes #123
```

---

## Deployment & DevOps

### Environment Configuration

Managed via `.env` and environment variables (production uses secrets):
- `DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_USER`, `DB_PASSWORD`
- `AUTHCORE_BASE_URL`, `AUTHCORE_CLIENT_ID`, `AUTHCORE_CLIENT_SECRET`
- `AUTHCORE_INTROSPECT_PATH`, `AUTHCORE_PROFILE_PATH`
- `AUTHCORE_PROFILE_TTL`, `AUTHCORE_INTROSPECT_CACHE_TTL`
- `ENVIRONMENT` (development, staging, production)

### Docker Support

Multi-stage build for minimal image size. See `Dockerfile` (to be added).

### CI/CD Pipeline

- **On PR**: Lint, unit tests, integration tests
- **On Merge**: Build, deploy to staging
- **Manual**: Deploy to production

---

## Future Enhancements

1. **GraphQL API**: Alternative API layer
2. **Real-time Features**: WebSocket support for notifications
3. **Message Queue**: Event-driven architecture with message queue
4. **Microservices**: Potential to split into separate services
5. **Analytics**: User engagement tracking and insights
6. **Redis-backed introspection cache**: move the 30s AuthCore cache out of process

---

Generated: 2026-04-14
Last Updated: 2026-04-14
