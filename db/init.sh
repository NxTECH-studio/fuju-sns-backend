#!/bin/bash
# Database initialization script
# Usage: ./db/init.sh

set -e

# Load environment variables
if [ -f .env ]; then
  export $(cat .env | grep -v '#' | xargs)
fi

# Set defaults
DB_HOST=${DB_HOST:-localhost}
DB_PORT=${DB_PORT:-5432}
DB_NAME=${DB_NAME:-fuju}
DB_USER=${DB_USER:-fuju_user}
DB_PASSWORD=${DB_PASSWORD:-fuju_password}

echo "Connecting to PostgreSQL at $DB_HOST:$DB_PORT..."

# Wait for PostgreSQL to be ready
max_attempts=30
attempt=1
while ! PGPASSWORD="$DB_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -c "SELECT 1" &>/dev/null; do
  if [ $attempt -ge $max_attempts ]; then
    echo "Error: Failed to connect to PostgreSQL after $max_attempts attempts"
    exit 1
  fi
  echo "Waiting for PostgreSQL to be ready... (attempt $attempt/$max_attempts)"
  sleep 1
  ((attempt++))
done

echo "Connected to PostgreSQL successfully!"

# Run migrations
MIGRATIONS_DIR="./db/migrations"

if [ ! -d "$MIGRATIONS_DIR" ]; then
  echo "Error: Migrations directory not found: $MIGRATIONS_DIR"
  exit 1
fi

echo "Running migrations from $MIGRATIONS_DIR..."

for migration in $(ls "$MIGRATIONS_DIR"/*.sql | sort); do
  echo "Running migration: $(basename $migration)"
  PGPASSWORD="$DB_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" < "$migration"
done

echo "All migrations completed successfully!"
