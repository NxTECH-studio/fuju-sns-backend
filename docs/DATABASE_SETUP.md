# Database Setup Guide

## Prerequisites

- Docker and Docker Compose installed
- PostgreSQL 16+ (if not using Docker)

## Quick Start with Docker Compose

### 1. Start Database Services

```bash
# Copy environment file
cp .env.example .env

# Edit .env with your preferred settings
nano .env

# Start PostgreSQL
docker-compose up -d
```

The containers will start with:
- PostgreSQL on `localhost:5432`
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

## Environment Variables

Required environment variables for database connection:

```bash
DB_HOST=localhost
DB_PORT=5432
DB_NAME=fuju
DB_USER=fuju_user
DB_PASSWORD=fuju_password
```

## Database Schema

### Users Table
- Mirror of AuthCore identity keyed by `sub` (ULID) plus SNS-owned
  fields (`bio`, `banner_url`, `is_admin`)
- `*_cached` columns refreshed with a 1h TTL via `profile_refreshed_at`
- Soft deletes with `deleted_at` timestamp

### Posts Table
- Stores post content with ULID primary key
- References author via `user_id` (`users.sub`)
- Replies are modelled as posts pointing back via `parent_post_id`
  (there is no separate `comments` table)
- Tracks `likes_count`; like relationships live in the dedicated
  `likes` table

### Database Indexes

Created for performance optimization:
- User lookups: `profile_refreshed_at`, `display_id_cached`, `deleted_at`
- Post queries: `user_id`, `created_at`, `parent_post_id`, `deleted_at`
- Likes: `(user_id, post_id)` unique, `post_id`

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
- Ensure PostgreSQL is running
- Check that port 5432 is available
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
