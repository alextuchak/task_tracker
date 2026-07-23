# Task Tracker

[![CI](https://github.com/alextuchak/task_tracker/actions/workflows/ci.yaml/badge.svg)](https://github.com/alextuchak/task_tracker/actions/workflows/ci.yaml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/alextuchak/task_tracker)](go.mod)
[![Release](https://img.shields.io/github/v/release/alextuchak/task_tracker)](https://github.com/alextuchak/task_tracker/releases)
[![golangci-lint](https://img.shields.io/badge/lint-golangci--lint-00ADD8?logo=go)](.golangci.yaml)
[![govulncheck](https://img.shields.io/badge/security-govulncheck-00ADD8?logo=go)](https://go.dev/blog/vuln)
[![gosec](https://img.shields.io/badge/security-gosec-00ADD8?logo=go)](https://github.com/securego/gosec)
[![testcontainers](https://img.shields.io/badge/tests-testcontainers-2496ED?logo=docker&logoColor=white)](tests/integration)
[![License: MIT](https://img.shields.io/badge/license-MIT-yellow.svg)](LICENSE)

REST API for team task management: teams with role-based access, task audit history,
JWT authentication, Redis caching and rate limiting, Prometheus observability.

## Features

- **Teams & RBAC** — team roles (`owner`/`admin`/`member`) checked per request against the
  database, global `admin` role with full bypass, invitations with best-effort email delivery
  behind a circuit breaker
- **Tasks with audit history** — every change is diffed field-by-field and written to
  `task_history` in the same transaction as the update; changes from one request share a
  `change_group_id` (Jira-style change groups)
- **Keyset pagination** everywhere — `cursor`/`next_cursor` instead of `LIMIT/OFFSET`:
  stable under concurrent inserts, no deep-page degradation
- **Redis** — task list cache (5 min TTL, invalidation on write) and GCRA rate limiting
  (per-user on authenticated routes, per-IP on `register`/`login`)
- **Admin analytics** — team stats, top-3 task creators per team (window functions),
  data integrity report (anti-join), all cursor-paginated
- **Observability** — Prometheus metrics with a provisioned Grafana dashboard,
  structured JSON logs, `/livez` + `/readyz` with a three-state readiness gate
- **Operational hygiene** — two-phase graceful shutdown (drain → release), startup
  connection pings, migrations run in a dedicated container before the app starts

## Quick start

```bash
docker compose up -d --build
```

That single command starts MySQL, Redis, the migrator (applies goose migrations and
exits), the API, Prometheus and Grafana.

| Endpoint            | URL                                    |
|---------------------|----------------------------------------|
| API                 | http://localhost:8080/api/v1           |
| Swagger UI          | http://localhost:8080/swagger/         |
| Prometheus          | http://localhost:9090                  |
| Grafana (admin/admin) | http://localhost:3000                |

Try it:

```bash
# register and log in
curl -X POST localhost:8080/api/v1/register \
  -d '{"email":"ada@example.com","name":"Ada","password":"password123"}'
TOKEN=$(curl -s -X POST localhost:8080/api/v1/login \
  -d '{"email":"ada@example.com","password":"password123"}' | jq -r .access_token)

# create a team and a task
curl -X POST localhost:8080/api/v1/teams \
  -H "Authorization: Bearer $TOKEN" -d '{"name":"backend"}'
curl -X POST localhost:8080/api/v1/tasks \
  -H "Authorization: Bearer $TOKEN" -d '{"team_id":1,"title":"ship it"}'

# complete it and read the audit trail
curl -X PUT localhost:8080/api/v1/tasks/1 \
  -H "Authorization: Bearer $TOKEN" -d '{"title":"ship it","status":"done"}'
curl -H "Authorization: Bearer $TOKEN" localhost:8080/api/v1/tasks/1/history
```

## API

| Method & path                      | Description                                | Access |
|------------------------------------|--------------------------------------------|--------|
| `POST /api/v1/register`            | Register                                   | public (IP rate limit) |
| `POST /api/v1/login`               | Log in, returns JWT                        | public (IP rate limit) |
| `GET /api/v1/me`                   | Current user with global role              | authenticated |
| `POST /api/v1/teams`               | Create team, creator becomes owner         | authenticated |
| `GET /api/v1/teams`                | Teams the user belongs to                  | authenticated |
| `POST /api/v1/teams/{id}/invite`   | Invite user by email                       | team owner/admin |
| `POST /api/v1/tasks`               | Create task                                | team member |
| `GET /api/v1/tasks`                | List with filters + cursor pagination      | team member |
| `PUT /api/v1/tasks/{id}`           | Update task (audited)                      | team member |
| `GET /api/v1/tasks/{id}/history`   | Change history                             | team member |
| `GET /api/v1/analytics/*`          | Team stats / top creators / integrity      | global admin |

Full contract with schemas: **Swagger UI** at `/swagger/`.

Authorization model: the JWT carries identity only (`sub`, no roles — they would go
stale). Team roles are resolved per request from `team_members`; the global `admin`
bypass lives in a single authorizer used by every service.

## Admin CLI

The first admin is created by an operator, not by env variables:

```bash
# register a user through the API first, then:
go run ./cmd/cli grant-admin --email ada@example.com
```

The command is idempotent and reuses the application config (`CONFIG_PATH`).

## Configuration

The YAML file is the single source of truth, mounted into the container
(`CONFIG_PATH=/config.yaml`) — the same model as a Kubernetes ConfigMap. Environment
variables are used only for what never lives in the file: `CONFIG_PATH`, `ENV`,
`APP_VERSION`. See [config.yaml](config.yaml) for all knobs: HTTP timeouts, MySQL pool,
Redis, JWT secret/TTL, rate limits, circuit breaker thresholds, shutdown budgets.

## Testing

```bash
task test        # everything, incl. integration (needs Docker)
task test-unit   # unit tests only
task cover       # coverage across all tests
```

The test strategy is sociable-first: the main suite spins real MySQL and Redis via
testcontainers, builds the actual application stack and exercises it through the HTTP
API. Mocks exist only for the unmanaged dependency (the external email service — an
`httptest` mock server with switchable failure mode). Unit tests cover pure logic:
JWT, middlewares, lifecycle, readiness states.

## Development

```bash
task -l              # all tasks
task check           # gofumpt + golangci-lint
task swagger         # regenerate OpenAPI from annotations
task pre-commit-install  # git hooks: fmt, lint, tidy, tests, swagger freshness
```

CI runs four parallel jobs on every PR commit (`lint`, `govulncheck`, `test-unit`,
`test-integration`); merges to `main` release automatically via semantic-release
(conventional commits → semver tag + changelog).

## Design decisions

- **Keyset over offset pagination** — `WHERE id > cursor ORDER BY id LIMIT n` seeks by
  index instead of reading and discarding offset rows; verified with `EXPLAIN ANALYZE`
  on 3M+ rows (query plans went from ~40k rows scanned per page to exactly `limit`).
  Composite indexes `(team_id, id)` / `(team_id, status, id)` carry the list queries;
  a `FORCE INDEX` hint pins the plan where the MySQL optimizer mispredicts on
  cursor+LIMIT shapes.
- **Audit in the write transaction** — application-level audit (not triggers, not CDC)
  keeps the actor and business intent, and the same-transaction write makes history
  drift impossible.
- **No task status state machine** — any-to-any transitions, like GitHub Issues and
  Linear: users make mistakes and must be able to roll a status back. `completed_at`
  is managed on the `done` boundary.
- **Local circuit breaker, fresh-start closed** — per-instance state is the canonical
  choice (Hystrix/resilience4j/Polly); a shared-state breaker would add a Redis
  round-trip and a new failure mode to every call for a best-effort email.
- **Centralized rate limiting** — GCRA counters live in Redis, which solves
  multi-replica fairness and restart persistence with one mechanism. Fails open:
  the limiter is protection, not a feature.
- **Analytics tradeoffs** — top-creators computes window functions over a month of
  tasks per page of teams (~seconds on millions of rows, admin-only); the integrity
  report is a full-table anti-join by design. Both are documented rather than
  prematurely materialized.

## Project layout

```
cmd/api, cmd/cli          entrypoints
internal/
  domain/                 entities and domain errors, zero dependencies
  service/                business logic, owns repository interfaces, single authorizer
  identity/               embedded IdP: JWT issue/parse, request principal
  infrastructure/         mysql, redis cache, rate limiter, email breaker,
                          lifecycle (starter/closer), health, config, logging
  transport/http/         chi router; handlers grouped per resource
                          (auth, teams, tasks, analytics) with ozzo-validated DTOs
migrations/               goose SQL migrations (run by the migrator container)
tests/integration/        sociable API tests on testcontainers
deploy/                   prometheus + grafana provisioning
```
