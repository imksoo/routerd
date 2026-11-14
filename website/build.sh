#!/bin/sh
set -eu
cd "$(dirname "$0")"
rm -rf build
mkdir -p build/docs build/examples
cp -R static/. build/
cp -R ../docs/. build/docs/
cp -R ../examples/. build/examples/
for locale in ja zh-Hant zh-Hans; do
  mkdir -p "build/$locale/docs"
  cp -R "i18n/$locale/docusaurus-plugin-content-docs/current/." "build/$locale/docs/"
done
cp index.html build/index.html
