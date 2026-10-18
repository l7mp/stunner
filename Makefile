# Build variables
PACKAGE = github.com/l7mp/stunner/v2
BUILD_DIR ?= bin/
VERSION ?= $(shell (git describe --tags --abbrev=8 --always --long) | tr "/" "-")
COMMIT_HASH ?= $(shell git rev-parse --short HEAD 2>/dev/null)
BUILD_DATE ?= $(shell date +%FT%T%z)
LDFLAGS += -s -w
LDFLAGS += -X main.version=${VERSION} -X main.commitHash=${COMMIT_HASH} -X main.buildDate=${BUILD_DATE}
GOARGS = -trimpath

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

# The eBPF offload engines exist only in the official build: in a tree without them the offload
# targets do nothing. The codegen also skips on a host without clang (the generated objects are
# committed).
OFFLOAD := $(wildcard internal/offload/xdp/xdp.c)

.PHONY: fetch-libbpf-headers generate-offload format-offload clean-offload purge-offload test-offload
ifneq ($(OFFLOAD),)
fetch-libbpf-headers:
	@if ! find internal/offload/headers/bpf_* >/dev/null 2>&1; then \
		cd internal/offload/headers && ./fetch-libbpf-headers.sh; \
	fi

generate-offload: fetch-libbpf-headers ## cilium/ebpf codegen of the offload engines
	@if ! command -v clang >/dev/null; then echo "clang not found: skipping the eBPF codegen"; exit 0; fi; \
	cd internal/offload/xdp && go generate && cd ../tc && go generate

format-offload:
	clang-format -i --style=file:internal/offload/.clang-format \
		internal/offload/xdp/xdp.c internal/offload/tc/tc.c

clean-offload:
	rm -vf internal/offload/xdp/xdp_bpfe*.o internal/offload/xdp/xdp_bpfe*.go
	rm -vf internal/offload/tc/tc_bpfe*.o internal/offload/tc/tc_bpfe*.go

purge-offload: clean-offload
	rm -vf internal/offload/headers/bpf_*

# Loads the XDP and tc programs onto the loopback interface and pushes crafted packets through
# them, so it needs root. internal/offload is the only package carrying these build tags, and
# running ./... here would put the whole suite through two extra root runs for nothing.
test-offload:
	sudo -E go test ./internal/offload/ -tags offloadtc -count=1 -v
	sudo -E go test ./internal/offload/ -tags offloadxdp -count=1 -v
else
fetch-libbpf-headers generate-offload format-offload clean-offload purge-offload test-offload:
	@:
endif

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: generate fmt vet
	go test ./... -v

.PHONY: lint
lint: ## Run golangci-lint against code.
	golangci-lint run

##@ Build

.PHONY: build
build: generate generate-offload fmt vet build-bin

.PHONY: build-bin
bin: build-bin
build-bin:
	go build ${GOARGS} -ldflags "${LDFLAGS}" -o ${BUILD_DIR}/stunnerd ./cmd/stunnerd
	go build ${GOARGS} -ldflags "${LDFLAGS}" -o ${BUILD_DIR}/stunnerctl ./cmd/stunnerctl
	go build ${GOARGS} -ldflags "${LDFLAGS}" -o ${BUILD_DIR}/icetester ./cmd/icetester
	go build ${GOARGS} -ldflags "${LDFLAGS}" -o ${BUILD_DIR}/softphone ./cmd/softphone

.PHONY: clean
clean:
	# echo 'Use "make generate" to autogenerate server code' > pkg/server/server.go
	# echo 'Use "make generate" to autogenerate client code' > pkg/client/client.go
	# echo 'Use "make generate" to autogenerate client code' > pkg/types/types.go
