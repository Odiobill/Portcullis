#!/bin/bash
# backup.sh — iterate over all service databases and dump each
set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-/backups}"
MANAGER_DB="${MANAGER_DB:-portcullis}"

export PGHOST="${PGHOST:-portcullis_db}"
export PGUSER="${PGUSER:-postgres}"
export PGPASSWORD="${PGPASSWORD:-change_me}"

TODAY=$(date +%Y-%m-%d)
WEEKDAY=$(date +%u)  # 1=Mon ... 7=Sun
DOM=$(date +%d)       # day of month

echo "[backup] $(date -Iseconds) — Starting backup run"

# Get list of service databases
DB_NAMES=$(psql -t -A -d "$MANAGER_DB" \
  -c "SELECT db_name FROM services WHERE db_name IS NOT NULL" 2>/dev/null || true)

if [ -z "$DB_NAMES" ]; then
  echo "[backup] No service databases found — nothing to back up."
  exit 0
fi

echo "[backup] Found $(echo "$DB_NAMES" | wc -l) service database(s)"

for DB_NAME in $DB_NAMES; do
  DB_BACKUP_DIR="$BACKUP_DIR/$DB_NAME"
  mkdir -p "$DB_BACKUP_DIR"

  DUMP_FILE="$DB_BACKUP_DIR/$TODAY.dump"

  echo "[backup] Dumping $DB_NAME → $DUMP_FILE"

  if pg_dump -Fc -d "$DB_NAME" -f "$DUMP_FILE" 2>/tmp/pg_dump_err; then
    SIZE=$(du -h "$DUMP_FILE" | cut -f1)
    echo "[backup]   ✓ $SIZE"

    # Tag for retention: daily, weekly (Sunday), monthly (1st)
    # Weekly tag
    if [ "$WEEKDAY" = "7" ]; then
      cp "$DUMP_FILE" "$DB_BACKUP_DIR/weekly-$TODAY.dump"
      echo "[backup]   → weekly copy"
    fi
    # Monthly tag
    if [ "$DOM" = "01" ]; then
      cp "$DUMP_FILE" "$DB_BACKUP_DIR/monthly-$TODAY.dump"
      echo "[backup]   → monthly copy"
    fi
  else
    echo "[backup]   ✗ FAILED: $(cat /tmp/pg_dump_err)"
  fi
done

echo "[backup] $(date -Iseconds) — Backup run complete"
