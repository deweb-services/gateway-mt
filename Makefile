GO_VERSION ?= 1.17.12
COMPONENTLIST := gateway-mt authservice linksharing
BRANCH_NAME ?= $(shell git rev-parse --abbrev-ref HEAD | sed "s!/!-!g")
LATEST_DEV_TAG := dev

ifeq (${BRANCH_NAME},main)
	TAG := $(shell git rev-parse --short HEAD)-go${GO_VERSION}
	BRANCH_NAME :=
else
	TAG := $(shell git rev-parse --short HEAD)-${BRANCH_NAME}-go${GO_VERSION}
	ifneq (,$(shell git describe --tags --exact-match --match "v[0-9]*\.[0-9]*\.[0-9]*"))
		LATEST_STABLE_TAG := latest
	endif
endif

DOCKER_BUILD := docker build --build-arg TAG=${TAG}

.DEFAULT_GOAL := help
.PHONY: help
help:
	@awk 'BEGIN { \
		FS = ":.*##"; \
		printf "\nUsage:\n  make \033[36m<target>\033[0m\n"\
	} \
	/^[a-zA-Z_-]+:.*?##/ { \
		printf "  \033[36m%-17s\033[0m %s\n", $$1, $$2 \
	} \
	/^##@/ { \
		printf "\n\033[1m%s\033[0m\n", substr($$0, 5) \
	} ' $(MAKEFILE_LIST)

##@ Dependencies

.PHONY: build-dev-deps
build-dev-deps: ## Install dependencies for builds
	go get golang.org/x/tools/cover
	go get github.com/josephspurrier/goversioninfo/cmd/goversioninfo

.PHONY: lint
lint: ## Analyze and find programs in source code
	@echo "Running ${@}"
	@golangci-lint run

.PHONY: goimports-fix
goimports-fix: ## Applies goimports to every go file (excluding vendored files)
	goimports -w -local storj.io $$(find . -type f -name '*.go' -not -path "*/vendor/*")

.PHONY: goimports-st
goimports-st: ## Applies goimports to every go file in `git status` (ignores untracked files)
	@git status --porcelain -uno|grep .go|grep -v "^D"|sed -E 's,\w+\s+(.+->\s+)?,,g'|xargs -I {} goimports -w -local storj.io {}

.PHONY: build-packages
build-packages: build-packages-normal build-packages-race ## Test docker images locally
build-packages-normal:
	go build -v ./...
build-packages-race:
	go build -v -race ./...

##@ Test

.PHONY: test
test: ## Run tests on source code (jenkins)
	go test -race -v -cover -coverprofile=.coverprofile ./...
	@echo done

##@ Build

.PHONY: images
images: gateway-mt-image authservice-image linksharing-image
	echo Built version: ${TAG}

.PHONY: gateway-mt-image
gateway-mt-image: ## Build gateway-mt Docker image
	${DOCKER_BUILD} --pull=true -t storjlabs/gateway-mt:${TAG}-amd64 \
		-f cmd/gateway-mt/Dockerfile .
	${DOCKER_BUILD} --pull=true -t storjlabs/gateway-mt:${TAG}-arm32v6 \
		--build-arg=GOARCH=arm --build-arg=DOCKER_ARCH=arm32v6 \
		-f cmd/gateway-mt/Dockerfile .
	${DOCKER_BUILD} --pull=true -t storjlabs/gateway-mt:${TAG}-arm64v8 \
		--build-arg=GOARCH=arm64 --build-arg=DOCKER_ARCH=arm64v8 \
		-f cmd/gateway-mt/Dockerfile .
	docker tag storjlabs/gateway-mt:${TAG}-amd64 storjlabs/gateway-mt:${LATEST_DEV_TAG}

.PHONY: authservice-image
authservice-image: ## Build authservice Docker image
	${DOCKER_BUILD} --pull=true -t storjlabs/authservice:${TAG}-amd64 \
		-f cmd/authservice/Dockerfile .
	${DOCKER_BUILD} --pull=true -t storjlabs/authservice:${TAG}-arm32v6 \
		--build-arg=GOARCH=arm --build-arg=DOCKER_ARCH=arm32v6 \
		-f cmd/authservice/Dockerfile .
	${DOCKER_BUILD} --pull=true -t storjlabs/authservice:${TAG}-arm64v8 \
		--build-arg=GOARCH=arm64 --build-arg=DOCKER_ARCH=arm64v8 \
		-f cmd/authservice/Dockerfile .
	docker tag storjlabs/authservice:${TAG}-amd64 storjlabs/authservice:${LATEST_DEV_TAG}

