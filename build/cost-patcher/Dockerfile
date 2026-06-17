# syntax=docker/dockerfile:1

FROM docker.io/library/golang:1.26-bookworm AS build
WORKDIR /workspace

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /cost-patcher ./cmd/cost-patcher

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /cost-patcher /cost-patcher
# Kubernetes runAsNonRoot verification prefers numeric IDs.
USER 65532:65532
ENTRYPOINT ["/cost-patcher"]
