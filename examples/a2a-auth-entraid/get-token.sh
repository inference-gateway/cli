#!/usr/bin/env bash
# Print an access token for the agent's API registration, as the signed-in az account.
set -euo pipefail
set -a
. "$(dirname "$0")/.env"
set +a

az account get-access-token --resource "api://${AZURE_API_APP_ID}" --query accessToken -o tsv