.PHONY: linksharing-image
linksharing-image: ## Build linksharing Docker image
	${DOCKER_BUILD} --pull=true -t storjlabs/linksharing:${TAG}-amd64 \
		-f cmd/linksharing/Dockerfile .
	${DOCKER_BUILD} --pull=true -t storjlabs/linksharing:${TAG}-arm32v6 \
		--build-arg=GOARCH=arm --build-arg=DOCKER_ARCH=arm32v6 \
		-f cmd/linksharing/Dockerfile .
	${DOCKER_BUILD} --pull=true -t storjlabs/linksharing:${TAG}-arm64v8 \
		--build-arg=GOARCH=arm64 --build-arg=DOCKER_ARCH=arm64v8 \
		-f cmd/linksharing/Dockerfile .
	docker tag storjlabs/linksharing:${TAG}-amd64 storjlabs/linksharing:${LATEST_DEV_TAG}

.PHONY: binary
binary:
	@if [ -z "${COMPONENT}" ]; then echo "Try one of the following targets instead:" \
		&& for b in binaries ${BINARIES}; do echo "- $$b"; done && exit 1; fi
	# freebsd/amd64 target is currently skipped: https://github.com/storj/gateway-st/issues/62
	CGO_ENABLED=0 storj-release \
		--components "cmd/${COMPONENT}" \
		--build-tags kqueue \
		--go-version "${GO_VERSION}" \
		--branch "${BRANCH_NAME}" \
		--skip-osarches "freebsd/amd64"

.PHONY: binaries
binaries: ${BINARIES} ## Build gateway-mt, authservice, and linksharing binaries (jenkins)
	for C in ${COMPONENTLIST}; do\
		$(MAKE) binary COMPONENT=$$C || exit $$? \
	; done

##@ Deploy

.PHONY: push-images
push-images: ## Push Docker images to Docker Hub (jenkins)
	# images have to be pushed before a manifest can be created
	for c in ${COMPONENTLIST}; do \
		docker push storjlabs/$$c:${TAG}-amd64 \
		&& docker push storjlabs/$$c:${TAG}-arm32v6 \
		&& docker push storjlabs/$$c:${TAG}-arm64v8 \
		&& for t in ${TAG} ${LATEST_DEV_TAG} ${LATEST_STABLE_TAG}; do \
			docker manifest create storjlabs/$$c:$$t \
			storjlabs/$$c:${TAG}-amd64 \
			storjlabs/$$c:${TAG}-arm32v6 \
			storjlabs/$$c:${TAG}-arm64v8 \
			&& docker manifest annotate storjlabs/$$c:$$t storjlabs/$$c:${TAG}-amd64 --os linux --arch amd64 \
			&& docker manifest annotate storjlabs/$$c:$$t storjlabs/$$c:${TAG}-arm32v6 --os linux --arch arm --variant v6 \
			&& docker manifest annotate storjlabs/$$c:$$t storjlabs/$$c:${TAG}-arm64v8 --os linux --arch arm64 \
			&& docker manifest push --purge storjlabs/$$c:$$t \
		; done \
	; done

.PHONY: binaries-upload
binaries-upload: ## Upload binaries to Google Storage (jenkins)
	cd "release/${TAG}"; for f in *; do \
		c="$${f%%_*}" \
		&& if [ "$${f##*.}" != "$${f}" ]; then \
			ln -s "$${f}" "$${f%%_*}.$${f##*.}" \
			&& zip "$${f}.zip" "$${f%%_*}.$${f##*.}" \
			&& rm "$${f%%_*}.$${f##*.}" \
		; else \
			ln -sf "$${f}" "$${f%%_*}" \
			&& zip "$${f}.zip" "$${f%%_*}" \
			&& rm "$${f%%_*}" \
		; fi \
	; done
	cd "release/${TAG}"; gsutil -m cp -r *.zip "gs://storj-v3-alpha-builds/${TAG}/"

##@ Clean

.PHONY: clean
clean: binaries-clean clean-images ## Clean local release binaries and local Docker images

.PHONY: binaries-clean
binaries-clean: ## Remove all local release binaries (jenkins)
	rm -rf release

.PHONY: clean-images
clean-images:
	-docker rmi -f $(shell docker images -q "storjlabs/gateway-mt:${TAG}-*")
	-docker rmi -f $(shell docker images -q "storjlabs/authservice:${TAG}-*")
	-docker rmi -f $(shell docker images -q "storjlabs/linksharing:${TAG}-*")

.PHONY: bump-dependencies
bump-dependencies:
	go get storj.io/common@main storj.io/private@main storj.io/uplink@main github.com/storj/minio
	go mod tidy
	cd testsuite;\
		go get storj.io/common@main storj.io/storj@main storj.io/uplink@main;\
		go mod tidy


