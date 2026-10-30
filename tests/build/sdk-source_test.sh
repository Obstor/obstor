#!/bin/bash
# Real, offline checkout/install regression
set -euo pipefail
build_dir="$(cd "$(dirname "$0")" && pwd)"
source "$build_dir/sdk-source.sh"
test_dir="$(mktemp -d)"
trap 'rm -rf "$test_dir"' EXIT
export OBSTOR_SDK_SOURCE_ROOT="$test_dir/source"
mkdir -p "$OBSTOR_SDK_SOURCE_ROOT/obstor-js/tests/functional"
git init --quiet "$OBSTOR_SDK_SOURCE_ROOT/obstor-js"
printf 'committed functional test\n' > "$OBSTOR_SDK_SOURCE_ROOT/obstor-js/tests/functional/functional-tests.js"
git -C "$OBSTOR_SDK_SOURCE_ROOT/obstor-js" add .
git -C "$OBSTOR_SDK_SOURCE_ROOT/obstor-js" -c commit.gpgsign=false -c user.name=Test -c user.email=test@example.invalid commit --quiet -m fixture
export OBSTOR_JS_REF="$(git -C "$OBSTOR_SDK_SOURCE_ROOT/obstor-js" rev-parse HEAD)"
printf 'uncommitted edit\n' > "$OBSTOR_SDK_SOURCE_ROOT/obstor-js/tests/functional/functional-tests.js"
export TESTS_ROOT_DIR="$test_dir/tests"
bash "$build_dir/obstor-js/install.sh"
cmp <(printf 'committed functional test\n') "$TESTS_ROOT_DIR/test-run/obstor-js/tests/functional/functional-tests.js"
if checkout_sdk obstor-js "$TESTS_ROOT_DIR/test-run/obstor-js" "$OBSTOR_JS_REF" 2>/dev/null; then
    echo 'existing SDK destination unexpectedly accepted' >&2
    exit 1
fi
if checkout_sdk obstor-js "$test_dir/invalid" '--upload-pack=bad' 2>/dev/null; then
    echo 'invalid SDK ref unexpectedly accepted' >&2
    exit 1
fi
for sdk in obstor-js obstor-py obstor-java; do
    saved_sdk_ref "$build_dir/$sdk" >/dev/null
done
if saved_sdk_ref "$test_dir" 2>/dev/null; then
    echo 'missing saved ref unexpectedly accepted' >&2
    exit 1
fi
echo 'SDK source checkout and JS installer regression passed'
