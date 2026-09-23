# Obstor NAS Backend

Obstor Backend adds S3 and [other supported protocol](/docs/protocols) compatibility to NAS storage. You may run multiple obstor instances on the same shared NAS volume as a distributed object backend.

## Run Obstor Backend for NAS Storage

### Using Docker

Please ensure to replace `/shared/nasvol` with actual mount path.

```bash
docker run -p 9000:9000 --name nas-s3 \
 -e "OBSTOR_ROOT_USER=obstor" \
 -e "OBSTOR_ROOT_PASSWORD=obstor123" \
 -v /shared/nasvol:/container/vol \
 ghcr.io/obstor/obstor backend nas /container/vol
```

### Using Binary

```bash
export OBSTOR_ROOT_USER=obstor
export OBSTOR_ROOT_PASSWORD=obstor123
obstor backend nas /shared/nasvol
```

## Test using Browser Dashboard

Obstor Backend comes with an embedded web based object browser. Point your web browser to http://127.0.0.1:9000 to ensure that your server has started successfully.

![Screenshot](../screenshots/dashboard.svg)

## Test using an S3 client

You can interact with the backend using rclone or the AWS CLI. Both support filesystems and S3-compatible cloud storage services.

### Configure your client

Configure an rclone S3 remote once:

```bash
rclone config create obstor s3 provider=Other endpoint=http://backend-ip:9000 access_key_id=access_key secret_access_key=secret_key
```

### List buckets on nas

```bash
rclone lsd obstor:
[2026-05-22 01:50:43]     0B ferenginar/
[2026-05-26 21:43:51]     0B my-bucket/
[2026-05-26 22:10:11]     0B test-bucket1/
```

Or with the AWS CLI:

```bash
aws --endpoint-url http://backend-ip:9000 s3 ls
```

### The file-based config settings are deprecated in NAS

The support for admin config APIs will be removed. This will include the getters and setters that previously edited config on disk, and any other on-disk config options. The reason for this change is to avoid un-necessary reloads of the config from the disk. And to comply with the Environment variable based settings like other backends.

### Migration guide

The users who have been using the older config approach should migrate to ENV settings by setting environment variables accordingly.

For example,

Consider the following webhook target config.

```bash
notify_webhook:1 endpoint=http://localhost:8080/ auth_token= queue_limit=0 queue_dir=/tmp/webhk client_cert= client_key=
```

The corresponding environment variable setting can be

```bash
export OBSTOR_NOTIFY_WEBHOOK_ENABLE_1=on
export OBSTOR_NOTIFY_WEBHOOK_ENDPOINT_1=http://localhost:8080/
export OBSTOR_NOTIFY_WEBHOOK_QUEUE_DIR_1=/tmp/webhk
```

> NOTE: Please check the docs for the corresponding ENV setting. Environment variables are mapped as `OBSTOR_*`.

## Symlink support

NAS backend implementation allows symlinks on regular files,

### Behavior

- For reads symlink resolves to file symlink points to.
- For deletes
  - Delete of symlink deletes the symlink but not the real file to which the symlink points.
  - Delete of actual file automatically makes symlink'ed file invisible, dangling symlinks won't be visible.

#### Caveats
- Disallows follow of directory symlinks to avoid security issues, and leaving them as is on namespace makes them very inconsistent.
- Dangling symlinks are ignored automatically.

*Directory symlinks is not and will not be supported as there are no safe ways to handle them.*

## Explore Further
- [Supported Protocols](/docs/protocols) - S3, SFTP, and more
- `rclone` command-line interface
- `aws` command-line interface
- `obstor-go` Go SDK