UNAME_S := $(shell uname -s)

.PHONY: badgerauth-install-dependencies
badgerauth-install-dependencies:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install storj.io/drpc/cmd/protoc-gen-go-drpc@latest

ifneq ($(shell which apt-get),)
	sudo apt-get install -y protobuf-compiler
endif

ifneq ($(shell which brew),)
	brew install protobuf
endif

.PHONY: badgerauth-format-protobufs
badgerauth-format-protobufs:
# If clang-format isn't found, we want to install it first.
ifeq ($(shell which clang-format),)
	ifneq ($(shell which apt-get),)
		sudo apt-get install -y clang-format
	endif
	ifneq ($(shell which brew),)
		brew install clang-format
	endif
endif

	clang-format -i pkg/auth/badgerauth/pb/badgerauth.proto
	clang-format -i pkg/auth/badgerauth/pb/badgerauth_admin.proto

##@ Local development/Public Jenkins/Integration Test

BUILD_NUMBER ?= ${TAG}

.PHONY: integration-run
integration-run: integration-env-start integration-all-tests ## Start the integration environment and run all tests

.PHONY: integration-env-start
integration-env-start: integration-checkout integration-image-build integration-network-create integration-services-start ## Start the integration environment

.PHONY: integration-env-stop
integration-env-stop: ## Stop all running services in the integration environment
	-docker stop --time=1 $$(docker ps -qf network=integration-network-${BUILD_NUMBER})

.PHONY: integration-env-clean
integration-env-clean:
	-docker rm $$(docker ps -aqf network=integration-network-${BUILD_NUMBER})
	-docker rmi $$(docker image ls -qf label=build=${BUILD_NUMBER})
	-docker rmi redis:latest
	-docker rmi postgres:latest
	-docker rmi storjlabs/gateway-mint:latest
	-docker rmi storjlabs/splunk-s3-tests:latest
	-rm -r volumes
	-rm -rf gateway-st

.PHONY: integration-env-purge
integration-env-purge: integration-env-stop integration-env-clean integration-network-remove ## Purge the integration environment

.PHONY: integration-env-logs
integration-env-logs: ## Retrieve logs from integration services
	-docker logs integration-sim-${BUILD_NUMBER}
	-docker logs integration-authservice-${BUILD_NUMBER}
	-docker logs integration-gateway-${BUILD_NUMBER}

.PHONY: integration-all-tests
integration-all-tests: integration-gateway-st-tests integration-mint-tests integration-splunk-tests ## Run all integration tests (environment needs to be started first)

