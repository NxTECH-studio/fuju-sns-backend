# FUJU Backend

A modern SNS (Social Network Service) backend built with Go, following Clean Architecture principles.

## Features

- **Clean Architecture**: Strict layer separation (Domain, Usecase, Repository, Handler, Middleware)
- **Authentication**: OAuth2.0 + JWT (mobile) + Session Cookies (web)
- **Database**: PostgreSQL with connection pooling
- **Caching**: Redis for sessions and frequently accessed data
- **Structured Logging**: JSON-formatted logs for easy parsing
- **Testing**: Comprehensive unit and integration tests
- **CI/CD**: GitHub Actions with automated testing, linting, and security checks
- **API Documentation**: OpenAPI/Swagger specification
- **Code Quality**: golangci-lint with multiple linters

## Quick Start

### Prerequisites

- Go 1.21+
- PostgreSQL 13+
- Redis 7+
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
- `REDIS_URL`: Redis connection URL
- `OAUTH_CLIENT_ID`: OAuth2 client ID
- `OAUTH_CLIENT_SECRET`: OAuth2 client secret
- `OAUTH_REDIRECT_URL`: OAuth2 redirect URL
- `JWT_SECRET`: Secret key for JWT signing
- `SESSION_SECRET`: Secret key for session encryption

Optional:
- `SERVER_PORT`: HTTP server port (default: 8080)
- `ENVIRONMENT`: Environment (development, staging, production)
- `LOG_LEVEL`: Log level (debug, info, warn, error)
- `DB_MAX_CONN`: Maximum database connections (default: 25)
- `DB_MIN_CONN`: Minimum database connections (default: 5)

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
│   ├── cache/           # Redis caching
│   └── auth/            # Authentication utilities
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

### Web Application (Browser)

1. User initiates OAuth2 login
2. Backend redirects to OAuth2 provider
3. OAuth2 provider redirects back with authorization code
4. Backend exchanges code for token and creates session cookie
5. Frontend uses HttpOnly cookie for subsequent requests

**Security**: CSRF tokens, HttpOnly cookies, SameSite=Strict

### Mobile Application

1. Mobile app handles OAuth2 login flow
2. App sends authorization code to backend
3. Backend exchanges code for tokens
4. Backend returns JWT and refresh token to app
5. App stores JWT in secure storage (Keychain/Keysafe)
6. App includes JWT in Authorization header for API requests

**Security**: Short-lived JWT, refresh token rotation, secure storage

## Database

### PostgreSQL Setup

```bash
# Create database
createdb fuju

# Run migrations (to be implemented)
make migrate
```

### Schema

- **users**: User profiles and OAuth information
- **posts**: User posts/tweets
- **comments**: Comments on posts
- **sessions**: Active user sessions (also in Redis)

## Testing

### Unit Tests

Test individual components in isolation with mocks.

```bash
go test -v ./internal/usecase/...
```

### Integration Tests

Test components with real PostgreSQL and Redis services.

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
