run:
	go run ./cmd/goro

docker-up:
	cd docker && docker-compose up --build