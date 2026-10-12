#!/usr/bin/env bash
# Nightly logical backup to encrypted S3 ( supplements Neon restore history).
# Runs on the EC2 host via cron/systemd timer. Alarms on failure.
set -euo pipefail

: "${MIGRATIONS_DATABASE_URL:?direct Neon URL required}"
: "${BACKUP_BUCKET:?s3 bucket required}"

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
FILE="/tmp/quizforge-$STAMP.dump"
pg_dump --format=custom --no-owner --file="$FILE" "$MIGRATIONS_DATABASE_URL"
aws s3 cp "$FILE" "s3://$BACKUP_BUCKET/nightly/quizforge-$STAMP.dump" --sse AES256
rm -f "$FILE"
echo "backup ok: quizforge-$STAMP.dump"

# Retention is enforced by the bucket lifecycle rule (14 days).
