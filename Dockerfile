# hadolint ignore=FromPlatformFlagConstDisallowed
FROM --platform=linux/amd64 alpine:3.22 AS ipxe-builder

RUN apk add --no-cache --no-progress \
    gcc g++ make perl xz-dev mtools libc-dev linux-headers binutils bash git curl openssl openssl-dev coreutils

WORKDIR /build
RUN git clone --depth 1 https://github.com/ipxe/ipxe.git

WORKDIR /build/ipxe/src
RUN mkdir -p config/local && \
    echo '#define DOWNLOAD_PROTO_HTTPS' > config/local/general.h && \
    printf '#undef OCSP_CHECK\n#undef CROSSCERT\n#define CROSSCERT ""\n' > config/local/crypto.h

RUN curl -fsSL -o digicert-root-g2.pem https://cacerts.digicert.com/DigiCertGlobalRootG2.crt.pem && \
    curl -fsSL -o digicert-root-ca.pem https://cacerts.digicert.com/DigiCertGlobalRootCA.crt.pem

RUN make -j$(nproc) bin-x86_64-efi/snponly.efi \
    CERT=digicert-root-g2.pem,digicert-root-ca.pem \
    TRUST=digicert-root-g2.pem,digicert-root-ca.pem

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
COPY cmd/ cmd/
COPY plugins/ plugins/
COPY internal/ internal/
COPY api/ api/

ARG TARGETOS
ARG TARGETARCH

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GO111MODULE=on go build -a -o metaldhcp . && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GO111MODULE=on go build -a -o tftpd ./cmd/tftpd/

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
COPY --from=builder /workspace/tftpd /tftpd
COPY --from=ipxe-builder /build/ipxe/src/bin-x86_64-efi/snponly.efi /ipxe/snponly.efi

USER 65532:65532

ENTRYPOINT ["/metaldhcp"]