# note: umask 0000 is needed for rclone tests so files can be cleaned up.
.PHONY: integration-gateway-st-tests
integration-gateway-st-tests: ## Run gateway-st test suite (environment needs to be started first)
	export $$(docker exec integration-authservice-${BUILD_NUMBER} ./authservice register --address drpc://authservice:20002 --format-env $$(docker exec integration-sim-${BUILD_NUMBER} storj-sim network env GATEWAY_0_ACCESS)) && \
	docker run \
	--network integration-network-${BUILD_NUMBER} \
	-e AWS_ENDPOINT=https://gateway:20011 -e "AWS_ACCESS_KEY_ID=$$AWS_ACCESS_KEY_ID" -e "AWS_SECRET_ACCESS_KEY=$$AWS_SECRET_ACCESS_KEY" \
	-v $$PWD:/build \
	-w /build \
	--name integration-gateway-st-tests-${BUILD_NUMBER}-$$TEST \
	--entrypoint /bin/bash \
	--rm storjlabs/ci:latest \
	-c "umask 0000; scripts/run-integration-tests.sh $$TEST" \

.PHONY: integration-mint-tests
integration-mint-tests: ## Run mint test suite (environment needs to be started first)
	export $$(docker exec integration-authservice-${BUILD_NUMBER} ./authservice register --address drpc://authservice:20002 --format-env $$(docker exec integration-sim-${BUILD_NUMBER} storj-sim network env GATEWAY_0_ACCESS)) && \
	docker run \
	--network integration-network-${BUILD_NUMBER} \
	-e SERVER_ENDPOINT=gateway:20010 -e "ACCESS_KEY=$$AWS_ACCESS_KEY_ID" -e "SECRET_KEY=$$AWS_SECRET_ACCESS_KEY" -e ENABLE_HTTPS=0 \
	--name integration-mint-tests-${BUILD_NUMBER}-$$TEST \
	--rm storjlabs/gateway-mint:latest $$TEST

.PHONY: integration-splunk-tests
integration-splunk-tests: ## Run splunk test suite (environment needs to be started first)
	export $$(docker exec integration-authservice-${BUILD_NUMBER} ./authservice register --address drpc://authservice:20002 --format-env $$(docker exec integration-sim-${BUILD_NUMBER} storj-sim network env GATEWAY_0_ACCESS)) && \
	docker run \
	--network integration-network-${BUILD_NUMBER} \
	-e ENDPOINT=gateway:20010 -e "AWS_ACCESS_KEY_ID=$$AWS_ACCESS_KEY_ID" -e "AWS_SECRET_ACCESS_KEY=$$AWS_SECRET_ACCESS_KEY" -e SECURE=0 \
	--name integration-splunk-tests-${BUILD_NUMBER} \
	--rm storjlabs/splunk-s3-tests:latest

.PHONY: integration-checkout
integration-checkout:
	git clone --filter blob:none --depth 1 --no-tags --no-checkout https://github.com/storj/gateway-st gateway-st
	cd gateway-st && \
		git config core.sparsecheckout true && \
		echo "testsuite/integration" >> .git/info/sparse-checkout && \
		git checkout

.PHONY: integration-image-build
integration-image-build:
	for C in gateway-mt authservice; do \
		./scripts/build-image.sh $$C ${BUILD_NUMBER} ${GO_VERSION} \
	; done

.PHONY: integration-network-create
integration-network-create:
	docker network create integration-network-${BUILD_NUMBER}

.PHONY: integration-network-remove
integration-network-remove:
	-docker network remove integration-network-${BUILD_NUMBER}

.PHONY: integration-services-start
integration-services-start:
	docker run \
	--network integration-network-${BUILD_NUMBER} --network-alias postgres \
	-e POSTGRES_DB=sim -e POSTGRES_HOST_AUTH_METHOD=trust \
	--name integration-postgres-${BUILD_NUMBER} \
	--rm -d postgres:latest

	docker run \
	--network integration-network-${BUILD_NUMBER} --network-alias redis \
	--name integration-redis-${BUILD_NUMBER} \
	--rm -d redis:latest

	docker run \
	--network integration-network-${BUILD_NUMBER} --network-alias sim \
	-e STORJ_SIM_POSTGRES='postgres://postgres@postgres/sim?sslmode=disable' -e STORJ_SIM_REDIS=redis:6379 \
	-v $$PWD/scripts:/scripts:ro \
	--name integration-sim-${BUILD_NUMBER} \
	--rm -d storjlabs/golang:${GO_VERSION} /scripts/start_storj-sim.sh

	until docker exec integration-sim-${BUILD_NUMBER} storj-sim network env SATELLITE_0_URL > /dev/null; do \
		echo "*** storj-sim is not yet available; waiting for 3s..." && sleep 3; \
	done

	docker run \
	--network integration-network-${BUILD_NUMBER} --network-alias authservice \
	--name integration-authservice-${BUILD_NUMBER} \
	--rm -d storjlabs/authservice:${BUILD_NUMBER} run \
		--listen-addr 0.0.0.0:20000 \
		--drpc-listen-addr 0.0.0.0:20002 \
		--allowed-satellites $$(docker exec integration-sim-${BUILD_NUMBER} storj-sim network env SATELLITE_0_URL) \
		--auth-token super-secret \
		--endpoint http://gateway:20010 \
		--kv-backend memory://

	mkdir -p volumes/gateway
	openssl req \
		-x509 \
		-newkey rsa:4096 \
		-keyout volumes/gateway/cert.key \
		-out volumes/gateway/cert.crt \
		-nodes \
		-subj '/CN=gateway' \
		-addext "subjectAltName = DNS:gateway"

	docker run \
	--network integration-network-${BUILD_NUMBER} --network-alias gateway \
	--name integration-gateway-${BUILD_NUMBER} \
	--volume $$PWD/volumes/gateway:/cert:ro \
	--rm -d storjlabs/gateway-mt:${BUILD_NUMBER} run \
		--server.address 0.0.0.0:20010 \
		--server.address-tls 0.0.0.0:20011 \
		--auth.base-url http://authservice:20000 \
		--auth.token super-secret \
		--domain-name gateway \
		--insecure-log-all \
		--cert-dir /cert \
		--insecure-disable-tls=false \
		--s3compatibility.fully-compatible-listing \
		--s3compatibility.disable-copy-object=false

	until [ ! -z $$(docker exec integration-sim-${BUILD_NUMBER} storj-sim network env GATEWAY_0_ACCESS) ]; do \
		echo "*** main access grant is not yet available; waiting for 3s..." && sleep 3; \
	done
