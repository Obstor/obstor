#!/bin/bash -e
#
#  Mint (C) 2017-2020 Minio, Inc.
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
source_dir="$(mktemp -d)"
trap 'rm -rf "$source_dir"' EXIT
checkout_sdk obstor-py "$source_dir/sdk" "${OBSTOR_PY_REF:-$(saved_sdk_ref "$build_dir")}"
python3 -m pip install --break-system-packages --no-cache-dir \
    'faker==37.1.0' "$source_dir/sdk"
cp "$source_dir/sdk/tests/functional/tests.py" "$TESTS_RUN_CORE_DIR/obstor-py/tests.py"
python3 -c 'import obstor, faker'
