export GO111MODULE=on
DOCKER_USER=ut0mt8
IMAGE_NAME=yakle
BINARY_NAME=yakle

VERSION=`git describe --tags`
BUILD=`date +%FT%T%z`

LDFLAGS=-ldflags "-X main.version=${VERSION} -X main.build=${BUILD}"

all: deps fmt test build
build:
	go build ${LDFLAGS} -o $(BINARY_NAME) -v
test:
	go test -v ./...
clean:
	go clean
	rm -f $(BINARY_NAME)
deps:
	go get ./...
fmt:
	go fmt ./...
docker:
	docker build --build-arg VERSION=${VERSION} --build-arg BUILD=${BUILD} -t $(IMAGE_NAME):latest .
	docker push $(DOCKER_USER)/$(IMAGE_NAME):latest
