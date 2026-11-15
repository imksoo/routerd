#!/bin/sh
set -eu
cd "$(dirname "$0")"
rm -rf build
mkdir -p build/docs build/examples
cp -R static/. build/
cp -R ../docs/. build/docs/
cp -R ../examples/. build/examples/
cp index.html build/index.html
python3 -c 'import markdown' 2>/dev/null || python3 -m pip install --quiet Markdown==3.11.3
python3 ../scripts/render-docs.py build/docs
