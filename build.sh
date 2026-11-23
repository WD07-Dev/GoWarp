#!/usr/bin/env bash
set -euo pipefail

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
cd "$root"

declare -A build_targets=(
    [windows]="amd64"
    [linux]="amd64"
)

export CGO_ENABLED=0
host_os="$(go env GOHOSTOS)"
host_arch="$(go env GOHOSTARCH)"
dist="$root/output/dist"
packages="$root/output/package"
sdk="$root/app/assets/embed/runtime-source.zip"
mkdir -p "$dist" "$packages"
work="$(mktemp -d "$dist/build.XXXXXX")"
trap 'rm -rf -- "$work"; rm -f -- "$sdk"' EXIT

zip -qr "$work/runtime-source.zip" go.mod go.sum app core platform -x '*_test.go' '*/testdata/*' 'app/assets/embed/runtime-source.zip'
mv -- "$work/runtime-source.zip" "$sdk"
sdk_hash="$(sha256sum "$sdk")"
sdk_hash="${sdk_hash%% *}"

runtime_entries=()
for target_os in "${!build_targets[@]}"; do
    target_arch="${build_targets[$target_os]}"
    name="gowarp-$target_os-$target_arch"
    suffix=''
    linker_flags=''
    if [[ "$target_os" == windows ]]; then
        suffix='.exe'
        linker_flags='-H=windowsgui'
    fi
    runtime_dir="$dist/runtimes/$target_os-$target_arch"
    mkdir -p "$dist/$target_os" "$runtime_dir"

    GOOS="$target_os" GOARCH="$target_arch" go build -buildvcs=false -trimpath -tags gowarp_packager -ldflags="$linker_flags" -o "$work/$name$suffix" "$root/platform/$target_os"
    if [[ "$target_os" == windows ]]; then
        GOOS="$host_os" GOARCH="$host_arch" go run "$root/build.go" "$work/$name$suffix"
    fi
    mv -- "$work/$name$suffix" "$dist/$target_os/$name$suffix"
    GOOS="$target_os" GOARCH="$target_arch" go build -buildvcs=false -trimpath -ldflags="-s -w $linker_flags" -o "$runtime_dir/player$suffix" "$root/platform/$target_os/runtime"
    player_hash="$(sha256sum "$runtime_dir/player$suffix")"
    player_hash="${player_hash%% *}"
    printf '{"version":1,"os":"%s","arch":"%s","sha256":"%s","sdkSHA256":"%s"}\n' "$target_os" "$target_arch" "$player_hash" "$sdk_hash" > "$runtime_dir/player.json"
    runtime_entries+=("runtimes/$target_os-$target_arch")
done

for target_os in "${!build_targets[@]}"; do
    name="gowarp-$target_os-${build_targets[$target_os]}"
    suffix=''
    if [[ "$target_os" == windows ]]; then suffix='.exe'; fi
    zip -qj "$work/$name.zip" "$dist/$target_os/$name$suffix"
    (cd "$dist" && zip -qr "$work/$name.zip" "${runtime_entries[@]}")
    mv -- "$work/$name.zip" "$packages/$name.zip"
    echo "$packages/$name.zip"
done