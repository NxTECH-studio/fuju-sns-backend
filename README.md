# FUJU Backend

A modern SNS (Social Network Service) backend built with Go, following Clean Architecture principles.

## Quick Links

Frontend developers integrating against this backend should read in this
order:

1. This README (repository overview, local start-up)
2. [`docs/product-overview.md`](docs/product-overview.md) — product
   features, user model, ULID identifiers, timeline paging, error envelope
3. [`docs/swagger.yaml`](docs/swagger.yaml) — OpenAPI 3.0.3 spec (type-
   generation-ready)
4. [`docs/authcore-integration.md`](docs/authcore-integration.md) —
   AuthCore Bearer token flow, `/me` hydrate, admin flag, CORS
5. [`docs/frontend/image-upload.md`](docs/frontend/image-upload.md) —
   `multipart/form-data` upload to `POST /v1/images`, attaching images
   to posts via `image_ids`

Backend maintainers: see [`docs/architecture.md`](docs/architecture.md)
and [`docs/IMPLEMENTATION_GUIDE.md`](docs/IMPLEMENTATION_GUIDE.md).

## Features

- **Clean Architecture**: Strict layer separation (Domain, Usecase, Repository, Handler, Middleware)
- **Authentication**: Bearer JWT (AuthCore-issued) validated via RFC 7662 introspection
- **Database**: PostgreSQL with connection pooling
- **Structured Logging**: JSON-formatted logs for easy parsing
- **Testing**: Comprehensive unit and integration tests
- **CI/CD**: GitHub Actions with automated testing, linting, and security checks
- **API Documentation**: OpenAPI/Swagger specification
- **Code Quality**: golangci-lint with multiple linters

## Quick Start

### Prerequisites

- Go 1.21+
- PostgreSQL 13+
- Make

### Installation

1. Clone the repository:
   ```bash
   git clone https://github.com/fuju/backend.git
   cd backend
   ```

2. Install dependencies:
   ```bash
   make setup
   ```

3. Configure environment variables:
   ```bash
   cp .env.example .env
   # Edit .env with your configuration
   ```

4. Run tests:
   ```bash
   make test
   ```

5. Start the server:
   ```bash
   make run
   ```

## Environment Variables

Required:
- `DB_HOST`: PostgreSQL host
- `DB_PORT`: PostgreSQL port (default: 5432)
- `DB_NAME`: Database name
- `DB_USER`: Database user
- `DB_PASSWORD`: Database password
- `AUTHCORE_BASE_URL`: Base URL of the AuthCore service (no trailing slash)
- `AUTHCORE_CLIENT_ID`: AuthCore client ID (confidential client used for introspection)
- `AUTHCORE_CLIENT_SECRET`: AuthCore client secret

Optional:
- `SERVER_PORT`: HTTP server port (default: 8080)
- `ENVIRONMENT`: Environment (development, staging, production)
- `LOG_LEVEL`: Log level (debug, info, warn, error)
- `AUTHCORE_INTROSPECT_PATH`: RFC 7662 path (default: `/v1/auth/introspect`)
- `AUTHCORE_PROFILE_PATH`: Profile endpoint path (default: `/v1/user/profile`)
- `AUTHCORE_PROFILE_TTL`: Mirror refresh TTL (default: `1h`)
- `AUTHCORE_INTROSPECT_CACHE_TTL`: In-memory introspection cache TTL (default: `30s`)
- `CORS_ALLOWED_ORIGINS`: Comma-separated origins (default: `*`)
- `OGP_USER_AGENT`: User-Agent sent by the OGP fetcher

### Optional features

#### Image upload (Cloudflare R2)

The `POST /v1/images`, `GET /v1/images`, `DELETE /v1/images/{id}`
endpoints are mounted only when **all five** of the following are set.
Leaving them all empty disables image upload (the routes return 404 and
the server logs `Image upload disabled: R2 is not configured` at boot).
A partial configuration is rejected by `config.Validate` to surface
deployment mistakes early.

- `R2_ENDPOINT` — `https://<account_id>.r2.cloudflarestorage.com` (S3-compatible API endpoint, from the Cloudflare R2 dashboard)
- `R2_BUCKET_NAME` — bucket name (must already exist in R2)
- `R2_PUBLIC_DOMAIN` — public-access URL prefix, e.g. `https://images.fuju.example.com` (R2 Public Bucket or custom domain)
- `R2_ACCESS_KEY_ID` — R2 API token's access key ID
- `R2_SECRET_ACCESS_KEY` — R2 API token's secret access key

