#!/usr/bin/env bash
# Production Disaster Recovery Database Backup Script
# Uses custom compressed archive format (pg_dump -Fc) with SHA256 checksums and retention rotation.

set -euo pipefail

BACKUP_DIR="${1:-./backups}"
RETENTION_DAYS="${RETENTION_DAYS:-7}"

PGHOST="${PGHOST:-localhost}"
PGPORT="${PGPORT:-5432}"
PGUSER="${PGUSER:-vpnbuilder}"
PGDATABASE="${PGDATABASE:-vpnbuilder}"
PGPASSWORD="${PGPASSWORD:-vpnbuilder}"

export PGPASSWORD

mkdir -p "${BACKUP_DIR}"

TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
BACKUP_FILENAME="${PGDATABASE}_${TIMESTAMP}.dump"
BACKUP_PATH="${BACKUP_DIR}/${BACKUP_FILENAME}"
CHECKSUM_PATH="${BACKUP_PATH}.sha256"

echo "[$(date -u +"%Y-%m-%dT%H:%M:%SZ")] Starting database backup for '${PGDATABASE}' to '${BACKUP_PATH}'..."

# Run atomic serializable pg_dump in custom binary compressed format (-Fc)
pg_dump \
  -h "${PGHOST}" \
  -p "${PGPORT}" \
  -U "${PGUSER}" \
  -d "${PGDATABASE}" \
  -Fc \
  --serializable-deferrable \
  --no-owner \
  --no-privileges \
  --file="${BACKUP_PATH}"

# Generate SHA256 checksum
sha256sum "${BACKUP_PATH}" > "${CHECKSUM_PATH}"

BACKUP_SIZE=$(du -h "${BACKUP_PATH}" | cut -f1)
echo "[$(date -u +"%Y-%m-%dT%H:%M:%SZ")] Backup completed successfully. Size: ${BACKUP_SIZE}"
echo "[$(date -u +"%Y-%m-%dT%H:%M:%SZ")] SHA256: $(cat "${CHECKSUM_PATH}")"

# Retention policy rotation: delete backups older than RETENTION_DAYS
echo "[$(date -u +"%Y-%m-%dT%H:%M:%SZ")] Enforcing retention policy: pruning backups older than ${RETENTION_DAYS} days..."
find "${BACKUP_DIR}" -type f \( -name "*.dump" -o -name "*.dump.sha256" \) -mtime +"${RETENTION_DAYS}" -exec rm -f {} +

echo "[$(date -u +"%Y-%m-%dT%H:%M:%SZ")] Database backup lifecycle completed."
