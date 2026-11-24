#!/usr/bin/env bash
set -euo pipefail

apk="$(realpath "$1")"
work="$(mktemp -d)"
export ANDROID_HOME="$work/sdk"
export ANDROID_SDK_ROOT="$ANDROID_HOME"
export ANDROID_USER_HOME="$work/user"
export ANDROID_AVD_HOME="$work/avd"
export PATH="$ANDROID_HOME/platform-tools:$PATH"
emulator_pid=
cleanup() {
  status=$?
  if [[ -n "$emulator_pid" ]]; then
    adb emu kill >/dev/null 2>&1 || true
    kill "$emulator_pid" 2>/dev/null || true
  fi
  if ((status != 0)) && [[ -f "$work/emulator.log" ]]; then
    tail -60 "$work/emulator.log"
  fi
  rm -rf -- "$work"
}
trap cleanup EXIT

download() {
  local url="$1" checksum="$2" file="$work/archive.zip"
  curl --fail --silent --show-error --location --retry 3 "$url" -o "$file"
  printf '%s  %s\n' "$checksum" "$file" | sha1sum --check
  unzip -qo "$file" -d "$ANDROID_HOME/$3"
  rm -- "$file"
}
repository=https://dl.google.com/android/repository
mkdir -p "$ANDROID_HOME" "$ANDROID_USER_HOME" "$ANDROID_AVD_HOME"
download "$repository/build-tools_r35_linux.zip" 2cfaa0bbb2336e9ec18ed3ecea84fa2e2af607bc .
download "$repository/platform-tools_r37.0.3-linux.zip" e1579b288a7ae4e9bc28dedec6a154d78b3c04ed .
download "$repository/emulator-linux_x64-16970787.zip" 5f7314368fc6c05c937f6991bb4106214dc389e7 .
download "$repository/commandlinetools-linux-14742923_latest.zip" 48833c34b761c10cb20bcd16582129395d121b27 .
image="system-images/android-36/google_apis"
mkdir -p "$ANDROID_HOME/$image"
download "$repository/sys-img/google_apis/x86_64-36_r07.zip" c6bf44bdcd885bb902b4ba752d111a073ad7a817 "$image"
if [[ -e /dev/kvm && ! -w /dev/kvm ]]; then
  sudo setfacl -m "u:$(id -un):rw" /dev/kvm
fi

echo no | "$ANDROID_HOME/cmdline-tools/bin/avdmanager" create avd --force --name lexigo-test \
  --package 'system-images;android-36;google_apis;x86_64'
"$ANDROID_HOME/emulator/emulator" -avd lexigo-test -no-window -no-audio -no-snapshot -no-boot-anim \
  -gpu swiftshader_indirect -accel auto >"$work/emulator.log" 2>&1 &
emulator_pid=$!
timeout 390 bash -c 'until [[ "$(adb shell getprop sys.boot_completed 2>/dev/null | tr -d "\r")" == 1 ]]; do sleep 3; done'
[[ "$(adb shell getprop ro.build.version.sdk | tr -d '\r')" == 36 ]]

aapt="$ANDROID_HOME/android-15/aapt"
"$aapt" dump badging "$apk" >"$work/badging.txt"
read -r package version < <(sed -n "1s/^package: name='\([^']*\)' versionCode='\([^']*\)'.*/\1 \2/p" "$work/badging.txt")
activity="$package/cn.wordreview.app.MainActivity"
install() {
  local file="$1" output
  adb push "$file" /data/local/tmp/lexigo.apk
  output="$(adb shell pm install -r /data/local/tmp/lexigo.apk | tr -d '\r')"
  printf '%s\n' "$output"
  [[ "$output" == Success ]]
}
verify_install() {
  adb shell am start -W -n "$activity" | grep -F 'Status: ok'
  adb shell dumpsys package "$package" >"$work/package.txt"
  grep -m1 -F "versionCode=$version " "$work/package.txt"
}

# Copy only the APK so the package manager cannot rely on a v4 sidecar.
install "$apk"
verify_install

prior_sha=8241caf38ae40c7bcad34cc8b4d9a5dc326ba6dafa666f2eea381d68808f8e82
curl --fail --silent --show-error --location --retry 3 \
  https://github.com/QiNian2684/LexiGo/releases/download/v3.5.0/LexiGo-Android-3.5.0.apk \
  -o "$work/previous.apk"
printf '%s  %s\n' "$prior_sha" "$work/previous.apk" | sha256sum --check
adb uninstall "$package"
install "$work/previous.apk"
install "$apk"
verify_install
