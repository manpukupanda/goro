build-admin:
	cd internal/admin/ui && npm ci && npm run build

run:
	go run ./cmd/goro

docker-up:
	cd docker && docker-compose up --build