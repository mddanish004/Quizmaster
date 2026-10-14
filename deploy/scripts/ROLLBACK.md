# Rollback

Production is one Render web service (single Docker image serving UI + API)
plus managed Neon Postgres. There is no host to SSH into. Every release is an
immutable git tag (`v*`), so rollback is redeploy, not repair.

## App rollback (Render)

1. Open the Render dashboard → the `quizforge` service → **Deploys**.
2. Find the last good deploy and hit **Rollback** (re-boots the previous
   image with the current env). No data migration is involved.
3. Verify: `curl $APP/health/ready` → 200, `curl $APP/version` shows the
   previous tag, then run the core smoke (login → open a quiz → join code).

## Database rollback

Migrations are forward-only in the deploy path. To step back one migration
after a bad `up`:

```bash
export MIGRATIONS_DATABASE_URL='postgres://…direct Neon URL…'
docker run --rm -v "$PWD/database/migrations:/migrations:ro" \
  migrate/migrate:v4.18.3 -path /migrations -database "$MIGRATIONS_DATABASE_URL" down 1
```

Rules: take a backup first (`deploy/scripts/backup.sh`), never edit an
applied migration, and confirm `down`/`up` on a disposable copy when the
migration touches live tables.
