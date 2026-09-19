#!/bin/sh
set -e

echo "Running database migrations..."
npx prisma migrate deploy || {
    echo "Migration failed, attempting to baseline and retry..."
    # If migration fails due to existing schema, try to resolve migrations
    npx prisma migrate resolve --applied 20251027015416_init || true
    npx prisma migrate resolve --applied 20251027021143_add_hidden_field || true
    npx prisma migrate resolve --applied 20251027021655_add_submission_limits || true
    npx prisma migrate resolve --applied 20251027022635_add_device_and_whitelist || true
    npx prisma migrate resolve --applied 20251027023608_add_admin_user || true
    npx prisma migrate resolve --applied 20251029141542_add_movie_description || true
    # Now apply the new migration
    npx prisma migrate deploy
}

echo "Starting application..."
exec node server.js
