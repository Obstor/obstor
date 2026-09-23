#!/bin/bash -e
#
#  Minio Cloud Storage, (C) 2024 Minio, Inc.
#  PGG Obstor, (C) 2021-2026 PGG, Inc.
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

export TESTS_RUN_CORE_DIR="$TESTS_ROOT_DIR/run/core"
export TESTS_RUN_BUILD_DIR="$TESTS_ROOT_DIR/build"
export APT="apt --quiet -y"
export WGET="wget --quiet --no-check-certificate"
export WGET="wget --quiet --no-check-certificate"

## Software versions
export GO_VERSION="1.26.4"
export GRADLE_VERSION="9.2.0"
export GRADLE_INSTALL_PATH="/opt/gradle"
export GO_INSTALL_PATH="/usr/local"

export PATH=${GO_INSTALL_PATH}/bin:$PATH
export PATH=${GRADLE_INSTALL_PATH}/gradle-${GRADLE_VERSION}/bin:$PATH
