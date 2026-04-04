#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TARGET="${1:-host}"
OUTPUT_PATH="${2:-}"

if [[ -z "${OUTPUT_PATH}" ]]; then
  echo "Usage: $(basename "$0") <target> <output-path>"
  exit 1
fi

mkdir -p "$(dirname "${OUTPUT_PATH}")"

build_host() {
  (
    cd "${ROOT_DIR}"
    go build -ldflags="-s -w" -o "${OUTPUT_PATH}" ./cmd/localdrop
  )
}

find_android_clang() {
  local candidates=(
    "${ANDROID_NDK_HOME:-}/toolchains/llvm/prebuilt/darwin-x86_64/bin/aarch64-linux-android21-clang"
    "${ANDROID_NDK_HOME:-}/toolchains/llvm/prebuilt/darwin-arm64/bin/aarch64-linux-android21-clang"
    "/opt/homebrew/share/android-ndk/toolchains/llvm/prebuilt/darwin-x86_64/bin/aarch64-linux-android21-clang"
    "/opt/homebrew/share/android-ndk/toolchains/llvm/prebuilt/darwin-arm64/bin/aarch64-linux-android21-clang"
  )

  for candidate in "${candidates[@]}"; do
    if [[ -n "${candidate}" && -x "${candidate}" ]]; then
      echo "${candidate}"
      return 0
    fi
  done

  return 1
}

build_android_arm64() {
  local clang_path
  if ! clang_path="$(find_android_clang)"; then
    echo "Android NDK clang not found."
    echo "Install it with: brew install android-ndk"
    echo "Or set ANDROID_NDK_HOME to your NDK root."
    exit 1
  fi

  (
    cd "${ROOT_DIR}"
    export GOOS=android
    export GOARCH=arm64
    export CGO_ENABLED=1
    export CC="${clang_path}"
    export CXX="${clang_path/clang/clang++}"
    go build -ldflags="-s -w" -o "${OUTPUT_PATH}" ./cmd/localdrop
  )
}

case "${TARGET}" in
  host)
    build_host
    ;;
  android-arm64)
    build_android_arm64
    ;;
  *)
    echo "Unsupported target: ${TARGET}"
    echo "Supported targets: host, android-arm64"
    exit 1
    ;;
esac
