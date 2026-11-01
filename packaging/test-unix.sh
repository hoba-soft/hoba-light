#!/usr/bin/env bash
# Run only in disposable CI runners/containers: installs into system locations.
set -euo pipefail
version="$1"
previous="$2"
current="$3"
system="$4"
arch="$5"
base="hobalight_${version}_${system}_${arch}"
old="hobalight_0.0.0_${system}_${arch}"
home="$(mktemp -d)"
trap 'rm -rf "$home"' EXIT
check_version() {
  "$1" --version | grep -F "hobalight $2-"
}
if [[ "$system" == linux ]]; then
  sudo dpkg -i "$previous/$old.deb"
  check_version /usr/bin/hobalight 0.0.0
  HOME="$home" /usr/bin/hobalight info
  cp "$home/.hobalight/hobalight.conf" "$home/config.before"
  echo 'keep user data' > "$home/.hobalight/preserve"
  sudo dpkg -i "$current/$base.deb"
  check_version /usr/bin/hl "$version"
  cmp "$home/config.before" "$home/.hobalight/hobalight.conf"
  sudo dpkg -r hoba-light
  test ! -e /usr/bin/hobalight
  test ! -e /usr/bin/hl
  cmp "$home/config.before" "$home/.hobalight/hobalight.conf"
  test -f "$home/.hobalight/preserve"
  # Native arm64 and amd64 Fedora images exercise RPM transactions too.
  docker run --rm -v "$(pwd):/packages:ro" -e RELEASE_VERSION="$version" -e TARGET_ARCH="$arch" fedora:43 bash /packages/packaging/test-rpm.sh
else
  sudo installer -pkg "$previous/$old.pkg" -target /
  check_version /usr/local/bin/hobalight 0.0.0
  HOME="$home" /usr/local/bin/hobalight info
  cp "$home/.hobalight/hobalight.conf" "$home/config.before"
  echo 'keep user data' > "$home/.hobalight/preserve"
  sudo installer -pkg "$current/$base.pkg" -target /
  check_version /usr/local/bin/hl "$version"
  cmp "$home/config.before" "$home/.hobalight/hobalight.conf"
  test -f "$home/.hobalight/preserve"
  # macOS receipts do not offer file removal; remove only package-owned paths.
  sudo rm /usr/local/bin/hl /usr/local/bin/hobalight
  sudo rm -rf /usr/local/share/doc/hobalight
  sudo pkgutil --forget com.hoba-soft.hobalight
  cmp "$home/config.before" "$home/.hobalight/hobalight.conf"
  test -f "$home/.hobalight/preserve"
fi
