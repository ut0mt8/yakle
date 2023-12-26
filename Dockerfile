FROM golang:1.21 AS builder

WORKDIR /app
COPY . .

RUN make staticbuild

FROM alpine
COPY --from=builder /app/yakle /app/yakle

ENTRYPOINT ["/app/yakle"]
