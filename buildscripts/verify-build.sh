#!/bin/bash
#
# MinIO Cloud Storage, (C) 2017, 2018 MinIO, Inc.
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

export TESTS_MODE=core
export TESTS_DATA_DIR="$WORK_DIR/data"
export SERVER_ENDPOINT="127.0.0.1:9000"
export ACCESS_KEY="obstor"
export SECRET_KEY="obstor123"
export ENABLE_HTTPS=0
export GO111MODULE=on
export GOGC=25

OBSTOR=( "$PWD/obstor" )

FILE_1_MB="$TESTS_DATA_DIR/datafile-1-MB"
FILE_65_MB="$TESTS_DATA_DIR/datafile-65-MB"

FUNCTIONAL_TESTS="$WORK_DIR/obstor-go-functest"

function start_obstor_fs()
{
  "${OBSTOR[@]}" server "${WORK_DIR}/fs-disk" >"$WORK_DIR/fs-obstor.log" 2>&1 &
  sleep 10
}

function start_obstor_erasure()
{
  "${OBSTOR[@]}" server "${WORK_DIR}/erasure-disk1" "${WORK_DIR}/erasure-disk2" "${WORK_DIR}/erasure-disk3" "${WORK_DIR}/erasure-disk4" >"$WORK_DIR/erasure-obstor.log" 2>&1 &
  sleep 15
}

function start_obstor_erasure_sets()
{
  export OBSTOR_ENDPOINTS="${WORK_DIR}/erasure-disk-sets{1...32}"
  "${OBSTOR[@]}" server > "$WORK_DIR/erasure-obstor-sets.log" 2>&1 &
  sleep 15
}

function start_obstor_pool_erasure_sets()
{
  export OBSTOR_ROOT_USER=$ACCESS_KEY
  export OBSTOR_ROOT_PASSWORD=$SECRET_KEY
  export OBSTOR_ENDPOINTS="http://127.0.0.1:9000${WORK_DIR}/pool-disk-sets{1...4} http://127.0.0.1:9001${WORK_DIR}/pool-disk-sets{5...8}"
  "${OBSTOR[@]}" server --web-address ":9000" > "$WORK_DIR/pool-obstor-9000.log" 2>&1 &
  "${OBSTOR[@]}" server --web-address ":9001" > "$WORK_DIR/pool-obstor-9001.log" 2>&1 &

  sleep 40
}

function start_obstor_pool_erasure_sets_ipv6()
{
  export OBSTOR_ROOT_USER=$ACCESS_KEY
  export OBSTOR_ROOT_PASSWORD=$SECRET_KEY
  export OBSTOR_ENDPOINTS="http://[::1]:9000${WORK_DIR}/pool-disk-sets{1...4} http://[::1]:9001${WORK_DIR}/pool-disk-sets{5...8}"
  "${OBSTOR[@]}" server --web-address="[::1]:9000" > "$WORK_DIR/pool-obstor-ipv6-9000.log" 2>&1 &
  "${OBSTOR[@]}" server --web-address="[::1]:9001" > "$WORK_DIR/pool-obstor-ipv6-9001.log" 2>&1 &

  sleep 40
}

function start_obstor_dist_erasure()
{
  export OBSTOR_ROOT_USER=$ACCESS_KEY
  export OBSTOR_ROOT_PASSWORD=$SECRET_KEY
  export OBSTOR_ENDPOINTS="http://127.0.0.1:9000${WORK_DIR}/dist-disk1 http://127.0.0.1:9001${WORK_DIR}/dist-disk2 http://127.0.0.1:9002${WORK_DIR}/dist-disk3 http://127.0.0.1:9003${WORK_DIR}/dist-disk4"
  for i in $(seq 0 3); do
    "${OBSTOR[@]}" server --web-address ":900${i}" > "$WORK_DIR/dist-obstor-900${i}.log" 2>&1 &
  done

  sleep 40
}

function run_test_fs()
{
  start_obstor_fs

  (cd "$WORK_DIR" && "$FUNCTIONAL_TESTS")
  rv=$?

  pkill obstor
  sleep 3

  if [ "$rv" -ne 0 ]; then
    cat "$WORK_DIR/fs-obstor.log"
  fi
  rm -f "$WORK_DIR/fs-obstor.log"

  return "$rv"
}

function run_test_erasure_sets()
{
  start_obstor_erasure_sets

  (cd "$WORK_DIR" && "$FUNCTIONAL_TESTS")
  rv=$?

  pkill obstor
  sleep 3

  if [ "$rv" -ne 0 ]; then
    cat "$WORK_DIR/erasure-obstor-sets.log"
  fi
  rm -f "$WORK_DIR/erasure-obstor-sets.log"

  return "$rv"
}

function run_test_pool_erasure_sets()
{
  start_obstor_pool_erasure_sets

  (cd "$WORK_DIR" && "$FUNCTIONAL_TESTS")
  rv=$?

  pkill obstor
  sleep 3

  if [ "$rv" -ne 0 ]; then
    for i in $(seq 0 1); do
      echo "server$i log:"
      cat "$WORK_DIR/pool-obstor-900$i.log"
    done
  fi

  for i in $(seq 0 1); do
    rm -f "$WORK_DIR/pool-obstor-900$i.log"
  done

  return "$rv"
}

