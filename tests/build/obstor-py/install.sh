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

OBSTOR_PY_VERSION="7.2.20"
test_run_dir="$TESTS_RUN_CORE_DIR/obstor-py"

pip3 install --break-system-packages --user faker
pip3 install --break-system-packages --no-cache-dir obstor=="${OBSTOR_PY_VERSION}"
$WGET --output-document="$test_run_dir/tests.py" "https://raw.githubusercontent.com/obstor/obstor-py/${OBSTOR_PY_VERSION}/tests/functional/tests.py"
