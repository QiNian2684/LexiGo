release_tag_commit() {
  local tag="$1" refs
  refs="$(git ls-remote --tags origin "refs/tags/$tag" "refs/tags/$tag^{}")" || return
  awk '/\^\{\}$/ {peeled=$1; next} {ref=$1} END {print (peeled ? peeled : ref)}' <<< "$refs"
}

verify_public_asset() {
  local url="$1" destination="$2" checksum="$3" status
  status="$(curl --disable --fail --silent --show-error --location --retry 2 --retry-all-errors \
    --connect-timeout 15 --max-time 120 --proto '=https' --proto-redir '=https' \
    --output "$destination" --write-out '%{http_code}' "$url")" || return
  if [[ "$status" != 200 ]]; then
    printf 'Expected a full HTTP 200 download, got %s for %s\n' "$status" "$url" >&2
    return 1
  fi
  printf '%s  %s\n' "$checksum" "$destination" | sha256sum --strict --check
}

write_apk_notes() {
  local notes_file="$1" apk="$2" url="$3" notice="${4:-}" notes checksum
  notes="$(awk '
    /^<!-- lexigo-apk-checksum:start -->$/ {skip=1; next}
    /^<!-- lexigo-apk-checksum:end -->$/ {skip=0; next}
    !skip
  ' "$notes_file")"
  checksum="$(sha256sum -- "$apk")"
  if [[ -n "$notes" ]]; then
    printf '%s\n\n' "$notes"
  fi
  printf '%s\n\n' '<!-- lexigo-apk-checksum:start -->' '### Android 下载校验'
  if [[ -n "$notice" ]]; then
    printf '%s\n\n' "$notice"
  fi
  printf -- '- 安装包 [`%s`](%s)\n- 大小 `%s` 字节\n- SHA-256 `%s`\n\n' \
    "$(basename "$apk")" "$url" "$(stat --format=%s -- "$apk")" "${checksum%% *}"
  printf '%s\n' '<!-- lexigo-apk-checksum:end -->'
}
