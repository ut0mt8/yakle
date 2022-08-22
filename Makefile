DOCKER_USER=ut0mt8
IMAGE_NAME=yakle
BINARY_NAME=yakle

#VERSION=`git describe --tags`
VERSION=v0.6.0
BUILD=`date +%FT%T%z`

LDFLAGS=-ldflags "-X main.version=${VERSION} -X main.build=${BUILD}"

all: deps fmt vet build
build:
	go build ${LDFLAGS} -o $(BINARY_NAME) -v
vet:
	go vet
clean:
	go clean
	rm -f $(BINARY_NAME)
deps:
	go mod download
fmt:
	go fmt ./...
docker:
	docker build --build-arg VERSION=${VERSION} --build-arg BUILD=${BUILD} -t $(DOCKER_USER)/$(IMAGE_NAME):$(VERSION) .
	docker push $(DOCKER_USER)/$(IMAGE_NAME):$(VERSION)
