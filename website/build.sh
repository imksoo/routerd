#!/bin/sh
set -eu
cd "$(dirname "$0")"
rm -rf build
mkdir -p build/docs build/examples
cp -R static/. build/
cp -R ../docs/. build/docs/
cp -R ../examples/. build/examples/
cp index.html build/index.html
