FROM golang:1.26-alpine AS go-builder

ENV GOPATH=/go
ENV CGO_ENABLED=0
ENV GO111MODULE=on

# Dependencies
RUN apk add --no-cache git

# Cache dependencies
WORKDIR /go/obstor
COPY go.mod go.sum ./
RUN go mod download

ARG VERSION=dev
ARG COMMIT=unknown

# Build source
COPY . .
RUN SHORT_COMMIT="$(printf '%.12s' "$COMMIT")" && \
    go build -trimpath -ldflags "-s -w \
    -X github.com/obstor/obstor/cmd.Version=${VERSION} \
    -X github.com/obstor/obstor/cmd.ReleaseTag=${VERSION} \
    -X github.com/obstor/obstor/cmd.CommitID=${COMMIT} \
    -X github.com/obstor/obstor/cmd.ShortCommitID=${SHORT_COMMIT}" -o /go/bin/obstor .

FROM node:26-alpine AS node-builder

ENV NEXT_TELEMETRY_DISABLED=1

RUN npm install -g pnpm

WORKDIR /app
COPY browser/package.json browser/pnpm-lock.yaml browser/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

COPY browser/ .
RUN pnpm build

FROM alpine:3.23

LABEL maintainer="PGG Inc <oss@pgg.net>"

RUN apk add --no-cache curl ca-certificates su-exec nodejs

COPY --from=go-builder /go/bin/obstor /usr/bin/obstor
COPY --from=go-builder /go/obstor/CREDITS /licenses/CREDITS
COPY --from=go-builder /go/obstor/LICENSE /licenses/LICENSE
COPY --from=go-builder /go/obstor/dockerscripts/docker-entrypoint.sh /usr/bin/
RUN chmod +x /usr/bin/docker-entrypoint.sh

COPY --from=node-builder /app/.next/standalone /opt/frontend
COPY --from=node-builder /app/.next/static /opt/frontend/.next/static

ENV OBSTOR_ACCESS_KEY_FILE=access_key \
    OBSTOR_SECRET_KEY_FILE=secret_key \
    OBSTOR_ROOT_USER_FILE=access_key \
    OBSTOR_ROOT_PASSWORD_FILE=secret_key \
    OBSTOR_KMS_SECRET_KEY_FILE=kms_master_key \
    OBSTOR_UPDATE_MINISIGN_PUBKEY="RWTx5Zr1tiHQLwG9keckT0c45M3AGeHD6IvimQHpyRywVWGbP1aVSGav"

EXPOSE 9000 9001

ENTRYPOINT ["/usr/bin/docker-entrypoint.sh"]

VOLUME ["/data"]

CMD ["obstor", "server", "/data"]
