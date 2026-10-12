# Rollback

Migrations must be backward compatible (expand/migrate/contract). Rollback never
reverses a migration automatically.

1. Read the previous digests:
   `cat /opt/quizforge/previous.env` (written by `deploy.sh` before switching).
2. Re-run the deploy driver with previous digests:
   `DEPLOY_DIR=/opt/quizforge ./deploy/scripts/deploy.sh <prev-api-digest> <prev-web-digest>`
   Skip the migration step only if the failed deploy never migrated; otherwise the
   old image must tolerate the new schema (backward-compatibility requirement).
3. Verify `/health/ready`, `/version` (expect previous commit), and one live
   question with two browsers.
4. If the database itself needs recovery, restore into a **new Neon branch**,
   verify row counts plus one sample session reconstruction, then point
   `MIGRATIONS_DATABASE_URL`/`DATABASE_URL` at the restored branch. Never
   restore over production in place without a snapshot.
5. Record commit, migration version, checks, duration, and result in
   `docs/IMPLEMENTATION_STATUS.md`.

Target RTO 4h, RPO 24h (measured drill replaces these targets).
