# build image
FROM golang:1.14-alpine as builder

WORKDIR /app
COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -a -installsuffix cgo -o /go/bin/yakle

# executable image
FROM alpine:3.11
COPY --from=builder /go/bin/yakle /go/bin/yakle

ENTRYPOINT ["/go/bin/yakle"]
