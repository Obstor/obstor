#!/bin/bash -e
#
#  Minio Cloud Storage, (C) 2017 Minio, Inc.
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

# handle command line arguments
if [ $# -ne 2 ]; then
	echo "usage: run.sh <OUTPUT-LOG-FILE> <ERROR-LOG-FILE>"
	exit 1
fi

output_log_file="$1"
error_log_file="$2"

# Prepare test runner project
cp -R /tests/test-run/obstor-js/ ./obstor-js
pnpm install --frozen-lockfile

# Run tests
node ./node_modules/mocha/bin/mocha.js "./obstor-js/tests/functional/functional-tests.js" -R obstorreporter >>"$output_log_file" 2>"$error_log_file"
