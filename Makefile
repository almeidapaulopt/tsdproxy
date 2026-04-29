default: dev

# Change these variables as necessary.
MAIN_PACKAGE_PATH := "cmd/server/main.go"
BINARY_NAME := tsdproxy
PACKAGE := github.com/almeidapaulopt/tsdproxy



BUILD_DATE=$(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
GIT_COMMIT=$(shell git rev-parse HEAD)
GIT_TAG=$(shell if [ -z "`git status --porcelain`" ]; then git describe --exact-match --tags HEAD 2>/dev/null; fi)
GIT_TREE_STATE=$(shell if [ -z "`git status --porcelain`" ]; then echo "clean" ; else echo "dirty"; fi)
GIT_REMOTE_REPO=upstream
VERSION=$(shell if [ ! -z "${GIT_TAG}" ] ; then echo "${GIT_TAG}" | sed -e "s/^v//"  ; else cat internal/core/version.txt ; fi)
GO_VERSION=$(shell go version | cut -d " " -f3)



# docker image publishing options
DOCKER_PUSH=false
IMAGE_TAG=latest

override LDFLAGS +=  \
  -X ${PACKAGE}/internal/core.AppVersion=${VERSION} \
  -X ${PACKAGE}/internal/core.BuildDate=${BUILD_DATE} \
  -X ${PACKAGE}/internal/core.GitCommit=${GIT_COMMIT} \
  -X ${PACKAGE}/internal/core.GitTreeState=${GIT_TREE_STATE} \
	-X ${PACKAGE}/internal/core.GoVersion=${GO_VERSION}


ifneq (${GIT_TAG},)
IMAGE_TAG=${GIT_TAG}
override LDFLAGS += -X ${PACKAGE}/internal/core.GitTag=${GIT_TAG}
endif




# ==================================================================================== #
# HELPERS
# ==================================================================================== #

## help: print this help message
.PHONY: help
help:
	@echo 'Usage:'
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' |  sed -e 's/^/ /'

.PHONY: confirm
confirm:
	@echo -n 'Are you sure? [y/N] ' && read ans && [ $${ans:-N} = y ]

.PHONY: no-dirty
no-dirty:
	git diff --exit-code


# ==================================================================================== #
# DEVELOPMENT
# ==================================================================================== #

## test: run all tests
.PHONY: test
test:
	go test -v -race -buildvcs ./...

## test/cover: run all tests and display coverage
.PHONY: test/cover
test/cover:
	go test -v -race -buildvcs -coverprofile=./tmp/coverage.out ./...
	go tool cover -html=./tmp/coverage.out

## build: build the application
.PHONY: build
build:
	@echo "GIT_TAG: ${GIT_TAG}"
	go build -ldflags '$(LDFLAGS)' -o=./tmp/${BINARY_NAME}  ${MAIN_PACKAGE_PATH}

## build/all: build frontend assets and binary
.PHONY: build/all
build/all:
	bun run --cwd web build
	templ generate
	make build

## run: run the  application
.PHONY: run
run: build/all
	./tmp/${BINARY_NAME}


## dev: start dev server
.PHONY: dev
dev: download-icons docker_start
	make -j2 assets server_start

## download-icons: download icon sets to web/public/icons/
.PHONY: download-icons
download-icons:
	bun run --cwd web download-icons

## bootstrap: full build from clean checkout (for CI and first-time setup)
.PHONY: bootstrap
bootstrap:
	bun run --cwd web build
	templ generate

## ci: clean-checkout verification — deletes all generated assets and rebuilds from scratch
.PHONY: ci
ci:
	rm -rf web/dist web/public/icons/si web/public/icons/mdi web/public/icons/sh web/scripts/.download-cache.json
	bun run --cwd web build
	templ generate
	go test -v -race ./...

## server_start: start the server
.PHONY: server_start
server_start:
	templ generate --proxy="http://localhost:5173" --watch --cmd="echo RELOAD" & 
	air

.PHONY: assets
assets:
	bun run --cwd web dev

## docker_start: start the docker containers
.PHONY: docker_start
docker_start:
	cd dev && docker compose -f docker-compose-local.yaml up -d

## dev_docker: start the dev docker containers
.PHONY: dev_docker
dev_docker:
	CURRENT_UID=$(shell id -u):$(shell id -g) docker compose -f dev/docker-compose-dev.yaml up

## dev_docker_stop: stop the dev docker containers
.PHONY: dev_docker_stop
dev_docker_stop:
	CURRENT_UID=$(shell id -u):$(shell id -g) docker compose -f dev/docker-compose-dev.yaml down


## dev_image: generate docker development image
.PHONY: dev_image
dev_image:
	docker build --build-arg UID=$(shell id -u) --build-arg GID=$(shell id -g) -f dev/Dockerfile.dev -t devimage .

## docker_stop: stop the docker containers
.PHONY: docker_stop
docker_stop:
	-cd dev && docker compose -f docker-compose-local.yaml down


## stop: stop the dev server
.PHONY: stop
stop: docker_stop


## docker_image: Create docker image
.PHONY: docker_image
docker_image:
	docker buildx build  -t "tsdproxy:latest" .


## docs local server
.PHONY: docs
docs:
	cd docs && hugo server --disableFastRender


.PHONY: run_in_docker
run_in_docker:
	templ generate --proxy="http://localhost:5173" --watch --cmd="echo RELOAD" & 
	air

## audit: run quality control checks
.PHONY: audit
audit:
	go mod verify
	golangci-lint run 
	go run honnef.co/go/tools/cmd/staticcheck@latest -checks=all,-ST1000,-U1000 ./...
	go vet ./...
	deadcode ./...
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...
	go test -race -buildvcs -vet=off ./...
	gosec -exclude-generated  ./...


# ==================================================================================== #
# OPERATIONS
# ==================================================================================== #

## push: push changes to the remote Git repository
.PHONY: push
push: tidy audit no-dirty
	git push
	git push --tags

