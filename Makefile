GO ?= go
PYTHON ?= python3
IMAGE ?= objectstoreviewer:dev
GOVULNCHECK_VERSION ?= v1.7.0
GOLANGCI_LINT_VERSION ?= v2.13.1
GOLANGCI_LINT ?= $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
FUZZ_TIME ?= 5s
DIST_DIR ?= dist
ARTIFACT_DIR ?= artifacts
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test test-api test-fuzz test-race test-stress test-integration test-barman-fixtures test-s3 test-azure test-gcs test-provider-parity test-scale test-container test-multiarch lint golangci-lint check-api vuln check docs package docker-build supply-chain release-check generate-evidence-artifacts generate-brand-assets

build:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -o bin/objectstoreviewer ./cmd/objectstoreviewer

test: test-api
	$(GO) test ./...

test-api:
	$(GO) -C api test ./...

# Exercise each untrusted-input fuzz boundary with one worker and a fixed time
# budget. Ordinary `make test` still runs every committed seed as a unit test.
test-fuzz:
	$(GO) test ./internal/formats/barmancloud -run '^$$' -fuzz '^FuzzParseBackupInfo$$' -fuzztime=$(FUZZ_TIME) -parallel=1
	$(GO) test ./internal/formats/barmancloud -run '^$$' -fuzz '^FuzzParseTimelineHistory$$' -fuzztime=$(FUZZ_TIME) -parallel=1
	$(GO) test ./internal/formats/barmancloud -run '^$$' -fuzz '^FuzzParseWALName$$' -fuzztime=$(FUZZ_TIME) -parallel=1
	$(GO) test ./internal/provider/cursor -run '^$$' -fuzz '^FuzzCodec$$' -fuzztime=$(FUZZ_TIME) -parallel=1

test-race:
	$(GO) -C api test -race ./...
	$(GO) test -race ./...

# Repeat the publication, channel, runtime, and probe concurrency cases. A
# single full race pass remains useful, while repetition catches lifecycle
# failures that only appear across interleavings.
test-stress:
	$(GO) test -race ./internal/evidenceapi -run '^(TestEngine|TestNewEngine|TestEvidenceHandler|TestLoadTokenFile|TestListenUnix|TestServeUnix|TestUnixHTTP)' -count=10
	$(GO) test -race ./internal/application ./internal/config ./internal/inventory ./internal/provider/s3 -run '^(TestSidecar|TestLoad.*Sidecar|TestLoadStandaloneRejects|TestNewAcceptsExplicitWebIdentity|TestScanner)' -count=10
	$(GO) test -race ./cmd/objectstoreviewer ./internal/evidenceapi -run '^(TestRunProbe|TestProbeHealth)' -count=10

# These tests create pinned Barman and provider-emulator environments. They are
# intentionally separate from the hermetic unit suite because they need Docker
# and network access to pull pinned images.
test-integration: test-barman-fixtures test-provider-parity

test-barman-fixtures:
	./hack/test-barman-fixtures.sh

test-s3:
	./hack/test-s3.sh

test-azure:
	./hack/test-azure.sh

test-gcs:
	./hack/test-gcs.sh

test-provider-parity:
	./hack/test-provider-parity.sh

test-scale:
	$(GO) test -tags=scale ./internal/inventory -run '^TestScannerMillionObjectSnapshotIsBounded$$' -count=1 -v
	$(GO) test ./internal/web -run '^$$' -bench '^BenchmarkHandlerCachedSummary$$' -benchmem -benchtime=100x

test-container: docker-build
	./hack/test-container.sh $(IMAGE)
	./hack/test-evidence-sidecar-container.sh $(IMAGE)

test-multiarch:
	./hack/test-multiarch.sh $(ARTIFACT_DIR)/release

lint: golangci-lint
	test -z "$$(gofmt -l $$(find . -name '*.go' -type f))"
	$(GO) -C api vet ./...
	$(GO) vet ./...
	./hack/check-boilerplate.sh
	./hack/check-readonly.sh
	./hack/check-go-version.sh
	$(GO) test ./internal/store -run '^TestReaderSurface$$'

# golangci-lint carries the enforced gosec pass. Both modules are linted with
# the same configuration; the api module is a separate go.mod.
golangci-lint:
	$(GOLANGCI_LINT) run --timeout 10m ./...
	cd api && $(GOLANGCI_LINT) run --config ../.golangci.yml --timeout 10m ./...

# The API module must stay dependency-free and generated wire artifacts must be
# byte-for-byte current. Its ordinary behavior remains covered by make test.
check-api:
	test "$$($(GO) -C api list -m all | wc -l)" -eq 1
	$(GO) -C api run ./cmd/evidence-artifacts -check

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

# Complete local validation that does not require Docker.
check: lint test test-fuzz test-race test-stress check-api vuln

docs:
	cd web && npm ci && npm run typecheck && npm run build

# Reproducible release binaries plus checksums. The image is built separately by
# docker-build so packaging works without a container runtime.
package:
	rm -rf $(DIST_DIR)
	mkdir -p $(DIST_DIR)
	for platform in linux/amd64 linux/arm64; do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags='-s -w' \
			-o $(DIST_DIR)/objectstoreviewer-$$os-$$arch ./cmd/objectstoreviewer || exit 1; \
	done
	cd $(DIST_DIR) && sha256sum objectstoreviewer-* > SHA256SUMS
	printf 'version=%s\n' '$(VERSION)' > $(DIST_DIR)/VERSION

docker-build:
	docker build --tag $(IMAGE) .

supply-chain: docker-build
	./hack/generate-supply-chain-artifacts.sh $(IMAGE) $(ARTIFACT_DIR)/release

# Expensive checks and retained supply-chain output for a release candidate.
# CI runs the component targets separately so pull requests do not repeat work.
release-check: check docs test-integration test-scale test-container package test-multiarch supply-chain

generate-evidence-artifacts:
	$(GO) -C api run ./cmd/evidence-artifacts

# Rebuild every published brand asset from hack/brand/lockup.png. Output is
# deterministic, so a clean tree after running this is the check that the
# committed assets still match their source.
generate-brand-assets:
	$(PYTHON) hack/generate-brand-assets.py
