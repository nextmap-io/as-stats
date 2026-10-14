#!/bin/sh
# Usage: scripts/demo/shoot.sh <outdir> <shots.json>
# Runs headless Chromium on the demo network and writes <name>-<theme>.png.
set -eu
DIR=$(cd "$(dirname "$0")" && pwd)
OUT=$(cd "$1" && pwd)
SHOTS=$(cd "$(dirname "$2")" && pwd)/$(basename "$2")
docker run --rm --shm-size=1g --network asdemo -u "$(id -u):$(id -g)" \
  -v "$DIR/shot.js":/usr/src/app/s.js:ro -v "$SHOTS":/shots.json:ro -v "$OUT":/out \
  -w /usr/src/app zenika/alpine-chrome:with-puppeteer node s.js /out /shots.json
