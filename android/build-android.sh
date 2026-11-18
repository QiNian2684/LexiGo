#!/usr/bin/env bash
set -euo pipefail

project="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
android_project="$project/android"
output="${1:-$project/dist/LexiGo.apk}"
mkdir -p "$(dirname "$output")"
output="$(cd "$(dirname "$output")" && pwd)/$(basename "$output")"
sdk="${ANDROID_SDK_ROOT:-${ANDROID_HOME:-}}"
ndk="${ANDROID_NDK_HOME:-${ANDROID_NDK_ROOT:-}}"
java_root="${WORDREVIEW_JAVA_HOME:-${JAVA_HOME:-}}"
go_cmd="${WORDREVIEW_GO:-go}"
tools_dir="${WORDREVIEW_ANDROID_BUILD_TOOLS:-$sdk/build-tools/35.0.0}"
platform="${WORDREVIEW_ANDROID_PLATFORM:-$sdk/platforms/android-35/android.jar}"
abi_list="${WORDREVIEW_ANDROID_ABIS:-arm64-v8a x86_64}"
if [[ "$(uname -s)" == Darwin ]]; then host_tag=darwin-x86_64; else host_tag=linux-x86_64; fi
clang_dir="$ndk/toolchains/llvm/prebuilt/$host_tag/bin"
javac_cmd="${java_root:+$java_root/bin/}javac"
keytool_cmd="${java_root:+$java_root/bin/}keytool"

if [[ -z "$sdk" || -z "$ndk" || ! -f "$platform" || ! -x "$tools_dir/aapt" ]]; then
  echo 'Set ANDROID_SDK_ROOT and ANDROID_NDK_HOME, install SDK platform 35 and build-tools 35.0.0.' >&2
  exit 1
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/classes" "$work/dex" "$work/apk/lib"
mapfile_list=()
while IFS= read -r file; do mapfile_list+=("$file"); done < <(find "$android_project/src/main/java" -name '*.java' -type f | sort)
"$javac_cmd" --release 8 -encoding UTF-8 -classpath "$platform" -d "$work/classes" "${mapfile_list[@]}"
compiled_classes=()
while IFS= read -r file; do compiled_classes+=("$file"); done < <(find "$work/classes" -type f -name '*.class' | sort)
env "JAVA_HOME=$java_root" "$tools_dir/d8" --min-api 28 --lib "$platform" --output "$work/dex" "${compiled_classes[@]}"
for abi in $abi_list; do
  case "$abi" in
    arm64-v8a) target_arch=arm64; compiler=aarch64-linux-android28-clang ;;
    x86_64) target_arch=amd64; compiler=x86_64-linux-android28-clang ;;
    *) echo "Unsupported ABI: $abi" >&2; exit 1 ;;
  esac
  mkdir -p "$work/apk/lib/$abi"
  (
    cd "$project"
    GOOS=android GOARCH="$target_arch" CGO_ENABLED=1 CC="$clang_dir/$compiler" \
      "$go_cmd" build -buildvcs=false -trimpath -buildmode=c-shared \
      -ldflags='-s -w -extldflags=-Wl,-z,max-page-size=16384' \
      -o "$work/apk/lib/$abi/libwordreview.so" .
  )
  rm -f "$work/apk/lib/$abi/libwordreview.h"
done
manifest="$android_project/src/main/AndroidManifest.xml"
package_args=()
if [[ -n "${WORDREVIEW_ANDROID_PACKAGE:-}" ]]; then
  [[ "$WORDREVIEW_ANDROID_PACKAGE" =~ ^[a-zA-Z][a-zA-Z0-9_]*(\.[a-zA-Z][a-zA-Z0-9_]*)+$ ]] || {
    echo 'Invalid Android package identifier.' >&2
    exit 1
  }
  sed "s/cn\.wordreview\.app\.lexigo\.share\"/${WORDREVIEW_ANDROID_PACKAGE}.share\"/" "$manifest" > "$work/AndroidManifest.xml"
  manifest="$work/AndroidManifest.xml"
  package_args=(--rename-manifest-package "$WORDREVIEW_ANDROID_PACKAGE")
fi
"$tools_dir/aapt" package -f "${package_args[@]}" -M "$manifest" \
  -S "$android_project/src/main/res" -I "$platform" -F "$work/resources.apk"
python3 - "$work" <<'PY'
from pathlib import Path
import sys
import zipfile
work = Path(sys.argv[1])
with zipfile.ZipFile(work / "resources.apk", "a", zipfile.ZIP_DEFLATED, compresslevel=6) as apk:
    apk.write(work / "dex/classes.dex", "classes.dex")
    for library in sorted((work / "apk/lib").rglob("*.so")):
        apk.write(library, library.relative_to(work / "apk").as_posix())
PY
"$tools_dir/zipalign" -P 16 -f 4 "$work/resources.apk" "$work/aligned.apk"

signing_key="${WORDREVIEW_KEYSTORE:-$project/.local-signing/wordreview.p12}"
signing_alias="${WORDREVIEW_KEY_ALIAS:-wordreview}"
if [[ -z "${WORDREVIEW_KEY_PASSWORD:-}" ]]; then
  printf 'Signing password: ' >&2
  IFS= read -rs WORDREVIEW_KEY_PASSWORD
  printf '\n' >&2
fi
export WORDREVIEW_KEY_PASSWORD
if [[ ! -f "$signing_key" ]]; then
  mkdir -p "$(dirname "$signing_key")"
  (umask 077; "$keytool_cmd" -genkeypair -keystore "$signing_key" -storetype PKCS12 \
    -storepass:env WORDREVIEW_KEY_PASSWORD -alias "$signing_alias" -keyalg RSA -keysize 3072 \
    -validity 10000 -dname 'CN=LexiGo Release')
fi
env "JAVA_HOME=$java_root" "$tools_dir/apksigner" sign --ks "$signing_key" --ks-key-alias "$signing_alias" \
  --ks-pass env:WORDREVIEW_KEY_PASSWORD --out "$output" "$work/aligned.apk"
env "JAVA_HOME=$java_root" "$tools_dir/apksigner" verify --verbose "$output"
echo "$output"
