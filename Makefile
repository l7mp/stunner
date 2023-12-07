.PHONY: all
all: build

.PHONY: generate
generate: ## OpenAPI codegen
	go generate ./pkg/config/...

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet test ## Run tests.

##@ Build

.PHONY: build
build: generate fmt vet ## Build binary.
	go build -o bin/stunnerd cmd/stunnerd/main.go
	go build -o bin/turncat cmd/turncat/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run cmd/stunnerd/main.go

# clean up generated files
.PHONY: clean
clean:
	echo 'Use "make generate` to autogenerate server code' > pkg/server/server.go
	echo 'Use "make generate` to autogenerate client code' > pkg/client/client.go
	echo 'Use "make generate` to autogenerate client code' > pkg/types/types.go
