#!/bin/sh
# Database migrations for the Glyph chart — runs in the migrate/migrate image
# (busybox sh + the golang-migrate CLI).
#
#   glyph-migrate.sh apply   Bring the schema up to this release's latest
#                            migration. Run once per install/upgrade by the
#                            migrate Job (a pre-upgrade hook on upgrades).
#                            A schema *newer* than the shipped files (after a
#                            rollback) is left alone with a warning; a dirty
#                            one is refused (see docs/runbooks/migrations.md).
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
RUNBOOK="docs/runbooks/migrations.md in the glyph repo"

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
    if [ "$DIRTY" = true ]; then
      # A migration failed part-way. Retrying `up` on top of that is never
      # safe; a person has to look at what ran and decide.
      log "schema version $CURRENT is dirty: a migration failed part-way."
      log "Fix it by hand before deploying again — see $RUNBOOK"
      exit 1
    fi
    if [ -n "$CURRENT" ] && [ "$CURRENT" -gt "$LATEST" ]; then
      # After a rollback (or a deploy of an older tree) the database has
      # migrations this release doesn't ship. `migrate up` would fail with
      # "no migration found for version N"; the schema is already past what
      # this release needs, so there is nothing to do.
      log "WARNING: schema version $CURRENT is newer than this release's latest migration ($LATEST) — skipping. Expected after a rollback; this release must be compatible with the newer schema (expand/contract)."
      exit 0
    fi
    log "schema version ${CURRENT:-none}, this release ships up to $LATEST — applying"
    if ! migrate -path "$DIR" -database "$DATABASE_URL" up; then
      log "migration failed. If the schema is now dirty, see $RUNBOOK"
      exit 1
    fi
    ;;

  wait)
    attempt=0
    while :; do
      attempt=$((attempt + 1))
      if read_version; then
        if [ -n "$CURRENT" ] && [ "$DIRTY" = false ] && [ "$CURRENT" -ge "$LATEST" ]; then
          if [ "$CURRENT" -gt "$LATEST" ]; then
            log "WARNING: schema version $CURRENT is newer than this release's latest migration ($LATEST) — starting anyway (expected after a rollback)"
          else
            log "schema version $CURRENT is ready"
          fi
          exit 0
        fi
        if [ "$DIRTY" = true ]; then
          log "waiting: schema version $CURRENT is dirty (a migration failed part-way) — see $RUNBOOK"
        else
          log "waiting for the schema: version ${CURRENT:-none}, need $LATEST"
        fi
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
