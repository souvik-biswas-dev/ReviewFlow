.PHONY: setup run build test test-e2e generate fmt vet

# First-time setup: resolve dependencies and write go.sum.
setup:
	go mod tidy

# Run the API (reads ../.env automatically). Needs MongoDB, see ../README.md.
run:
	go run ./cmd/server

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/server ./cmd/server

# Unit tests (hub, AI pipeline). Add -race if you have a C toolchain.
test:
	go test ./...

# Full end-to-end test: needs a MongoDB on localhost:27017.
test-e2e:
	MONGO_TEST_URI=mongodb://localhost:27017 go test -race -count=1 ./...

# Regenerate GraphQL code after editing internal/graph/schema.graphqls.
generate:
	go run github.com/99designs/gqlgen generate

fmt:
	gofmt -l -w .

vet:
	go vet ./...
