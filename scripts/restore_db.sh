#!/usr/bin/env bash
# Production Disaster Recovery Database Restore Script
# Validates SHA256 checksum and restores via pg_restore.

set -euo pipefail

BACKUP_FILE="${1:-}"

if [[ -z "${BACKUP_FILE}" ]]; then
  echo "Usage: $0 <path-to-dump-file>"
  exit 1
fi

if [[ ! -f "${BACKUP_FILE}" ]]; then
  echo "Error: backup file '${BACKUP_FILE}' does not exist."
  exit 1
fi

PGHOST="${PGHOST:-localhost}"
PGPORT="${PGPORT:-5432}"
PGUSER="${PGUSER:-vpnbuilder}"
PGDATABASE="${PGDATABASE:-vpnbuilder}"
PGPASSWORD="${PGPASSWORD:-vpnbuilder}"

export PGPASSWORD

CHECKSUM_FILE="${BACKUP_FILE}.sha256"
if [[ -f "${CHECKSUM_FILE}" ]]; then
  echo "[$(date -u +"%Y-%m-%dT%H:%M:%SZ")] Verifying SHA256 checksum..."
  (cd "$(dirname "${BACKUP_FILE}")" && sha256sum -c "$(basename "${CHECKSUM_FILE}")")
else
  echo "[$(date -u +"%Y-%m-%dT%H:%M:%SZ")] Warning: No checksum file found. Proceeding without checksum verification."
fi

echo "[$(date -u +"%Y-%m-%dT%H:%M:%SZ")] Restoring database '${PGDATABASE}' from '${BACKUP_FILE}'..."

pg_restore \
  -h "${PGHOST}" \
  -p "${PGPORT}" \
  -U "${PGUSER}" \
  -d "${PGDATABASE}" \
  --clean \
  --if-exists \
  --no-owner \
  --no-privileges \
  "${BACKUP_FILE}"

echo "[$(date -u +"%Y-%m-%dT%H:%M:%SZ")] Database restoration completed successfully."
