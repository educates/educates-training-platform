#!/bin/bash

set -x

set -eo pipefail

rm -rf build

tsc

esbuild build/frontend/scripts/educates.js --bundle --format=iife --global-name=educates --outfile=build/frontend/scripts/educates-bundle.js
esbuild build/frontend/scripts/educates.js --bundle --format=iife --global-name=educates --minify --outfile=build/frontend/scripts/educates-bundle.min.js
