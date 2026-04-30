# Goro

Lightweight video processing backend (Go + SQLite + S3).

## Development

Before starting for the first time, create a `.env` file in the `docker/` directory:

```
cp docker/.env.example docker/.env
```

The `.env.example` file contains a development-safe default value for `SECURE_LINK_SECRET`.
Edit `docker/.env` if you want to use a different value.

Then start the stack:

```
make docker-up
```

API → http://localhost:8080/healthz  
Nginx → http://localhost/

## Configuration

System-level settings are embedded in the binary at build time (`internal/config/default_config.yaml`).

- S3/MinIO connection settings
- HLS profiles (e.g. 1080p / 720p / 480p)

To override the defaults, point the `GORO_CONFIG` environment variable to a custom YAML file before starting the server.
