#!/usr/bin/env bash
set -euo pipefail

version="$(python3 - <<'PY'
from xml.etree.ElementTree import parse
manifest = parse("android/src/main/AndroidManifest.xml").getroot()
print(manifest.attrib["{http://schemas.android.com/apk/res/android}versionName"])
PY
)"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]
windows_version="$(sed -n 's/^!define VERSION "\([0-9.]*\)"$/\1/p' installer/LexiGo.nsi)"
[[ "$version" == "$windows_version" ]]
tag="v$version"
apk="LexiGo-Android-$version.apk"
setup="LexiGo-Setup-$version.exe"
portable=LexiGo.exe
uploads=("dist/android/$apk" "dist/windows/$setup" "dist/windows/$portable")

(cd dist/android && sha256sum --strict --check SHA256SUMS)
(cd dist/windows && sha256sum --strict --check SHA256SUMS)
work="$(mktemp -d)"
trap 'rm -rf -- "$work"' EXIT
for file in "${uploads[@]}"; do
  (cd "$(dirname "$file")" && sha256sum -- "$(basename "$file")")
done > "$work/expected-sha256"
printf '%s\n' "$apk" "$setup" "$portable" | sort > "$work/expected-assets"

# An existing tag must identify the commit used by both build jobs.
tag_refs="$(git ls-remote --tags origin "refs/tags/$tag" "refs/tags/$tag^{}")"
tag_commit="$(awk '/\^\{\}$/ {peeled=$1; next} {ref=$1} END {print (peeled ? peeled : ref)}' <<< "$tag_refs")"
if [[ -n "$tag_commit" && "$tag_commit" != "$GITHUB_SHA" ]]; then
  printf 'Tag %s already points to another commit.\n' "$tag" >&2
  exit 1
fi

draft=true
if gh release view "$tag" --json isDraft,targetCommitish,assets > "$work/existing.json"; then
  draft="$(jq -r '.isDraft' "$work/existing.json")"
  if [[ "$draft" == true ]]; then
    [[ -n "$tag_commit" ]] || jq -e --arg sha "$GITHUB_SHA" '.targetCommitish == $sha' "$work/existing.json" > /dev/null
    jq -r '.assets[].name' "$work/existing.json" | sort > "$work/existing-assets"
    [[ -z "$(comm -23 "$work/existing-assets" "$work/expected-assets")" ]]
    gh release upload "$tag" "${uploads[@]}" --clobber
  else
    [[ "$tag_commit" == "$GITHUB_SHA" ]]
  fi
else
  notes=(--generate-notes)
  if [[ -s "releases/$tag.md" ]]; then
    notes=(--notes-file "releases/$tag.md")
  fi
  gh release create "$tag" "${uploads[@]}" --draft --target "$GITHUB_SHA" --title "LexiGo $version" "${notes[@]}"
fi

gh release view "$tag" --json assets --jq '.assets[].name' | sort > "$work/uploaded-assets"
diff -u "$work/expected-assets" "$work/uploaded-assets"
gh release download "$tag" --dir "$work" --pattern "$apk" --pattern "$setup" --pattern "$portable"
(cd "$work" && sha256sum --strict --check expected-sha256)
if [[ "$draft" == true ]]; then
  gh release edit "$tag" --draft=false --latest --title "LexiGo $version"
fi
