#!/usr/bin/env bash
set -euo pipefail

jq -n '{ok: true, changed: false, observed: {}, plan: {}, conditions: []}'
