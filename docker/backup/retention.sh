#!/bin/bash
# retention.sh — prune old backups per retention policy
set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-/backups}"
RETENTION_DAILY="${RETENTION_DAILY:-7}"
RETENTION_WEEKLY="${RETENTION_WEEKLY:-4}"
RETENTION_MONTHLY="${RETENTION_MONTHLY:-3}"

echo "[retention] $(date -Iseconds) — Running retention prune"

for SERVICE_DIR in "$BACKUP_DIR"/*/; do
  [ -d "$SERVICE_DIR" ] || continue
  SERVICE=$(basename "$SERVICE_DIR")

  # Daily dumps (YYYY-MM-DD.dump)
  find "$SERVICE_DIR" -maxdepth 1 -name '????-??-??.dump' -type f \
    | sort -r \
    | tail -n +$((RETENTION_DAILY + 1)) \
    | while read -r f; do
      echo "[retention]   rm daily: $f"
      rm "$f"
    done

  # Weekly dumps (weekly-YYYY-MM-DD.dump)
  find "$SERVICE_DIR" -maxdepth 1 -name 'weekly-*.dump' -type f \
    | sort -r \
    | tail -n +$((RETENTION_WEEKLY + 1)) \
    | while read -r f; do
      echo "[retention]   rm weekly: $f"
      rm "$f"
    done

  # Monthly dumps (monthly-YYYY-MM-DD.dump)
  find "$SERVICE_DIR" -maxdepth 1 -name 'monthly-*.dump' -type f \
    | sort -r \
    | tail -n +$((RETENTION_MONTHLY + 1)) \
    | while read -r f; do
      echo "[retention]   rm monthly: $f"
      rm "$f"
    done
done

echo "[retention] $(date -Iseconds) — Prune complete"
