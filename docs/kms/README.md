# KMS Guide

Obstor uses a key-management-system (KMS) to support SSE-S3. If a client requests SSE-S3, or auto-encryption is enabled, the Obstor server encrypts each object with an unique object key which is protected by a master key managed by the KMS.

## Auto Encryption
Auto-Encryption is useful when Obstor administrator wants to ensure that all data stored on Obstor is encrypted at rest.

### Using bucket encryption (recommended)
Obstor automatically encrypts all objects on buckets if KMS is successfully configured and bucket encryption configuration is enabled for each bucket as shown below:
```bash
aws --endpoint-url http://HOST:9000 s3api put-bucket-encryption \
  --bucket bucket \
  --server-side-encryption-configuration '{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"}}]}'
```

Verify if Obstor has `sse-s3` enabled
```bash
aws --endpoint-url http://HOST:9000 s3api get-bucket-encryption --bucket bucket
```

### Using environment (deprecated)
> NOTE: The following ENV might be removed in future, you are advised to move to the previously recommended approach using bucket encryption. S3 backend supports encryption at backend layer which may  be dropped in favor of simplicity at a later time. It is advised that S3 backend users migrate to Obstor server mode or enable encryption at REST at the backend.

Obstor automatically encrypts all objects on buckets if KMS is successfully configured and following ENV is enabled:
```bash
export OBSTOR_KMS_AUTO_ENCRYPTION=on
```

### Verify auto-encryption
> Note that auto-encryption only affects requests without S3 encryption headers. So, if a S3 client sends
> e.g. SSE-C headers, Obstor will encrypt the object with the key sent by the client and won't reach out to
> the configured KMS.

To verify auto-encryption, upload an object and inspect its metadata:

```bash
rclone copy test.file obstor:bucket/
```

```bash
aws --endpoint-url http://HOST:9000 s3api head-object --bucket bucket --key test.file
{
    "ServerSideEncryption": "AES256",
    ...
}
```

## Explore Further

- Use `rclone` with Obstor Server
- Use `aws-cli` with Obstor Server
- Use `s3cmd` with Obstor Server
- [The Obstor documentation website](/docs)