Frontend integrators should follow
[`docs/frontend/image-upload.md`](docs/frontend/image-upload.md) for
the API contract; the swagger spec is also available at
`docs/swagger.yaml` under `/v1/images`.

## Project Structure

```
backend/
├── cmd/
│   └── server/          # Application entry point
├── internal/
│   ├── domain/          # Business entities and rules
│   ├── usecase/         # Business logic
│   ├── repository/      # Data persistence
│   ├── handler/         # HTTP handlers
│   └── middleware/      # Cross-cutting concerns
├── pkg/
│   ├── db/              # Database utilities
│   ├── logger/          # Structured logging
│   ├── errors/          # Error handling
│   └── authcore/        # AuthCore introspection client + 30s in-memory cache
├── config/              # Configuration management
├── docs/
│   ├── swagger.yaml     # API specification
│   └── architecture.md  # Design documentation
└── .github/workflows/   # CI/CD pipelines
```

## API Documentation

View the API documentation at:
- **Swagger UI**: http://localhost:8080/docs/swagger
- **OpenAPI Spec**: [docs/swagger.yaml](docs/swagger.yaml)

## Architecture

This project follows **Clean Architecture** principles:

1. **Domain Layer**: Pure business logic with no external dependencies
2. **Usecase Layer**: Application business rules
3. **Repository Layer**: Data persistence abstraction
4. **Handler Layer**: HTTP request/response handling
5. **Middleware Layer**: Cross-cutting concerns (auth, logging, etc.)

```
Request → Handler → Middleware → Usecase → Repository → Database
```

## Development

### Build

```bash
make build
```

### Run Tests

```bash
# Run all tests
make test

# Run with race detection
make test-verbose
```

### Code Quality

```bash
# Run linters
make lint

# Format code
make fmt

# Clean artifacts
make clean
```

### Available Commands

```bash
make help              # Show all available commands
make setup             # Install dependencies and tools
make build             # Build the binary
make run               # Run the server locally
make test              # Run all tests with coverage
make test-verbose      # Run tests with verbose output and race detection
make lint              # Run linters
make fmt               # Format code
make clean             # Clean build artifacts
make ci-test           # Run CI tests (lint + test)
make build-prod        # Build production binary
```

## Authentication

Clients obtain an access token from AuthCore and pass it as
`Authorization: Bearer <token>` on every FUJU API call. The backend
validates it per request via RFC 7662 introspection with a 30s
in-memory cache. No cookies are issued by this service.

See [`docs/authcore-integration.md`](docs/authcore-integration.md) for
the FE-facing flow (token refresh, `/me` hydrate, admin flag, CORS) and
`pkg/authcore/` + `internal/middleware/` for the backend implementation.

## Database

### PostgreSQL Setup

```bash
# Create database
createdb fuju

# Apply migrations (PostgreSQL must be running)
make db-init
```

### Schema

- **users**: SNS-local mirror cache of AuthCore profile + bio / banner / is_admin
- **posts**: User posts, including replies via `parent_post_id`
- **likes**: Like relationships between users and posts
- **images**: Uploaded image metadata (Cloudflare R2 storage keys)

## Testing

### Unit Tests

Test individual components in isolation with mocks.

```bash
go test -v ./internal/usecase/...
```

### Integration Tests

Test components with a real PostgreSQL service.

```bash
go test -v -race ./...
```

### Coverage

```bash
go test -v -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

Target: **80%+ code coverage**

## CI/CD Pipeline

Automated workflows run on every push and pull request:

1. **Lint**: Code style and quality checks
2. **Test**: Unit and integration tests
3. **Build**: Multi-platform binary compilation
4. **Security**: Vulnerability scanning

See [.github/workflows](.github/workflows/) for configuration.

## Agent Instructions

For AI code generation and assistance, see [.agent.md](.agent.md)

Key principles:
- Clean Architecture adherence
- Standard library preference
- Comprehensive error handling
- 80%+ test coverage
- Conventional commit messages

## Contributing

1. Create a feature branch: `git checkout -b feat/feature-name`
2. Make changes following [.agent.md](.agent.md)
3. Write tests for new code
4. Run `make lint` and `make test`
5. Commit with conventional message: `git commit -m "feat(scope): message"`
6. Push and create a pull request

## License

MIT

## Support

For issues and questions, please open a GitHub issue or check the documentation in [docs/](docs/).

---

**Project Start Date**: 2026-04-14
