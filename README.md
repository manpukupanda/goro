# Goro

Goro is a small-scale, self-hosted video management server for teams that want to build their own frontend and use an API-first backend.

It is designed for simple deployment and operations on a single VPS, while still allowing object storage growth through S3-compatible backends.

## What Goro is for

- Building your own product UI while delegating video processing and delivery to Goro
- Replacing part of a video SaaS workflow with a self-hosted backend
- Running lightweight production workloads with minimal operational complexity

## Product shape (API-first)

- **Primary interface:** Public API (for your frontend/system integration)
- **Secondary interface:** Admin console (for manual checks and corrections during development/operations)

In other words, daily product integration should happen through the API; the admin console is operational support tooling.

## Quick start (Docker)

1. Create environment file:

   ```bash
   cp docker/.env.example docker/.env
   ```

2. Edit `docker/.env` and set secure values (at minimum):
   - `GORO_S3_ACCESS_KEY`
   - `GORO_S3_SECRET_KEY`
   - `GORO_SECURE_LINK_SECRET`
   - `GORO_API_KEY`
   - `GORO_ADMIN_PASSWORD`

3. Start all services:

   ```bash
   make docker-up
   ```

4. Check endpoints (default Docker Compose ports):
   - API health: `http://localhost:5600/healthz`
   - Admin console: `http://localhost:5601/admin`
   - Nginx entrypoint: `http://localhost/`

## API usage basics

- API key auth uses `Authorization: Bearer <GORO_API_KEY>`
- Typical API flow:
  1. Upload video (`POST /videos`)
  2. Poll/list processing status (`GET /videos`)
  3. Get playback playlist (`GET /videos/:id/playlist`)
  4. Manage visibility/tokens as needed

## Operations notes (small-scale production)

- **Backup is simple:** aside from object storage data, core state lives in SQLite.
  - In Docker Compose setup, DB path is `/app/data/goro.db` in the goro container (mapped from host `./data/goro.db` under the repository root).
- **Scaling expectation:** optimized for single-VPS operation.
  - Compute remains on one server.
  - Storage can scale independently by using S3-compatible object storage.
- **Monitoring:** no built-in monitoring suite is provided yet.

## Configuration

Default system configuration is embedded at build time:

- `internal/config/default_config.yaml`

You can override it by setting `GORO_CONFIG` to a custom YAML file path before starting Goro.

## Development

- Run app directly:

  ```bash
  go run ./cmd/goro
  ```

- Build admin UI:

  ```bash
  cd internal/admin/ui && npm ci && npm run build
  ```
