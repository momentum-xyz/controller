all: build

sql-up:
	docker compose up -d

sql-down:
	docker compose down

build:
	go build -o ./bin/controller ./apps/service

run: build
	./bin/controller

tester:
	go build -o ./bin/tester ./apps/tester

test:
	go test -v -race ./...

# docker run ...
docker:
	echo "Not implemented"

.PHONY: build run sql-up sql-down test docker tester
