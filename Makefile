# Makefile for DXClusterGoAPI
APP_NAME := dxcluster-go-api
IMAGE_NAME := user00265/dxclustergoapi
GHCR := ghcr.io/user00265/dxclustergoapi

.PHONY: all build test docker-build docker-push

all: build

build:
	go build -o $(APP_NAME) ./cmd/dxcluster-client

test:
	go test ./... -v

docker-build:
	docker build -t $(IMAGE_NAME):latest -t $(GHCR):latest .

docker-push: docker-build
	# Push to Docker Hub and GHCR (login required)
	docker push $(IMAGE_NAME):latest
	docker push $(GHCR):latest
