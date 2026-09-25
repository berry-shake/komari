#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
# Only a committed full SHA is allowed; never follow a moving upstream branch.
frontend_ref=$(tr -d '\r\n' < .fork/frontend-ref)
[[ "$frontend_ref" =~ ^[0-9a-f]{40}$ ]] || { echo 'Invalid .fork/frontend-ref' >&2; exit 1; }
frontend_dir=$(mktemp -d)
trap 'rm -rf "$frontend_dir"' EXIT
git -C "$frontend_dir" init -q
git -C "$frontend_dir" remote add origin https://github.com/berry-shake/komari-web.git
git -C "$frontend_dir" fetch --depth=1 origin "$frontend_ref"
git -C "$frontend_dir" checkout --detach FETCH_HEAD
[[ "$(git -C "$frontend_dir" rev-parse HEAD)" == "$frontend_ref" ]]
(cd "$frontend_dir" && npm ci && npm audit --audit-level=low && npm test && npm run build)
mkdir -p web/public/defaultTheme
rm -rf web/public/defaultTheme/dist
cp -R "$frontend_dir/dist" web/public/defaultTheme/dist
cp "$frontend_dir/komari-theme.json" "$frontend_dir/preview.png" web/public/defaultTheme/
