#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
fixture_dir="$(mktemp -d "${TMPDIR:-/tmp}/barista-refactor-smoke.XXXXXX")"
project="barista-refactor-smoke-$$"
export BARISTA_SMOKE_DIR="$fixture_dir"
export BARISTA_ENV_FILE="$fixture_dir/smoke.env"
export BARISTA_SMOKE_PORT="${BARISTA_SMOKE_PORT:-13030}"
export BARISTA_E2E_URL="http://localhost:$BARISTA_SMOKE_PORT"
node "$repo_root/frontend/e2e/prepare-compose.mjs" "$fixture_dir"
compose=(docker compose --env-file "$fixture_dir/smoke.env" -p "$project" -f "$repo_root/docker-compose.yml" -f "$repo_root/frontend/e2e/compose.smoke.yaml")
cleanup() {
  "${compose[@]}" logs --no-color >> "$fixture_dir/runtime.log" 2>&1 || true
  "${compose[@]}" down -v --remove-orphans
  echo "Smoke artifacts: $fixture_dir"
}
trap cleanup EXIT
"${compose[@]}" config --format json > "$fixture_dir/compose.json"
"${compose[@]}" up --build -d --wait
"${compose[@]}" exec -T barista-api sh -c 'test "$BARISTA_SMOKE_SENTINEL" = fixture-only && test "$SECURE_API_KEY" = e2e-dummy-credential && test ! -e /app/.env'
npm --prefix "$repo_root/frontend" run test:e2e
node "$repo_root/frontend/e2e/compose-state.mjs" prepare "$fixture_dir/expected.json"
"${compose[@]}" logs --no-color >> "$fixture_dir/runtime.log" 2>&1
"${compose[@]}" up -d --no-deps --force-recreate --wait barista-api
node "$repo_root/frontend/e2e/compose-state.mjs" verify "$fixture_dir/expected.json"
node "$repo_root/frontend/e2e/compose-state.mjs" interrupt "$fixture_dir/expected.json"
"${compose[@]}" logs --no-color >> "$fixture_dir/runtime.log" 2>&1
"${compose[@]}" kill -s KILL barista-api
"${compose[@]}" up -d --no-deps --force-recreate --wait barista-api
node "$repo_root/frontend/e2e/compose-state.mjs" verify-interrupted "$fixture_dir/expected.json"
"${compose[@]}" exec -T barista-api cat /app/data/state.json > "$fixture_dir/state.json"
backend_image="$("${compose[@]}" images -q barista-api)"
docker image inspect --format '{{json .Config.Env}}' "$backend_image" > "$fixture_dir/image-env.json"
"${compose[@]}" logs --no-color >> "$fixture_dir/runtime.log" 2>&1
node "$repo_root/frontend/e2e/compose-state.mjs" audit "$fixture_dir"
