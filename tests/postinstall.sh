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

export APT="apt --quiet -y"

# remove all packages listed in remove-packages.list
xargs --arg-file="${TESTS_ROOT_DIR}/remove-packages.list" apt --quiet -y purge
${APT} autoremove

# flush to disk
sync
