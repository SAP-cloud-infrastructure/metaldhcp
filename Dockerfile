FROM --platform=$BUILDPLATFORM golang:1.26.3 AS builder

ARG GOARCH=''

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY plugins/ plugins/
COPY internal/ internal/
COPY api/ api/

ARG TARGETOS
ARG TARGETARCH

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GO111MODULE=on go build -a -o metaldhcp .

FROM debian:bookworm-slim AS installer

RUN apt-get update \
  && apt-get -y install --no-install-recommends libcap2-bin \
  && apt-get clean \
  && rm -rf /var/lib/apt/lists/*

COPY --from=builder /workspace/metaldhcp /metaldhcp
RUN /sbin/setcap 'cap_net_bind_service,cap_net_raw=+ep' /metaldhcp

FROM gcr.io/distroless/base-debian12 AS output-image

WORKDIR /

COPY --from=installer /metaldhcp /metaldhcp

USER 65532:65532

ENTRYPOINT ["/metaldhcp"]
