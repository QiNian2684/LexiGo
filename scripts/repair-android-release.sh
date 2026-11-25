#!/usr/bin/env bash
set -euo pipefail
source scripts/release-assets.sh

tag="$1"
run_id="$2"
revision="$3"
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ && "$run_id" =~ ^[1-9][0-9]*$ && "$revision" =~ ^[1-9][0-9]{0,3}$ ]]

version="${tag#v}"
base="LexiGo-Android-$version"
apk="$base.apk"
replacement="$base-r$revision.apk"
setup="LexiGo-Setup-$version.exe"
endpoint="repos/$GH_REPO/releases/tags/$tag"
run_endpoint="repos/$GH_REPO/actions/runs/$run_id"
url="https://github.com/$GH_REPO/releases/download/$tag/$replacement"
work="$(mktemp -d)"
trap 'rm -rf -- "$work"' EXIT

tag_commit="$(release_tag_commit "$tag")"
[[ "$tag_commit" =~ ^[0-9a-f]{40}$ ]]
gh api "$run_endpoint" | jq -e --arg sha "$tag_commit" --arg repo "$GH_REPO" '
  .status == "completed" and .head_sha == $sha and .head_repository.full_name == $repo
  and .path == ".github/workflows/release.yml"
' > /dev/null
gh api "$run_endpoint/jobs?per_page=100" | jq -e '
  any(.jobs[]; .name == "android / install" and .conclusion == "success")
' > /dev/null
gh run download "$run_id" --name verified-android-release --dir "$work/artifact"
(cd "$work/artifact" && sha256sum --strict --check SHA256SUMS)
checksum="$(sha256sum -- "$work/artifact/$apk")"
checksum="${checksum%% *}"
size="$(stat --format=%s -- "$work/artifact/$apk")"

gh api "$endpoint" > "$work/release.json"
previous="$(jq -er --arg setup "$setup" --arg next "$replacement" '
  select(.draft == false and .immutable != true)
  | [.assets[].name] as $names
  | select(($names | index($setup)) != null and ($names | index("LexiGo.exe")) != null)
  | $names - [$setup, "LexiGo.exe", $next]
  | select(length <= 1) | .[0] // ""
' "$work/release.json")"
if [[ -n "$previous" && "$previous" != "$apk" ]]; then
  [[ "$previous" =~ ^"$base"-r([1-9][0-9]{0,3})\.apk$ && "${BASH_REMATCH[1]}" -lt "$revision" ]]
fi

registered_asset_matches() {
  jq -e --arg name "$1" --arg digest "sha256:$checksum" --argjson size "$size" '
    any(.assets[]; .name == $name and .state == "uploaded" and .digest == $digest and .size == $size)
  ' "$work/release.json" > /dev/null
}

# A replacement must be byte-identical to the APK originally registered for the tag.
if [[ -n "$previous" ]]; then
  registered_asset_matches "$previous"
fi
if ! jq -e --arg name "$replacement" 'any(.assets[]; .name == $name)' "$work/release.json" > /dev/null; then
  [[ -n "$previous" ]]
  cp -- "$work/artifact/$apk" "$work/$replacement"
  gh release upload "$tag" "$work/$replacement"
  gh api "$endpoint" > "$work/release.json"
fi
registered_asset_matches "$replacement"
verify_public_asset "$url" "$work/$replacement" "$checksum"

notes="$(jq -r '.body // ""' "$work/release.json")"
if [[ -n "$previous" ]]; then
  notes="${notes//"$previous"/"$replacement"}"
fi
printf '%s\n' "$notes" > "$work/notes.md"
write_apk_notes "$work/notes.md" "$work/$replacement" "$url" \
  '此附件重新上传了原版已签名 APK。若之前下载时遇到解析失败，请从此新地址重新下载。' > "$work/repair-notes.md"
gh release edit "$tag" --notes-file "$work/repair-notes.md"
if [[ -n "$previous" ]]; then
  gh release delete-asset "$tag" "$previous" --yes
fi

gh api "$endpoint" > "$work/release.json"
jq -e --arg setup "$setup" --arg apk "$replacement" '
  ([.assets[].name] | sort) == ([$setup, "LexiGo.exe", $apk] | sort)
' "$work/release.json" > /dev/null
registered_asset_matches "$replacement"
