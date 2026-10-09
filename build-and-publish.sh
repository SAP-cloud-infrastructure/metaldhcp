#!/usr/bin/env bash
set -euo pipefail

REGISTRY="keppel.eu-de-2.cloud.sap/i-cant-believe-its-not-cloud-infrastructure-dev/metaldhcp"

sha=$(git rev-parse --short HEAD)
tag="sha-${sha}"
tarball="metaldhcp-${sha}.tgz"
image="${REGISTRY}:${tag}"
source_repo=$(git remote get-url origin)

echo
echo "  commit     : ${sha}"
echo "  image      : ${image}"
echo "  tarball    : ${tarball}"
echo "  source_repo: ${source_repo}"
echo
read -rp "Build and publish? [y/N] " confirm
[[ "${confirm}" =~ ^[Yy]$ ]] || { echo "Aborted."; exit 1; }

echo
echo "==> Building (linux/amd64)…"
docker buildx build --platform linux/amd64 \
  --label "source_repository=${source_repo}" \
  --output "type=docker,dest=${tarball}" .

echo
echo "==> Pushing to Keppel…"
crane push "${tarball}" "${image}"

echo
echo "==> Done: ${image}"
