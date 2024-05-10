# Build variables
PACKAGE = github.com/l7mp/stunner
BUILD_DIR ?= bin/
VERSION ?= $(shell (git describe --tags --abbrev=8 --always --long) | tr "/" "-")
COMMIT_HASH ?= $(shell git rev-parse --short HEAD 2>/dev/null)
BUILD_DATE ?= $(shell date +%FT%T%z)
LDFLAGS += -X main.version=${VERSION} -X main.commitHash=${COMMIT_HASH} -X main.buildDate=${BUILD_DATE}

ifeq (${VERBOSE}, 1)
ifeq ($(filter -v,${GOARGS}),)
	GOARGS += -v
endif
endif

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
test: generate fmt vet
	go test ./... -v

##@ Build

.PHONY: build
build: generate fmt vet
	go build ${GOARGS} -ldflags "${LDFLAGS}" -o ${BUILD_DIR}/stunnerd cmd/stunnerd/main.go
	go build ${GOARGS} -o ${BUILD_DIR}/turncat cmd/turncat/main.go

.PHONY: clean
clean:
	echo 'Use "make generate` to autogenerate server code' > pkg/server/server.go
	echo 'Use "make generate` to autogenerate client code' > pkg/client/client.go
	echo 'Use "make generate` to autogenerate client code' > pkg/types/types.go
