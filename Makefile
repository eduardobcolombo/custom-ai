# Start the database
up-db:
	docker-compose up -d

# Stop the database
down:
	docker-compose down

# Run the Go application
run:
	go run ./cmd/bifrost/main.go

# Spin up database and then run the application
up: up-db run

up-mem:
	USE_IN_MEMORY=true go run ./cmd/bifrost/main.go

