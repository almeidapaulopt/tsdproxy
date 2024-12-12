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


## run: run the  application
.PHONY: run
run: build/static build 
	./tmp/${BINARY_NAME}


## start: start dev server
.PHONE: start 
start: dev

## dev: start dev server
.PHONY: dev
dev: docker_start server_start


## server_start: start the server
.PHONY: server_start
server_start:
	# TSDPROXY_DataDir=./dev/data TSDPROXY_LOG_LEVEL=debug  \
	# 	TSDPROXY_AUTHKEYFILE=./dev/KEY_FILE \
	# 	TSDPROXY_DASHBOARD_ENABLED=true \
	# 	TSDPROXY_DASHBOARD_NAME=DASH1 \
	# 	DOCKER_HOST=unix:///run/user/1000/docker.sock \
	# 	wgo run -file=.go -file=.yaml -file=.env -file=.json -file=.toml ${MAIN_PACKAGE_PATH} -config nonefile.yaml
		wgo run -file=.go -file=.yaml -file=.env -file=.json -file=.toml ${MAIN_PACKAGE_PATH} -config ./dev/tsdproxy.yaml



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
stop: dev_kill docker_stop


## docker_image: Create docker image
.PHONY: docker_image
docker_image:
	docker buildx build  -t "tsdproxy:latest" .


## docs local server
.PHONY: docs
docs:
	cd docs && hugo server --disableFastRender
##	cd docs && hugo server --buildDrafts --disableFastRender


.PHONY: run_in_docker
run_in_docker:
	wgo -file=.go -file=.templ -xfile=_templ.go templ generate :: go run ${MAIN_PACKAGE_PATH}

	# templ generate --proxy="http://localhost:8080" --watch --cmd="echo reload" &
	# wgo -file=.go -file=.templ -file=.yaml -xfile=_templ.go templ generate --notify-proxy :: go run ${MAIN_PACKAGE_PATH}

# ==================================================================================== #
# QUALITY CONTROL
# ==================================================================================== #

## tidy: format code and tidy modfile
.PHONY: tidy
tidy:
	go get -u ./...
	go fmt ./...
	go mod tidy -v -e -x

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
push: gen tidy audit no-dirty
	git push
	git push --tags

## info: print version info
.PHONY: info
info:
	 @echo "Version:           ${VERSION}"
	 @echo "Git Tag:           ${GIT_TAG}"
	 @echo "Git Commit:        ${GIT_COMMIT}"
	 @echo "Git Tree State:    ${GIT_TREE_STATE}"
