#!/bin/bash
#
# MinIO Cloud Storage, (C) 2020 MinIO, Inc.
# PGG Obstor, (C) 2021-2026 PGG, Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

set -e
set -E
set -o pipefail

if [ ! -x "$PWD/obstor" ]; then
  echo "obstor executable binary not found in current directory"
  exit 1
fi

WORK_DIR="$PWD/.verify-$RANDOM"
OBSTOR=( "$PWD/obstor" server )

export GOGC=25

function start_obstor_3_node() {
  export OBSTOR_ROOT_USER=obstor
  export OBSTOR_ROOT_PASSWORD=obstor123
  export OBSTOR_ERASURE_SET_DRIVE_COUNT=6

  start_port=$(shuf -i 10000-65000 -n 1)
  args=""
  for i in $(seq 1 3); do
    args="$args http://127.0.0.1:$[$start_port+$i]${WORK_DIR}/$i/1/ http://127.0.0.1:$[$start_port+$i]${WORK_DIR}/$i/2/ http://127.0.0.1:$[$start_port+$i]${WORK_DIR}/$i/3/ http://127.0.0.1:$[$start_port+$i]${WORK_DIR}/$i/4/ http://127.0.0.1:$[$start_port+$i]${WORK_DIR}/$i/5/ http://127.0.0.1:$[$start_port+$i]${WORK_DIR}/$i/6/"
  done

  "${OBSTOR[@]}" --web-address ":$[$start_port+1]" $args > "${WORK_DIR}/dist-obstor-server1.log" 2>&1 &
  disown $!

  "${OBSTOR[@]}" --web-address ":$[$start_port+2]" $args > "${WORK_DIR}/dist-obstor-server2.log" 2>&1 &
  disown $!

  "${OBSTOR[@]}" --web-address ":$[$start_port+3]" $args > "${WORK_DIR}/dist-obstor-server3.log" 2>&1 &
  disown $!

  sleep "$1"
  if [ "$(pgrep -c obstor)" -ne 3 ]; then
    for i in $(seq 1 3); do
      echo "server$i log:"
      cat "${WORK_DIR}/dist-obstor-server$i.log"
    done
    echo "FAILED"
    purge "$WORK_DIR"
    exit 1
  fi
  if ! pkill obstor; then
    for i in $(seq 1 3); do
      echo "server$i log:"
      cat "${WORK_DIR}/dist-obstor-server$i.log"
    done
    echo "FAILED"
    purge "$WORK_DIR"
    exit 1
  fi

  sleep 1;
  if pgrep obstor; then
    # forcibly killing, to proceed further properly.
    if ! pkill -9 obstor; then
      echo "no obstor process running anymore, proceed."
    fi
  fi
}


function check_online() {
  if grep -q 'Server switching to safe mode' ${WORK_DIR}/dist-obstor-*.log; then
    echo "1"
  fi
}

function purge()
{
  rm -rf "$1"
}

function __init__()
{
  echo "Initializing environment"
  mkdir -p "$WORK_DIR"
}

function perform_test() {
  start_obstor_3_node 60

  echo "Testing Distributed Erasure setup healing of drives"
  echo "Remove the contents of the disks belonging to '${1}' erasure set"

  rm -rf ${WORK_DIR}/${1}/*/

  start_obstor_3_node 60

  rv=$(check_online)
  if [ "$rv" == "1" ]; then
    pkill -9 obstor
    for i in $(seq 1 3); do
      echo "server$i log:"
      cat "${WORK_DIR}/dist-obstor-server$i.log"
    done
    echo "FAILED"
    purge "$WORK_DIR"
    exit 1
  fi
}

function main()
{
  perform_test "2"
  perform_test "1"
  perform_test "3"
}

( __init__ "$@" && main "$@" )
rv=$?
purge "$WORK_DIR"
exit "$rv"
