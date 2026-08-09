# cmaker packs API server

M1 + M2 of the plan in `../PACKS_PLAN.md`: server skeleton, migration
runner, the three auth endpoints (login/whoami/logout), and packs
publish/download/search backed by real Cloudflare R2 (presigned PUT/GET -
the server never proxies file bytes). CLI integration is M3.

## Local development

Requires a Postgres instance and (for M2's publish/download endpoints) a
Cloudflare R2 bucket - set `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`,
`R2_SECRET_ACCESS_KEY`, `R2_BUCKET` (all required at startup, same as
`DATABASE_URL`).

Postgres, easiest via Docker:

```
docker run -d --name cmaker-packs-pg \
  -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=cmakerpacks \
  -p 5433:5432 postgres:16-alpine
```

Then run the server:

```
export DATABASE_URL="postgres://postgres:postgres@localhost:5433/cmakerpacks?sslmode=disable"
export PORT=8099   # optional, defaults to 8080
go run ./cmd/api
```

Migrations in `internal/db/migrations/*.sql` run automatically on startup
(idempotent - safe on every restart).

To actually log in, you need a real GitHub user access token (`gh auth
token` if you have the `gh` CLI set up) and a matching allowlist row -
there's no self-service signup, it's added by hand:

```
psql "$DATABASE_URL" -c "INSERT INTO allowlist (github_login, added_by) VALUES ('<your-github-login>', 'manual');"

curl -X POST localhost:8099/v1/auth/login -d "{\"github_token\":\"$(gh auth token)\"}"
```

## Tests

```
go test ./...
```

Handler tests use in-memory fakes (`fakeStore`/`fakeGitHub` in
`internal/api/auth_test.go`) - no database required to run the suite.

## Deploying (Fly.io)

Live at **https://cmaker-packs-api.fly.dev** (app `cmaker-packs-api`, region
`iad`), backed by a real Neon Postgres database. To redeploy after a change:

```
fly deploy --remote-only --app cmaker-packs-api
```

`DATABASE_URL` and the four `R2_*` secrets are already set on the Fly app
(`fly secrets set ...`). Both machines have `auto_stop_machines`/
`auto_start_machines` on, so they scale to zero when idle - near-$0 at
POC traffic. The R2 bucket itself (`cmaker-packs`) already exists too.

There's no self-service signup: a GitHub login has to be added to the
`allowlist` table by hand (direct SQL against the Neon connection string)
before `cmaker login` will work for that account.
