#!/bin/sh
# Database migrations for the Glyph chart — runs in the migrate/migrate image
# (busybox sh + the golang-migrate CLI).
#
#   glyph-migrate.sh apply   Bring the schema up to this release's latest
#                            migration. Run once per install/upgrade by the
#                            migrate Job (a pre-upgrade hook on upgrades).
#   glyph-migrate.sh wait    Check-only: block until the schema is at least
#                            this release's latest migration. Run by each API
#                            pod's init container; it never changes the schema.
#
# Env: DATABASE_URL (required), MIGRATIONS_DIR (default /migrations),
#      WAIT_INTERVAL_SECONDS (default 2), WAIT_MAX_ATTEMPTS (default 0 = forever).
set -eu

MODE="${1:-}"
DIR="${MIGRATIONS_DIR:-/migrations}"
INTERVAL="${WAIT_INTERVAL_SECONDS:-2}"
MAX_ATTEMPTS="${WAIT_MAX_ATTEMPTS:-0}"

log() { echo "glyph-migrate: $*" >&2; }

# Highest migration version shipped with this release (000021_x.up.sql → 21).
latest_shipped() {
  ls "$DIR" | sed -n 's/^0*\([0-9][0-9]*\)_.*\.up\.sql$/\1/p' | sort -n | tail -n 1
}

# Reads the schema version into CURRENT (empty for a fresh database) and
# DIRTY (true/false). Returns 1 if the database can't be queried.
read_version() {
  CURRENT=""
  DIRTY=false
  if out="$(migrate -path "$DIR" -database "$DATABASE_URL" version 2>&1)"; then
    :
  else
    case "$out" in
      *"no migration"*) return 0 ;;
      *) VERSION_ERROR="$out"; return 1 ;;
    esac
  fi
  CURRENT="$(printf '%s\n' "$out" | sed -n 's/^\([0-9][0-9]*\).*/\1/p' | tail -n 1)"
  case "$out" in *dirty*) DIRTY=true ;; esac
  if [ -z "$CURRENT" ]; then
    VERSION_ERROR="unrecognised output from 'migrate version': $out"
    return 1
  fi
}

LATEST="$(latest_shipped)"
if [ -z "$LATEST" ]; then
  log "no *.up.sql migrations found in $DIR"
  exit 1
fi

case "$MODE" in
  apply)
    if ! read_version; then
      log "cannot read the schema version: $VERSION_ERROR"
      exit 1
    fi
    log "schema version ${CURRENT:-none}, this release ships up to $LATEST — applying"
    migrate -path "$DIR" -database "$DATABASE_URL" up
    ;;

  wait)
    attempt=0
    while :; do
      attempt=$((attempt + 1))
      if read_version; then
        if [ -n "$CURRENT" ] && [ "$DIRTY" = false ] && [ "$CURRENT" -ge "$LATEST" ]; then
          log "schema version $CURRENT is ready (this release needs $LATEST)"
          exit 0
        fi
        log "waiting for the schema: version ${CURRENT:-none}$( [ "$DIRTY" = true ] && echo ' (dirty)' ), need $LATEST"
      else
        log "waiting for the database: $VERSION_ERROR"
      fi
      if [ "$MAX_ATTEMPTS" -gt 0 ] && [ "$attempt" -ge "$MAX_ATTEMPTS" ]; then
        log "gave up waiting after $attempt attempts"
        exit 1
      fi
      sleep "$INTERVAL"
    done
    ;;

  *)
    echo "usage: $0 apply|wait" >&2
    exit 2
    ;;
esac