function run_test_pool_erasure_sets_ipv6()
{
  start_obstor_pool_erasure_sets_ipv6

  export SERVER_ENDPOINT="[::1]:9000"

  (cd "$WORK_DIR" && "$FUNCTIONAL_TESTS")
  rv=$?

  pkill obstor
  sleep 3

  if [ "$rv" -ne 0 ]; then
    for i in $(seq 0 1); do
      echo "server$i log:"
      cat "$WORK_DIR/pool-obstor-ipv6-900$i.log"
    done
  fi

  for i in $(seq 0 1); do
    rm -f "$WORK_DIR/pool-obstor-ipv6-900$i.log"
  done

  return "$rv"
}

function run_test_erasure()
{
  start_obstor_erasure

  (cd "$WORK_DIR" && "$FUNCTIONAL_TESTS")
  rv=$?

  pkill obstor
  sleep 3

  if [ "$rv" -ne 0 ]; then
    cat "$WORK_DIR/erasure-obstor.log"
  fi
  rm -f "$WORK_DIR/erasure-obstor.log"

  return "$rv"
}

function run_test_dist_erasure()
{
  start_obstor_dist_erasure

  (cd "$WORK_DIR" && "$FUNCTIONAL_TESTS")
  rv=$?

  pkill obstor
  sleep 3

  if [ "$rv" -ne 0 ]; then
    echo "server1 log:"
    cat "$WORK_DIR/dist-obstor-9000.log"
    echo "server2 log:"
    cat "$WORK_DIR/dist-obstor-9001.log"
    echo "server3 log:"
    cat "$WORK_DIR/dist-obstor-9002.log"
    echo "server4 log:"
    cat "$WORK_DIR/dist-obstor-9003.log"
  fi

  rm -f "$WORK_DIR/dist-obstor-9000.log" "$WORK_DIR/dist-obstor-9001.log" "$WORK_DIR/dist-obstor-9002.log" "$WORK_DIR/dist-obstor-9003.log"

  return "$rv"
}

function purge()
{
  rm -rf "$1"
}

function __init__()
{
  echo "Initializing environment"
  mkdir -p "$WORK_DIR"
  mkdir -p "$TESTS_DATA_DIR"

  OBSTOR_GO_VERSION=$(curl --retry 10 -Ls -o /dev/null -w "%{url_effective}" https://github.com/obstor/obstor-go/releases/latest | sed "s/https:\/\/github.com\/obstor\/obstor-go\/releases\/tag\///")
  if [ -z "$OBSTOR_GO_VERSION" ]; then
    echo "unable to get obstor-go version from github"
    exit 1
  fi
  GO_TEST_DIR="obstor-go-functest-$RANDOM"
  mkdir -p "$GO_TEST_DIR"
  if ! curl -sL -o "$GO_TEST_DIR/main.go" "https://raw.githubusercontent.com/obstor/obstor-go/${OBSTOR_GO_VERSION}/functional_tests.go"; then
    echo "failed to download obstor-go functional_tests.go"
    purge "${GO_TEST_DIR}"
    exit 1
  fi
  (cd "$GO_TEST_DIR" && go mod init obstor-go-functest && go mod tidy && CGO_ENABLED=0 go build -o "$FUNCTIONAL_TESTS" main.go)
  purge "${GO_TEST_DIR}"

  shred -n 1 -s 1M - 1>"$FILE_1_MB" 2>/dev/null
  shred -n 1 -s 65M - 1>"$FILE_65_MB" 2>/dev/null
}

function main()
{
  echo "Testing in FS setup"
  if ! run_test_fs; then
    echo "FAILED"
    purge "$WORK_DIR"
    exit 1
  fi

  echo "Testing in Erasure setup"
  if ! run_test_erasure; then
    echo "FAILED"
    purge "$WORK_DIR"
    exit 1
  fi

  echo "Testing in Distributed Erasure setup"
  if ! run_test_dist_erasure; then
    echo "FAILED"
    purge "$WORK_DIR"
    exit 1
  fi

  echo "Testing in Erasure setup as sets"
  if ! run_test_erasure_sets; then
    echo "FAILED"
    purge "$WORK_DIR"
    exit 1
  fi

  echo "Testing in Distributed Eraure expanded setup"
  if ! run_test_pool_erasure_sets; then
    echo "FAILED"
    purge "$WORK_DIR"
    exit 1
  fi

  echo "Testing in Distributed Erasure expanded setup with ipv6"
  if ! run_test_pool_erasure_sets_ipv6; then
    echo "FAILED"
    purge "$WORK_DIR"
    exit 1
  fi

  purge "$WORK_DIR"
}

( __init__ "$@" && main "$@" )
rv=$?
purge "$WORK_DIR"
exit "$rv"
