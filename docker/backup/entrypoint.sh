#!/bin/bash
# entrypoint.sh — sleep-loop cron scheduler (no dcron, no setpgid issues)
set -euo pipefail

SCHEDULE="${BACKUP_SCHEDULE:-0 3 * * *}"

# Parse cron expression into minute and hour
CRON_MIN=$(echo "$SCHEDULE" | awk '{print $1}')
CRON_HOUR=$(echo "$SCHEDULE" | awk '{print $2}')

echo "[backup] Cron schedule: minute=$CRON_MIN hour=$CRON_HOUR (UTC if TZ not set)"
echo "[backup] Starting sleep-loop scheduler..."

LAST_RUN_DATE=""

while true; do
  NOW_MIN=$(date +%M)
  NOW_HOUR=$(date +%H)
  NOW_DATE=$(date +%Y-%m-%d)

  # Remove leading zeros for comparison
  CMIN=${CRON_MIN#0}
  CHOUR=${CRON_HOUR#0}
  NMIN=${NOW_MIN#0}
  NHOUR=${NOW_HOUR#0}

  if [ "$CMIN" = "$NMIN" ] && [ "$CHOUR" = "$NHOUR" ] && [ "$NOW_DATE" != "$LAST_RUN_DATE" ]; then
    echo "[backup] $(date -Iseconds) — Triggering backup..."
    /bin/bash /app/backup.sh
    /bin/bash /app/retention.sh
    LAST_RUN_DATE="$NOW_DATE"
    # Sleep past the current minute to avoid double-trigger
    sleep 61
  fi

  sleep 1
done
