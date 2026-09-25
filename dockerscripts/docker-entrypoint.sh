#!/bin/sh
#
# MinIO Cloud Storage, (C) 2019 MinIO, Inc.
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

# If command starts with an option, prepend obstor.
if [ "${1}" != "obstor" ]; then
    if [ -n "${1}" ]; then
        set -- obstor "$@"
    fi
fi

docker_secrets_env() {
    if [ -f "$OBSTOR_ROOT_USER_FILE" ]; then
        ROOT_USER_FILE="$OBSTOR_ROOT_USER_FILE"
    else
        ROOT_USER_FILE="/run/secrets/$OBSTOR_ROOT_USER_FILE"
    fi
    if [ -f "$OBSTOR_ROOT_PASSWORD_FILE" ]; then
        SECRET_KEY_FILE="$OBSTOR_ROOT_PASSWORD_FILE"
    else
        SECRET_KEY_FILE="/run/secrets/$OBSTOR_ROOT_PASSWORD_FILE"
    fi

    if [ -f "$ROOT_USER_FILE" ] && [ -f "$SECRET_KEY_FILE" ]; then
        if [ -f "$ROOT_USER_FILE" ]; then
            OBSTOR_ROOT_USER="$(cat "$ROOT_USER_FILE")"
            export OBSTOR_ROOT_USER
        fi
        if [ -f "$SECRET_KEY_FILE" ]; then
            OBSTOR_ROOT_PASSWORD="$(cat "$SECRET_KEY_FILE")"
            export OBSTOR_ROOT_PASSWORD
        fi
    fi
}

## Set KMS_MASTER_KEY from docker secrets if provided
docker_kms_encryption_env() {
    if [ -f "$OBSTOR_KMS_SECRET_KEY_FILE" ]; then
        KMS_SECRET_KEY_FILE="$OBSTOR_KMS_SECRET_KEY_FILE"
    else
        KMS_SECRET_KEY_FILE="/run/secrets/$OBSTOR_KMS_SECRET_KEY_FILE"
    fi

    if [ -f "$KMS_SECRET_KEY_FILE" ]; then
        OBSTOR_KMS_SECRET_KEY="$(cat "$KMS_SECRET_KEY_FILE")"
        export OBSTOR_KMS_SECRET_KEY
    fi
}

# su-exec to requested user, if service cannot run exec will fail.
docker_switch_user() {
    if [ -n "${OBSTOR_USERNAME}" ] && [ -n "${OBSTOR_GROUPNAME}" ]; then
        if [ -n "${OBSTOR_UID}" ] && [ -n "${OBSTOR_GID}" ]; then
            groupadd -g "$OBSTOR_GID" "$OBSTOR_GROUPNAME" && \
                useradd -u "$OBSTOR_UID" -g "$OBSTOR_GROUPNAME" "$OBSTOR_USERNAME"
        else
            groupadd "$OBSTOR_GROUPNAME" && \
                useradd -g "$OBSTOR_GROUPNAME" "$OBSTOR_USERNAME"
        fi
        exec setpriv --reuid="${OBSTOR_USERNAME}" --regid="${OBSTOR_GROUPNAME}" --keep-groups "$@"
    else
        exec "$@"
    fi
}

# Start frontend if enabled
start_frontend() {
    case "${OBSTOR_BROWSER}" in false|FALSE|0) return ;; esac
    if [ -f /opt/frontend/server.js ]; then
        API_PORT=9000
        CERTS_DIR=""
        prev=""
        for arg in "$@"; do
            case "$arg" in
                --s3-address=*) API_PORT="${arg#--s3-address=}" ;;
                --certs-dir=*) CERTS_DIR="${arg#--certs-dir=}" ;;
            esac
            case "$prev" in
                --s3-address) API_PORT="$arg" ;;
                --certs-dir) CERTS_DIR="$arg" ;;
            esac
            prev="$arg"
        done
        case "$API_PORT" in *:*) API_PORT="${API_PORT##*:}" ;; esac

        # Enable TLS if certs exist
        PROTO="http"
        CA_CERT=""
        if [ -n "$CERTS_DIR" ] && [ -f "$CERTS_DIR/public.crt" ]; then
            PROTO="https"
            CA_CERT="$CERTS_DIR/public.crt"
        elif [ -f /etc/obstor/certs/public.crt ]; then
            PROTO="https"
            CA_CERT="/etc/obstor/certs/public.crt"
        fi

        # OBSTOR_ENDPOINT is internal RPC
        # OBSTOR_HOST is external presigned URLs
        EXTERNAL_HOST=$(hostname)
        if [ "$API_PORT" = "443" ] && [ "$PROTO" = "https" ]; then
            DEFAULT_HOST="$EXTERNAL_HOST"
        else
            DEFAULT_HOST="$EXTERNAL_HOST:$API_PORT"
        fi

        if [ "$PROTO" = "https" ]; then
            DEFAULT_ENDPOINT="https://${EXTERNAL_HOST}:${API_PORT}"
        else
            DEFAULT_ENDPOINT="http://127.0.0.1:${API_PORT}"
        fi

        env PORT=3000 \
        HOSTNAME=127.0.0.1 \
        ${CA_CERT:+"NODE_EXTRA_CA_CERTS=$CA_CERT"} \
        "OBSTOR_ENDPOINT=${OBSTOR_ENDPOINT:-${DEFAULT_ENDPOINT}}" \
        "OBSTOR_HOST=${OBSTOR_HOST:-${DEFAULT_HOST}}" \
        node /opt/frontend/server.js > /dev/null &
    fi
}

## Load secrets
docker_secrets_env
docker_kms_encryption_env

## Start frontend
start_frontend "$@"

## Switch to user and exec obstor
docker_switch_user "$@"
