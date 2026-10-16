# backend

Go service: the admin API for users, projects, apps and git connections, plus the internal issues service that files issues on GitHub, GitLab and Bitbucket. Status: Phases 0–2 implemented (foundations, users, projects, apps). See [docs/](docs/README.md) for the full design.

## Quick start

```sh
make dev-up                       # PostgreSQL on localhost:5433 (Docker)
cp backend/.env.example backend/.env
set -a; . backend/.env; set +a    # export the configuration
make backend-run                  # migrates first when MIGRATE_ON_START=true
```

```sh
curl -X POST localhost:8080/v1/users \
  -H "Authorization: Bearer $ADMIN_API_TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Wail","email":"wail@example.com"}'
```

## Tests

```sh
make backend-test
```

The integration tests use `TEST_DATABASE_URL` if it is set; otherwise they start a PostgreSQL container through Docker. They are skipped when neither is available. See [docs/09-testing.md](docs/09-testing.md).
