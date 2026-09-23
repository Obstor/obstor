#!/bin/bash
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

set -e

OBSTOR_DOTNET_SDK_PATH="$TESTS_RUN_CORE_DIR/obstor-dotnet"

OBSTOR_DOTNET_SDK_VERSION=$(curl --retry 10 -Ls -o /dev/null -w "%{url_effective}" https://github.com/obstor/obstor-dotnet/releases/latest | sed "s/https:\/\/github.com\/obstor\/obstor-dotnet\/releases\/tag\///")
if [ -z "$OBSTOR_DOTNET_SDK_VERSION" ]; then
	echo "unable to get obstor-dotnet version from github"
	exit 1
fi

out_dir="$OBSTOR_DOTNET_SDK_PATH/out"
if [ -z "$out_dir" ]; then
	mkdir "$out_dir"
fi

temp_dir="$OBSTOR_DOTNET_SDK_PATH/temp"
git clone --quiet https://github.com/obstor/obstor-dotnet.git "${temp_dir}"
pushd "${temp_dir}" >/dev/null
git checkout --quiet "tags/${OBSTOR_DOTNET_SDK_VERSION}"

dotnet publish Obstor.IntegrationTests --configuration Tests --framework net8.0 --output ../out

popd >/dev/null
rm -fr "${temp_dir}"
