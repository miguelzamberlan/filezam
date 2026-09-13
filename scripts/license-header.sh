#!/usr/bin/env bash
# Filezam - https://github.com/miguelzamberlan/filezam
# Copyright (C) 2026 Miguel Zamberlan
# SPDX-License-Identifier: AGPL-3.0-only
#
# Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
# Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
# alterar este cabeçalho viola a licença e os direitos autorais do autor.

# Cabeçalho de licença dos arquivos-fonte.
#   scripts/license-header.sh check   lista quem está sem o cabeçalho e sai com 1 (CI, make test)
#   scripts/license-header.sh apply   põe o cabeçalho no topo de quem está sem
# Migrações SQL ficam de fora: uma migração aplicada nunca é editada.
set -euo pipefail
cd "$(dirname "$0")/.."

MARK='SPDX-License-Identifier: AGPL-3.0-only'
BODY=(
  'Filezam - https://github.com/miguelzamberlan/filezam'
  'Copyright (C) 2026 Miguel Zamberlan'
  "$MARK"
  ''
  'Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.'
  'Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou'
  'alterar este cabeçalho viola a licença e os direitos autorais do autor.'
)

files() {
  git ls-files --cached --others --exclude-standard -- \
    'cmd/**.go' 'internal/**.go' 'web/src/**.ts' 'web/src/**.tsx' 'web/src/**.css' 'web/vite.config.ts' 'scripts/*.sh' |
    grep -v '^internal/server/webdist/'
}

header() { # $1 = prefixo de linha ("//" ou "#"); CSS usa bloco /* */
  local p=$1 line
  if [ "$p" = "css" ]; then
    echo '/*'
    for line in "${BODY[@]}"; do [ -n "$line" ] && echo " * $line" || echo ' *'; done
    echo ' */'
  else
    for line in "${BODY[@]}"; do [ -n "$line" ] && echo "$p $line" || echo "$p"; done
  fi
}

case "${1:-check}" in
check)
  missing=0
  while IFS= read -r f; do
    [ -f "$f" ] || continue
    if ! head -n 12 "$f" | grep -qF "$MARK"; then
      echo "sem cabeçalho de licença: $f"
      missing=1
    fi
  done < <(files)
  [ "$missing" = 0 ] || { echo "rode: scripts/license-header.sh apply"; exit 1; }
  ;;
apply)
  while IFS= read -r f; do
    [ -f "$f" ] || continue
    head -n 12 "$f" | grep -qF "$MARK" && continue
    case "$f" in
      *.css) kind=css ;;
      *.sh) kind='#' ;;
      *) kind=// ;;
    esac
    tmp=$(mktemp)
    if [ "$kind" = '#' ] && head -n 1 "$f" | grep -q '^#!'; then
      { head -n 1 "$f"; header "$kind"; echo; tail -n +2 "$f"; } >"$tmp"
    else
      # linha em branco depois: em Go, um comentário colado no "package" vira a doc do pacote
      { header "$kind"; echo; cat "$f"; } >"$tmp"
    fi
    cat "$tmp" >"$f" && rm -f "$tmp"
    echo "cabeçalho aplicado: $f"
  done < <(files)
  ;;
*)
  echo "uso: $0 check|apply" >&2
  exit 2
  ;;
esac
