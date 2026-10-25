#!/bin/bash
# Build and push san8888/movoa for linux/amd64 + linux/arm64 without a Docker daemon.
# Needs go and crane; DOCKER_CONFIG must hold Docker Hub credentials.
#   ./build-image.sh [version]   build both arches and push <version> + latest
#   ./build-image.sh --local     build the host arch only and load it as san8888/movoa:local (no push)
set -euo pipefail
cd "$(dirname "$0")"
R=index.docker.io/san8888/movoa

layer() { # $1 = arch
  mkdir -p dist/$1/root/etc/ssl/certs
  CGO_ENABLED=0 GOOS=linux GOARCH=$1 go build -trimpath -ldflags "-s -w" -o dist/$1/root/movoa .
  cp /etc/ssl/cert.pem dist/$1/root/etc/ssl/certs/ca-certificates.crt
  (cd dist/$1/root && COPYFILE_DISABLE=1 tar --no-xattrs --no-mac-metadata --uid 0 --gid 0 --format ustar -cf ../layer.tar movoa etc)
}

rm -rf dist
if [ "${1:-}" = "--local" ]; then
  a=$(uname -m); [ "$a" = x86_64 ] && a=amd64; [ "$a" = aarch64 ] && a=arm64
  layer $a
  docker import --platform linux/$a \
    --change 'ENTRYPOINT ["/movoa"]' --change 'ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt' \
    dist/$a/layer.tar san8888/movoa:local
  exit 0
fi

V=${1:-0.1.0}
for a in amd64 arm64; do
  layer $a
  crane append --platform linux/$a -f dist/$a/layer.tar -t "${R}:$a"
  crane mutate --entrypoint=/movoa --set-platform linux/$a -e SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt -t "${R}:$a" "${R}:$a"
done
crane index append -m "${R}:amd64" -m "${R}:arm64" -t "${R}:$V"
crane copy "${R}:$V" "${R}:latest"
