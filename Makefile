PWD := $(shell pwd)
GOPATH := $(shell go env GOPATH)
GOLANGCI_LINT ?= $(shell command -v golangci-lint 2>/dev/null || printf '%s/bin/golangci-lint' '$(GOPATH)')
LDFLAGS := $(shell go run buildscripts/gen-ldflags.go)

GOARCH := $(shell go env GOARCH)
GOOS := $(shell go env GOOS)

VERSION ?= $(shell git describe --tags)
TAG ?= "ghcr.io/obstor/obstor:$(VERSION)"

all: build

checks:
	@echo "Checking dependencies"
	@(env bash $(PWD)/buildscripts/checkdeps.sh)

getdeps:
	@mkdir -p ${GOPATH}/bin
	@test -x "$(GOLANGCI_LINT)" || (echo "Installing golangci-lint" && curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOPATH)/bin v2.15.0)
	@which msgp 1>/dev/null || (echo "Installing msgp" && go install github.com/tinylib/msgp@v1.6.5)
	@which stringer 1>/dev/null || (echo "Installing stringer" && go install golang.org/x/tools/cmd/stringer@latest)

crosscompile:
	@(env bash $(PWD)/buildscripts/cross-compile.sh)

verifiers: getdeps lint check-gen

check-gen:
	@go generate ./... >/dev/null
	@(! git diff --name-only | grep '_gen.go$$') || (echo "Non-committed changes in auto-generated code is detected, please commit them to proceed." && false)

lint:
	@echo "Running $@ check"
	@GO111MODULE=on "$(GOLANGCI_LINT)" cache clean
	@GO111MODULE=on "$(GOLANGCI_LINT)" run --build-tags kqueue --timeout=10m --config ./.golangci.yml

# Builds obstor, runs the verifiers then runs the tests.
check: test
test: verifiers build
	@echo "Running unit tests"
	@GOGC=25 GO111MODULE=on CGO_ENABLED=0 go test -tags kqueue ./... 1>/dev/null

test-race: verifiers build
	@echo "Running unit tests under -race"
	@(env bash $(PWD)/buildscripts/race.sh)

# Verify obstor binary
verify:
	@echo "Verifying build with race"
	@GO111MODULE=on CGO_ENABLED=1 go build -tags kqueue -trimpath --ldflags "$(LDFLAGS)" -o $(PWD)/obstor 1>/dev/null
	@(env bash $(PWD)/buildscripts/verify-build.sh)

# Verify healing of disks with obstor binary
verify-healing:
	@echo "Verify healing build with race"
	@GO111MODULE=on CGO_ENABLED=1 go build -race -tags kqueue -trimpath --ldflags "$(LDFLAGS)" -o $(PWD)/obstor 1>/dev/null
	@(env bash $(PWD)/buildscripts/verify-healing.sh)

# Builds obstor locally.
build: checks
	@echo "Building obstor binary to './obstor'"
	@GO111MODULE=on CGO_ENABLED=0 go build -tags kqueue -trimpath --ldflags "$(LDFLAGS)" -o $(PWD)/obstor 1>/dev/null

hotfix-vars:
	$(eval LDFLAGS := $(shell OBSTOR_RELEASE="RELEASE" OBSTOR_HOTFIX="hotfix.$(shell git rev-parse --short HEAD)" go run buildscripts/gen-ldflags.go $(shell git describe --tags --abbrev=0 | \
    sed 's#RELEASE\.\([0-9]\+\)-\([0-9]\+\)-\([0-9]\+\)T\([0-9]\+\)-\([0-9]\+\)-\([0-9]\+\)Z#\1-\2-\3T\4:\5:\6Z#')))
	$(eval TAG := "ghcr.io/obstor/obstor:$(shell git describe --tags --abbrev=0).hotfix.$(shell git rev-parse --short HEAD)")
hotfix: hotfix-vars install

docker-hotfix: hotfix checks
	@echo "Building obstor docker image '$(TAG)'"
	@docker build -t $(TAG) . -f Dockerfile.dev

docker: build checks
	@echo "Building obstor docker image '$(TAG)'"
	@docker build -t $(TAG) . -f Dockerfile.dev

# Builds obstor and installs it to $GOPATH/bin.
install: build
	@echo "Installing obstor binary to '$(GOPATH)/bin/obstor'"
	@mkdir -p $(GOPATH)/bin && cp -f $(PWD)/obstor $(GOPATH)/bin/obstor
	@echo "Installation successful. To learn more, try \"obstor --help\"."

clean:
	@echo "Cleaning up all the generated files"
	@find . -name '*.test' | xargs rm -fv
	@find . -name '*~' | xargs rm -fv
	@rm -rvf obstor
	@rm -rvf build
	@rm -rvf release
	@rm -rvf .verify*
