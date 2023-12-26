REGISTRY := ut0mt8
PROJECT := yakle
VERSION := v0.7.0
BUILD := $(shell date +%FT%T%z)

LDFLAGS=-ldflags "-X main.version=${VERSION} -X main.build=${BUILD}"

all: deps fmt vet build
staticbuild:
	CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' ${LDFLAGS} -o ./ ./...
build:
	go build ${LDFLAGS} -o ./ ./...
vet:
	go vet ./...
clean:
	go clean
deps:
	go mod download
fmt:
	go fmt ./...
docker:
	docker build --build-arg VERSION=${VERSION} --build-arg BUILD=${BUILD} -t $(REGISTRY)/$(PROJECT):$(VERSION) .
	docker push $(REGISTRY)/$(PROJECT):$(VERSION)
