<div align="center">

# Task Tracker

Team task management REST API — role-based access, a transactional outbox, and first-class observability.

Go · chi · MySQL · Redis · Prometheus · Grafana · testcontainers

[![CI](https://img.shields.io/github/actions/workflow/status/alextuchak/task_tracker/ci.yaml?style=flat-square&label=ci)](https://github.com/alextuchak/task_tracker/actions/workflows/ci.yaml)
[![Go Report Card](https://goreportcard.com/badge/github.com/alextuchak/task_tracker?style=flat-square)](https://goreportcard.com/report/github.com/alextuchak/task_tracker)
[![Go Version](https://img.shields.io/github/go-mod/go-version/alextuchak/task_tracker?style=flat-square)](go.mod)
[![Release](https://img.shields.io/github/v/release/alextuchak/task_tracker?style=flat-square)](https://github.com/alextuchak/task_tracker/releases)
[![License](https://img.shields.io/badge/license-MIT-yellow?style=flat-square)](LICENSE)

[Features](#features) · [Quick start](#quick-start) · [Architecture](#architecture) · [Design decisions](#design-decisions) · [API](#api) · [Observability](#observability) · [Testing](#testing) · [Configuration](#configuration) · [Development](#development)

<img src="docs/img/grafana.jpg" alt="Grafana dashboard under ~900 rps" width="800">

</div>

## Features

- **Teams & RBAC** — team roles resolved per request, global admin bypass in one authorizer
- **Audited tasks** — field-level history written in the same transaction as the update
- **Transactional outbox** — invite emails queued in the invite's transaction, drained by a
  background relay with dedup, retries and dead-letter
- **Keyset pagination** — cursor-based everywhere, no deep-page degradation
- **Redis** — task list cache (5 min TTL) and GCRA rate limiting (per-user and per-IP)
- **Admin analytics** — window functions, aggregations and an integrity anti-join report
- **Observability** — Prometheus metrics, provisioned Grafana dashboard, JSON logs
- **Operational hygiene** — two-phase graceful shutdown, startup pings, migration container

## Quick start

```bash
docker compose up -d --build
```

One command starts MySQL, Redis, the migrator, the API, Prometheus and Grafana.

| Endpoint   | URL                            | Credentials |
|------------|--------------------------------|-------------|
| API        | http://localhost:8080/api/v1   | —           |
| Swagger UI | http://localhost:8080/swagger/ | —           |
| Prometheus | http://localhost:9090          | —           |
| Grafana    | http://localhost:3000          | admin/admin |

```bash
# register and log in
curl -X POST localhost:8080/api/v1/register \
  -H "Content-Type: application/json" \
  -d '{"email":"ada@example.com","name":"Ada","password":"password123"}'
TOKEN=$(curl -s -X POST localhost:8080/api/v1/login \
  -H "Content-Type: application/json" \
  -d '{"email":"ada@example.com","password":"password123"}' | jq -r .access_token)

# create a team and a task, then complete it
curl -X POST localhost:8080/api/v1/teams \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '{"name":"backend"}'
curl -X POST localhost:8080/api/v1/tasks \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '{"team_id":1,"title":"ship it"}'
curl -X PUT localhost:8080/api/v1/tasks/1 \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '{"title":"ship it","status":"done"}'

# read the audit trail
curl -H "Authorization: Bearer $TOKEN" localhost:8080/api/v1/tasks/1/history
```

```json
[
  {"change_group_id": "7f3a9c1e…", "field": "created", "old_value": "", "new_value": "", "changed_by": 1},
  {"change_group_id": "b1e4d206…", "field": "status", "old_value": "todo", "new_value": "done", "changed_by": 1}
]
```

Fields changed by one request share a `change_group_id` — Jira-style change groups.

## Architecture

```mermaid
flowchart LR
    C[Client] --> MW

    subgraph app [task-tracker]
        MW[middleware<br/>metrics · logging · auth · rate limit] --> H[handlers<br/>auth · teams · tasks · analytics]
        H --> S[services<br/>business logic · authorizer]
        S --> R[repositories]
        OB[outbox relay<br/>claim → send → settle] --> R
        OB -. circuit breaker .-> E[email provider<br/>mock, in-process]
    end

    R --> M[(MySQL)]
    MW -. rate limit .-> RC[(Redis)]
    S -. cache .-> RC
    OB -. dedup .-> RC
    P[Prometheus] -- scrape /metrics --> MW
    G[Grafana] --> P
```

JWT middleware resolves identity without touching the database; authorization
(team roles, admin bypass) happens in the service layer per request. Repositories
own raw SQL; the audit trail is written in the same transaction as the update. The
outbox relay runs alongside the API, draining queued emails independently of any
request. Only MySQL is on the critical path — Redis (cache, rate limits, dedup)
fails open.

## Design decisions

- **Keyset over offset pagination** — index seek instead of read-and-discard; verified
  with `EXPLAIN ANALYZE` on 3M+ rows (~40k scanned rows per page down to exactly `limit`)
- **Audit in the write transaction** — application-level audit keeps the actor and
  intent; same-transaction writes make history drift impossible
- **No task status state machine** — any-to-any transitions like GitHub Issues;
  users must be able to roll a status back
- **Outbox over inline email** — the send is queued in the invite's transaction, so the
  email can't be lost or block the request; a relay then claims a batch with `FOR UPDATE
  SKIP LOCKED` and holds those locks for the whole tick, so the lock *is* the claim —
  no in-flight status, no lease, no reaper, and a crash simply rolls the tick back and
  leaves the rows pending. `outbox.budget` caps how long a tick may hold them
- **One provider call per shard** — the batch is split across workers and each worker
  sends its slice in a single request, so a tick of 100 mails costs 4 HTTP calls rather
  than 100 in parallel; anything else burns the provider's rate limit, and burns it
  faster the more workers you add. Failing the call is the downstream's fault — the relay
  pauses and spends no attempts; a per-message verdict is that message's own fault and
  costs it one
- **One tick, one transaction, two contexts** — claim, send and settle share a single
  transaction, but the sends run on a context that dies with the app while the settle runs
  on `context.WithoutCancel` plus a grace window. Shutdown (or an exhausted budget) stops
  sending at once and still records what already left; without that split the bookkeeping
  for mail the provider has accepted would be rolled back, leaving dedup as the only thing
  preventing a resend
- **Batched settle** — per-row delays and error texts ride in `CASE` arms, so a claim of
  100 rows costs one `UPDATE` per outcome class instead of 100 round trips
- **Redis dedup marker, MySQL stays the source of truth** — each send writes a short-lived
  `outbox:sent:{id}` marker before the aggregate commit, so a crash between send and commit
  doesn't re-send; the marker is best-effort, MySQL remains authoritative
- **Redis is a non-critical dependency** — cache, rate limiter and outbox dedup all fail
  open, so a Redis outage degrades (not breaks) the service; only MySQL gates readiness
  and startup
- **Local circuit breaker, fresh-start closed** — per-instance state is the canonical
  choice; a shared breaker would add a Redis round-trip to every email send
- **Centralized rate limiting** — GCRA counters in Redis solve multi-replica fairness
  and restart persistence at once; fails open, the limiter is protection, not a feature;
  trusted CIDRs (load balancers, internal infra) bypass only the anonymous per-IP limit
- **Documented tradeoffs over premature optimization** — top-creators runs window
  functions over live data (admin-only, seconds on millions of rows); the integrity
  report is a full-table anti-join by design

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

The JWT carries identity only (`sub`, no roles — they would go stale). Team roles
are resolved per request from the database; the global admin bypass lives in a
single authorizer used by every service.

## Observability

Prometheus scrapes `/metrics` every 5s; Grafana auto-provisions the dashboard
shown above: an instant-value row, request rate, error rate, latency percentiles
(log scale) and runtime stats. The demo stack sustains ~900 rps locally with
p95 around 23ms and p99 around 120ms:

```bash
task loadgen   # 600 users, each within the 100 req/min per-user limit, for 3 minutes
```

Rate limits stay at their production values during the demo: throughput comes
from many users, not from lifting the limits. Localhost and the Docker network
are in `trusted_cidrs`, so only the anonymous per-IP limit is bypassed for the
generator — as it would be for a load balancer in production.

## Testing

```bash
task test        # everything, incl. integration (needs Docker)
task test-unit   # unit tests only
task cover       # coverage across all tests
```

The test strategy is sociable-first: the main suite spins real MySQL and Redis via
testcontainers, builds the actual application stack and exercises it through the HTTP
API. The only stub is the unmanaged dependency — the email provider is mocked in-process
with no network call, and a test double at that seam drives the relay's retry, dedup and
dead-letter paths against a real database. Unit tests cover pure logic: JWT, middlewares,
lifecycle, readiness states.

## Configuration

The YAML file is the single source of truth, mounted into the container
(`CONFIG_PATH=/config.yaml`). Environment variables carry only what never lives in
the file: `CONFIG_PATH`, `ENV`, `APP_VERSION`. See [config.yaml](config.yaml) for all
knobs: HTTP timeouts, MySQL pool, Redis, JWT secret/TTL, rate limits, circuit breaker
thresholds, outbox relay (tick, budget, dedup TTL, batch, workers, max attempts),
shutdown budgets.

The first admin is granted by an operator, not by env variables:

```bash
go run ./cmd/cli grant-admin --email ada@example.com
```

## Development

```bash
task -l                  # all tasks
task check               # gofumpt + golangci-lint
task swagger             # regenerate OpenAPI from annotations
task pre-commit-install  # hooks: fmt, lint, tidy, tests, swagger freshness
```

CI runs five parallel jobs on every PR commit — `lint` (golangci-lint), `govulncheck`,
`gosec`, `test-unit`, `test-integration` — and merges to `main` release automatically
via semantic-release (conventional commits → semver tag + changelog).

## Project layout

```text
cmd/                      api, cli (admin ops), loadgen (demo traffic)
internal/
  domain/                 entities and domain errors, zero dependencies
  service/                business logic, owns repository interfaces, single authorizer
  identity/               embedded IdP: JWT issue/parse, request principal
  infrastructure/         mysql, redis (cache · dedup), rate limiter, email breaker,
                          outbox relay, lifecycle (starter/closer), health, config
  transport/http/         chi router; handlers grouped per resource
                          (auth, teams, tasks, analytics) with ozzo-validated DTOs
migrations/               goose SQL migrations (run by the migrator container)
tests/integration/        sociable API tests on testcontainers
deploy/                   prometheus + grafana provisioning
```

## License

[MIT](LICENSE)
