# Appraisal CRM — review-service

Commercial project built for a real client. Code goes to production — treat it accordingly.

**This repo is `review-service`.** `request-service` is the reference
implementation — follow its layout and conventions. This file carries the
project-wide charter (each service is a separate repo) plus this service's specifics.

## What it does

CRM for a property appraisal company (apartments, houses, land, vehicles, commercial real estate).
Digitizes the full cycle: client submits request → inspector visits the property → appraiser evaluates → client receives report.

`review-service` owns the appraisal step: it creates an appraisal when a request
enters the appraisal stage, lets the appraiser record comparables, notes and the
market value, register the report file, and emits `report.ready` when done.

> **Calculation engine:** the apartment appraisal calculation engine (comparative
> approach with 4 analogs) is implemented. Formula parameters, baseline district prices,
> scale function constants, repair classes, and floor matrix are stored in `formula_configs`
> and editable by administrators (`admin` role). Other property types remain manual.

## Roles

| Role          | What they do in the system                                                         |
|---------------|------------------------------------------------------------------------------------|
| Client        | Submits a request, tracks status, downloads the final report                       |
| Appraiser     | Accepts requests, assigns an inspector, conducts appraisal, sends the report       |
| Inspector     | Receives field assignments, uploads photos and property data                       |
| Administrator | Manages users, monitors the system                                                 |

## Request lifecycle (strictly linear — no going back)

```
New → In Progress → Inspection Scheduled → Inspection Completed → Appraisal → Report Sent → Closed
```

The request state machine lives in request-service. review-service reacts to the
`appraisal` transition and, on completion, emits the event that lets
request-service advance to `report_sent`.

## Stack

| Layer              | Technology                               |
|--------------------|------------------------------------------|
| Backend services   | Go (chi, pgx, golang-migrate)            |
| Database per svc   | PostgreSQL (Database-per-Service pattern)|
| Auth               | Keycloak 26 (OAuth2/OIDC)               |
| Events             | Apache Kafka                             |
| Cache / Dedup      | Redis                                    |
| Object storage     | S3 Yandex Cloud                          |
| Frontend (4 SPAs)  | React + TypeScript                       |
| Architecture docs  | Structurizr DSL (C4)                     |

## Review Service (this repo)

- **Consumer + producer.** Consumes `request.status_changed` (topic
  `request.events`); produces `report.ready` (topic `review.events`) via a
  transactional **outbox**.
- **Aggregate:** `appraisals` (one per request, `request_id` UNIQUE, `calculation_data` JSONB) +
  `comparables` (free-form JSONB) + `formula_configs` (admin-managed formula parameters) +
  `appraisal_statuses` lookup.
- **Appraisal state machine:** `in_progress → completed` only. Completion is the
  single event-producing transition (CAS on `status = 'in_progress'`). A completed
  appraisal is **frozen** — update/comparables/report are rejected (422).
- **Calculation engine & endpoints:** apartment appraisal comparative engine with 8 steps
  (bargaining, district prices, area scale function, repair/condition, floor matrix, weights).
  - `POST /calculate/apartment` — instant calculation breakdown.
  - `POST /appraisals/{id}/calculate-apartment` — calculate, save `calculation_data`, update `market_value` and sync `comparables`.
  - `GET /settings/apartment-formula`, `PUT /settings/apartment-formula` — admin formula & coefficient configuration.
- **Market value:** NUMERIC input or calculated result, carried as a string in Go/JSON (no floats).
- **Reports:** binaries live in S3; only the object key is stored. Uploads go
  through a presigned URL. `internal/storage` is a stub until the S3 SDK is wired.
- **Consumer idempotency:** dedup by `event_id` in **Redis** — check before,
  mark only **after** successful processing (a crash never drops the event).
  Creation is additionally idempotent per request via the `request_id` UNIQUE
  constraint (`INSERT ... ON CONFLICT DO NOTHING`).
- **Access control:** role-only (`appraiser`, `admin`) — per the BRD an appraiser
  has access to every request, so there are no per-row ownership checks.
- **DB:** `review_db`. **Default port:** `8084`.

### How an appraisal is born

