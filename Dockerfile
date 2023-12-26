# build image
FROM golang:1.21-alpine as builder

ARG VERSION
ARG BUILD

WORKDIR /app
COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -ldflags "-X main.version=$VERSION -X main.build=$BUILD" -a -installsuffix cgo -o /go/bin/yakle

# executable image
FROM alpine:3.16
COPY --from=builder /go/bin/yakle /go/bin/yakle

ENTRYPOINT ["/go/bin/yakle"]
