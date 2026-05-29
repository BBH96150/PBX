# syntax=docker/dockerfile:1
FROM golang:1.25-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum* ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/control-plane ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S -g 65532 control && \
    adduser  -S -u 65532 -G control control
WORKDIR /app
COPY --from=build /out/control-plane /usr/local/bin/control-plane
# Drop privileges. The control-plane binds 8080 + 8443 which are non-
# privileged ports, so no capabilities are needed.
USER 65532:65532
EXPOSE 8080 8443
ENTRYPOINT ["/usr/local/bin/control-plane"]
