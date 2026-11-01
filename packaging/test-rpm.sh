#!/usr/bin/env bash
set -euo pipefail
version="$RELEASE_VERSION"
arch="$TARGET_ARCH"
rpm -i "/packages/dist-previous/hobalight_0.0.0_linux_$arch.rpm"
hobalight --version | grep -F 'hobalight 0.0.0-'
export HOME="$(mktemp -d)"
hobalight info
cp "$HOME/.hobalight/hobalight.conf" "$HOME/config.before"
echo 'keep user data' > "$HOME/.hobalight/preserve"
rpm -U "/packages/dist/hobalight_${version}_linux_$arch.rpm"
hl --version | grep -F "hobalight $version-"
[[ "$(cat "$HOME/config.before")" == "$(cat "$HOME/.hobalight/hobalight.conf")" ]]
rpm -e hoba-light
test ! -e /usr/bin/hobalight
test ! -e /usr/bin/hl
[[ "$(cat "$HOME/config.before")" == "$(cat "$HOME/.hobalight/hobalight.conf")" ]]
test -f "$HOME/.hobalight/preserve"
