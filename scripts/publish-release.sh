#!/usr/bin/env bash
set -euo pipefail
source scripts/release-assets.sh

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
printf '%s\n' "$apk" "$setup" "$portable" | sort > "$work/expected-assets"

# An existing tag must identify the commit used by both build jobs.
tag_commit="$(release_tag_commit "$tag")"
if [[ -n "$tag_commit" && "$tag_commit" != "$GITHUB_SHA" ]]; then
  printf 'Tag %s already points to another commit.\n' "$tag" >&2
  exit 1
fi

draft=true
public_apk="$apk"
if gh release view "$tag" --json isDraft,targetCommitish,assets > "$work/existing.json"; then
  draft="$(jq -r '.isDraft' "$work/existing.json")"
  if [[ "$draft" == true ]]; then
    [[ -n "$tag_commit" ]] || jq -e --arg sha "$GITHUB_SHA" '.targetCommitish == $sha' "$work/existing.json" > /dev/null
    jq -r '.assets[].name' "$work/existing.json" | sort > "$work/existing-assets"
    [[ -z "$(comm -23 "$work/existing-assets" "$work/expected-assets")" ]]
    gh release upload "$tag" "${uploads[@]}" --clobber
  else
    [[ "$tag_commit" == "$GITHUB_SHA" ]]
    public_apk="$(jq -er --arg apk "$apk" '
      ($apk | rtrimstr(".apk")) as $base
      | [.assets[].name | select(endswith(".apk"))]
      | select(length == 1)
      | .[0]
      | select(. == $apk or (startswith($base) and (ltrimstr($base) | test("^-r[1-9][0-9]{0,3}\\.apk$"))))
    ' "$work/existing.json")"
  fi
else
  notes=(--generate-notes)
  if [[ -s "releases/$tag.md" ]]; then
    notes=(--notes-file "releases/$tag.md")
  fi
  gh release create "$tag" "${uploads[@]}" --draft --target "$GITHUB_SHA" --title "LexiGo $version" "${notes[@]}"
fi

for file in "${uploads[@]}"; do
  checksum="$(sha256sum -- "$file")"
  name="$(basename "$file")"
  if [[ "$name" == "$apk" ]]; then
    name="$public_apk"
  fi
  printf '%s  %s\n' "${checksum%% *}" "$name"
done > "$work/expected-sha256"
printf '%s\n' "$public_apk" "$setup" "$portable" | sort > "$work/expected-assets"
gh release view "$tag" --json assets --jq '.assets[].name' | sort > "$work/uploaded-assets"
diff -u "$work/expected-assets" "$work/uploaded-assets"
download_url="https://github.com/$GH_REPO/releases/download/$tag"
if [[ "$draft" == true ]]; then
  gh release download "$tag" --dir "$work" --pattern "$apk" --pattern "$setup" --pattern "$portable"
  (cd "$work" && sha256sum --strict --check expected-sha256)
  gh release view "$tag" --json body --jq '.body' > "$work/notes.md"
  write_apk_notes "$work/notes.md" "dist/android/$apk" "$download_url/$apk" > "$work/publish-notes.md"
  gh release edit "$tag" --draft=false --latest --title "LexiGo $version" --notes-file "$work/publish-notes.md"
fi

# Check the unauthenticated routes used by browsers after the draft goes public.
mkdir "$work/public"
while read -r checksum name; do
  verify_public_asset "$download_url/$name" "$work/public/$name" "$checksum"
done < "$work/expected-sha256"
