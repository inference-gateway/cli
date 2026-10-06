#!/usr/bin/env bash
# Print an ID token for the service account in .env, minted for the agent's audience.
set -euo pipefail
set -a
. "$(dirname "$0")/.env"
set +a

gcloud auth print-identity-token \
  --impersonate-service-account="${GCP_SERVICE_ACCOUNT}" \
  --audiences="${AGENT_AUDIENCE}" \
  --include-email
