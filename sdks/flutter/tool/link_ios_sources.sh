#!/usr/bin/env bash
# CocoaPods only picks up files inside the pod directory, so the plugin
# reaches the native iOS SDK through one symlink per Swift file in
# ios/MSeismoSources. Run this after adding, renaming or removing a file
# in sdks/ios/Sources/MSeismo.
set -euo pipefail

cd "$(dirname "$0")/../ios"
rm -rf MSeismoSources
mkdir MSeismoSources
for f in ../../ios/Sources/MSeismo/*.swift; do
  ln -s "../$f" "MSeismoSources/$(basename "$f")"
done