`review-service` does not expose a "create appraisal" endpoint. When
request-service moves a request into `appraisal`, the resulting
`request.status_changed` event creates the appraisal row (status `in_progress`,
`appraiser_id` NULL). An appraiser then takes it via `PATCH /appraisals/{id}`.

## Go module path

Not a monorepo. Every service is a separate repository under the `appraisal-crm`
GitHub organization:
```
github.com/appraisal-crm/review-service
github.com/appraisal-crm/<name>-service   # pattern for services
```

## Go service structure (request-service is the reference layout)

```
review-service/
  cmd/server/          # entry point, wire DI
  internal/
    domain/            # entities, domain errors (errors.go), events
    repository/        # interface + PostgreSQL implementation
    service/           # business logic, appraisal state machine
    handler/           # HTTP (chi router), DTOs
    middleware/        # JWT auth, role-based access
    httputil/          # shared response helpers
    outbox/            # producer + relay (report.ready)
    kafka/             # consumer group (request.status_changed)
    dedup/             # Redis event_id dedup (check-then-mark)
    storage/           # S3 report storage (stub)
  config/              # ENV config (os.Getenv only)
  migrations/          # SQL files (golang-migrate up/down)
  api/                 # Swagger (swaggo/swag, generated, gitignored)
```

## Code rules

- No magic frameworks — chi, pgx, playground/validator only
- Config via `os.Getenv` only — no viper, no cobra
- Migration files: `000001_<description>.up.sql` / `.down.sql` — sequential, always both up and down
- JWT validation via Keycloak JWKS: `MicahParks/keyfunc` + `JWKS_URL` env var; RS256 allow-list + required `exp`
- Domain errors in `domain/errors.go`; map to HTTP status codes in the handler layer only
- Each Kafka event is a distinct type in `domain/events.go`
- Publish events via the transactional outbox — write to `outbox` in the same tx as the state change; never publish from a handler
- Kafka consumers MUST be idempotent — dedup by `event_id`, and mark the id only after successful processing
- Optimistic locking for concurrent mutations (CAS on `updated_at` / `status`)
- Swagger annotations required for all public endpoints
- Unit tests for business logic in `service/`

## Kafka events

**One topic per producing service** — events are told apart by `event_type`, NOT by topic.

| event_type               | Topic            | Producer        | This service |
|--------------------------|------------------|-----------------|--------------|
| `request.status_changed` | `request.events` | request-service | consumes     |
| `report.ready`           | `review.events`  | review-service  | produces     |

**Event conventions:**
- **Message key** = aggregate id (`request_id`) → per-request ordering within a partition
- **Format** = JSON envelope: `event_id`, `event_type`, `version`, `occurred_at`, `request_id`, `data{}`
- **Delivery** = at-least-once → consumers MUST be idempotent (dedup by `event_id`)
- **Schema evolution** = additive only; bump `version` for breaking changes
- **Broker** = KRaft mode (no Zookeeper)

## Commands

```bash
# Start shared infra (Kafka/Keycloak) once, then this service's data infra
docker compose -f ../infra/docker-compose.yml up -d
docker compose up -d

# Run the service (needs DATABASE_URL + REDIS_ADDR — see .env.example)
make run          # or: go run cmd/server/main.go

# Tests
make test         # go test ./...

# Migrations (DB_URL defaults to local review_db)
make migrate-up
make migrate-down
```

## Hard rules — do not break

- No synchronous cross-service calls between business services — events via Kafka only
- No cross-database JOINs between services
- Other property types (houses, land, commercial) remain manual until their specs land
- Do not switch config from `os.Getenv` to viper without explicit agreement
- Never modify already-applied migrations — new additive migrations only
- Never publish to Kafka straight from a handler/service — go through the outbox

## Workflow

- Tasks tracked in Jira, project **ACRM** (mdrslv.atlassian.net)
- Branch from `dev`: `feature/<scope>` / `fix/<scope>`; PR into `dev`
- `main` is the release branch — updated by merging `dev` → `main`; never PR features directly into `main`
- Conventional commits with the Jira key: `feat(review): ... (ACRM-XX)`
