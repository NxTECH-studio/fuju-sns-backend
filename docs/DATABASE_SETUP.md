# Database Setup Guide

## Prerequisites

- Docker and Docker Compose installed
- PostgreSQL 16+ (if not using Docker)
- Redis 7+ (if not using Docker)

## Quick Start with Docker Compose

### 1. Start Database Services

```bash
# Copy environment file
cp .env.example .env

# Edit .env with your preferred settings
nano .env

# Start PostgreSQL and Redis
docker-compose up -d
```

The containers will start with:
- PostgreSQL on `localhost:5432`
- Redis on `localhost:6379`
- Adminer (database UI) on `http://localhost:8081`

### 2. Run Database Migrations

```bash
# Using the initialization script
./db/init.sh

# Or manually with psql
PGPASSWORD="fuju_password" psql -h localhost -U fuju_user -d fuju < db/migrations/001_initial_schema.sql
```

### 3. Verify Database Setup

```bash
# Connect to PostgreSQL
PGPASSWORD="fuju_password" psql -h localhost -U fuju_user -d fuju

# List tables
\dt

# Exit
\q
```

## Manual Setup (Without Docker)

### 1. Create Database and User

```bash
sudo -u postgres psql

-- Create user
CREATE USER fuju_user WITH PASSWORD 'fuju_password';

-- Create database
CREATE DATABASE fuju OWNER fuju_user;

-- Grant privileges
GRANT ALL PRIVILEGES ON DATABASE fuju TO fuju_user;

-- Exit
\q
```

### 2. Run Migrations

```bash
PGPASSWORD="fuju_password" psql -h localhost -U fuju_user -d fuju < db/migrations/001_initial_schema.sql
```

### 3. Start Redis

```bash
# Using Homebrew (macOS)
brew services start redis

# Or using Docker
docker run -d -p 6379:6379 redis:7-alpine
```

## Environment Variables

Required environment variables for database connection:

```bash
DB_HOST=localhost
DB_PORT=5432
DB_NAME=fuju
DB_USER=fuju_user
DB_PASSWORD=fuju_password
REDIS_URL=redis://localhost:6379
```

## Database Schema

### Users Table
- Stores user information
- Uses OAuth provider and ID for authentication
- Soft deletes with `deleted_at` timestamp

### Posts Table
- Stores post content
- References user via `user_id`
- Tracks `likes_count` and `comments_count`
- Stores image URLs as JSONB array

### Comments Table
- Stores comments on posts
- References both post and user
- Cascading delete with posts

### Sessions Table
- Stores web client sessions
- Linked to users
- Automatic expiration

### OAuth States Table
- Temporary storage for CSRF protection during OAuth flow
- Automatic cleanup on expiration

### Likes Table (Future)
- Tracks like relationships between users and posts
- Ensures one like per user per post

## Database Indexes

Created for performance optimization:
- User lookups: `oauth_provider`, `deleted_at`
- Post queries: `user_id`, `created_at`, `deleted_at`
- Comment queries: `post_id`, `user_id`, `deleted_at`
- Session management: `user_id`, `expires_at`
- OAuth state cleanup: `provider`, `expires_at`

## Backup and Restore

### Backup Database

```bash
PGPASSWORD="fuju_password" pg_dump -h localhost -U fuju_user -d fuju > backup.sql
```

### Restore Database

```bash
PGPASSWORD="fuju_password" psql -h localhost -U fuju_user -d fuju < backup.sql
```

## Troubleshooting

### Connection Refused
- Ensure PostgreSQL and Redis are running
- Check that ports 5432 and 6379 are available
- Verify firewall settings

### Authentication Failed
- Verify credentials in `.env` file
- Check user privileges

### Migration Errors
- Ensure migrations are run in order
- Check for syntax errors in SQL files
- Verify database permissions

## Adminer Web UI

Access database management interface at `http://localhost:8081`:
- System: PostgreSQL
- Server: postgres
- Username: fuju_user
- Password: fuju_password
- Database: fuju

## Cleanup

### Stop Services

```bash
docker-compose down
```

### Remove Volumes (WARNING: Deletes Data)

```bash
docker-compose down -v
```
