export GO111MODULE=on
BINARY_NAME=yakle

all: deps fmt test build
build: 
	go build -o $(BINARY_NAME) -v
test: 
	go test -v ./...
clean: 
	go clean
	rm -f $(BINARY_NAME)
deps:
	go get ./...
fmt:
	go fmt ./...

