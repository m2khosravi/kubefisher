# Build the manager binary.
# Build context must be the repository root (parent of operator/), not operator/ alone:
#   docker build -f operator/Dockerfile -t IMG ..
#   make -C operator docker-build
FROM golang:1.26-bookworm AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Root module (replace target: github.com/m2khosravi/kubefisher => ../)
COPY go.mod go.sum ./
COPY pkg/ pkg/

# Operator module manifests
COPY operator/go.mod operator/go.sum ./operator/
WORKDIR /workspace/operator
RUN go mod download

COPY operator/ .

# GOARCH defaults to the build platform when unset (docker buildx / local).
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o manager cmd/main.go

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/operator/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
