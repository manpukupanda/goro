# Goro

Lightweight video processing backend (Go + SQLite + S3).

## Development

```
make docker-up
```

API → http://localhost:8080/healthz  
Nginx → http://localhost/

## Configuration

System-level settings are loaded from `configs/config.yaml`:

- S3/MinIO connection settings
- HLS profiles (e.g. 1080p / 720p / 480p)
