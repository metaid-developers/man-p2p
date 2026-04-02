#!/usr/bin/env bash
set -euo pipefail

if [[ $# -eq 0 ]]; then
  echo "usage: $0 <command> [args...]" >&2
  exit 64
fi

if [[ "$(uname -s)" != "Darwin" ]]; then
  exec "$@"
fi

sdk_root="${SDKROOT:-}"
if [[ -z "$sdk_root" ]]; then
  sdk_root="$(xcrun --sdk macosx --show-sdk-path)"
fi

sdk_flags="-isysroot ${sdk_root} -I${sdk_root}/usr/include"
ld_flags="-isysroot ${sdk_root}"

# Prevent host-installed headers under /usr/local/include from shadowing the macOS SDK.
exec env \
  SDKROOT="${sdk_root}" \
  CPATH="" \
  C_INCLUDE_PATH="" \
  OBJC_INCLUDE_PATH="" \
  CGO_CFLAGS="${sdk_flags}${CGO_CFLAGS:+ ${CGO_CFLAGS}}" \
  CGO_CPPFLAGS="${sdk_flags}${CGO_CPPFLAGS:+ ${CGO_CPPFLAGS}}" \
  CGO_LDFLAGS="${ld_flags}${CGO_LDFLAGS:+ ${CGO_LDFLAGS}}" \
  "$@"
