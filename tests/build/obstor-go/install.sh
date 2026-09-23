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

OBSTOR_GO_VERSION=$(curl --retry 10 -Ls -o /dev/null -w "%{url_effective}" https://github.com/obstor/obstor-go/releases/latest | sed "s/https:\/\/github.com\/obstor\/obstor-go\/releases\/tag\///")
if [ -z "$OBSTOR_GO_VERSION" ]; then
	echo "unable to get obstor-go version from github"
	exit 1
fi

test_run_dir="$TESTS_RUN_CORE_DIR/obstor-go"
curl -sL -o "${test_run_dir}/main.go" "https://raw.githubusercontent.com/obstor/obstor-go/${OBSTOR_GO_VERSION}/functional_tests.go"

# Extract only the function from versioning_test.go (skip package, imports, comments)
# Start from line 34 where the function definition begins
tail -n +34 "${test_run_dir}/versioning_test.go" >>"${test_run_dir}/main.go"

# Patch functional_tests.go to call our versioning test
# Add testBucketVersioningExcludedPrefixes() call after testStatObjectWithVersioning()
sed -i.bak '/testStatObjectWithVersioning()/a\
		testBucketVersioningExcludedPrefixes()
' "${test_run_dir}/main.go"

# Build the combined file
(cd "$test_run_dir" && go mod tidy -compat=1.21 && CGO_ENABLED=0 go build --ldflags "-s -w" -o obstor-go main.go)
