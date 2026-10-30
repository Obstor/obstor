#!/bin/bash -e
#
#  Mint (C) 2017 Minio, Inc.
# PGG Obstor, (C) 2021-2026 PGG, Inc.
#
#  Licensed under the Apache License, Version 2.0 (the "License");
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an "AS IS" BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.
#

set -euo pipefail
build_dir="$(cd "$(dirname "$0")" && pwd)"
source "$build_dir/../sdk-source.sh"
test_run_dir="$TESTS_RUN_CORE_DIR/obstor-java"
source_dir="$(mktemp -d)"
trap 'rm -rf "$source_dir"' EXIT
checkout_sdk obstor-java "$source_dir/sdk" "${OBSTOR_JAVA_REF:-$(saved_sdk_ref "$build_dir")}"
python3 "$build_dir/prepare-source.py" "$source_dir/sdk"
mkdir -p "$test_run_dir"
cd "$source_dir/sdk"
# The SDK builds net.obstor classes
bash ./gradlew --no-daemon -I "$build_dir/stage-functional.gradle" \
    "-PobstorTestsOutput=$test_run_dir" :functional:stageObstorFunctionalTests
