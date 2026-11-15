# The ethernetip-interop-otfabric image, from the repository root:
#   make -f own/go-ethernetip/Makefile image smoke interop
# and the Go module's own checks (CI runs them on every pull request):
#   make -f own/go-ethernetip/Makefile test vulncheck
#
# Apart from the top-level Makefile on purpose: this image is not in ADAPTERS
# and not in a release of the harness.

IMAGE ?= ghcr.io/otfabric/ethernetip-interop-otfabric:dev
# The harness release of the reference images to test against.
VERSION ?= $(shell git tag -l 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | head -1)
REFERENCES ?= opener eipscanner cpppo pycomm3 libplctag gologix pylogix etherip eiprs nodeeip eeip lokieip cbsigma
BASE_REGISTRY ?= docker.io/library

# The latest go-ethernetip release, as the workflow builds it.
UPSTREAM_VERSION ?= $(shell git ls-remote --tags --refs https://github.com/otfabric/go-ethernetip.git 'v[0-9]*' | sed 's,.*refs/tags/,,' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$$' | sort -V | tail -1)
UPSTREAM_SHA ?= $(shell git ls-remote --tags https://github.com/otfabric/go-ethernetip.git 'refs/tags/$(UPSTREAM_VERSION)^{}' | cut -f1)

export VERSION
export ADAPTER_IMAGE_otfabric = $(IMAGE)

# gofmt, vet, race tests and govulncheck of this module, with go.mod as
# committed, in the golang image of its Dockerfile, as the top-level Makefile's
# test-go and vulncheck do for the Go adapters. scripts/check-versions.sh
# checks the digest and the govulncheck version against versions.yaml.
GO_IMAGE ?= $(BASE_REGISTRY)/golang:1.27.4-trixie@sha256:b54b2639de8c1bfe538ec5ffc45f29b1932235ac5f6d63c059853646677112d9
GOVULNCHECK_VERSION ?= v1.9.0
go_run = scripts/retry.sh docker run --rm -v "$(CURDIR):/src" -v ethernetip-interop-gomod:/go/pkg/mod -w /src/own/go-ethernetip \
	-e GOFLAGS=-mod=readonly -e GOTOOLCHAIN=local $(GO_IMAGE) sh -c '$(1)'

.PHONY: image smoke interop test vulncheck

test:
	$(call go_run,test -z "$$(gofmt -l .)" && go vet ./... && go test -race -count=1 ./...)

vulncheck:
	$(call go_run,go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...)

image:
	docker build --build-arg BASE_REGISTRY=$(BASE_REGISTRY) --build-arg UPSTREAM_VERSION=$(UPSTREAM_VERSION) --build-arg UPSTREAM_SHA=$(UPSTREAM_SHA) --build-arg ADAPTER_VERSION=$(UPSTREAM_VERSION) --tag $(IMAGE) --file own/go-ethernetip/Dockerfile .

smoke:
	scripts/smoke.sh otfabric

interop:
	scripts/interop.sh otfabric $(REFERENCES)
