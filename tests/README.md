# Testing

Testing is a testing framework for Obstor object storage server, available as a podman image. It runs correctness, benchmarking and stress tests. Following are the SDKs/tools used in correctness tests.

- awscli
- aws-sdk-go-v2
- aws-sdk-java-v2
- aws-sdk-php
- aws-sdk-ruby
- healthcheck
- obstor-go
- obstor-java
- obstor-js
- obstor-py
- s3cmd
- s3select
- versioning

## Running Testing

Testing is run by `podman run` command which requires Podman to be installed. For Podman installation follow the steps [here](https://podman.io/getting-started/installation#installing-on-linux).

To run Testing with Obstor server as test target,

```sh
$ podman run -e SERVER_ENDPOINT=<your-server>:9000 -e ACCESS_KEY=YOUR_ACCESS_KEY \
             -e SECRET_KEY=YOUR_SECRET_KEY -e ENABLE_HTTPS=1 obstor/obstor-testing
```

After the tests are run, output is stored in `/tests/log` directory inside the container. To get these logs, use `podman cp` command. For example
```sh
podman cp <container-id>:/tests/log /tmp/logs
```

### Testing environment variables

Below environment variables are required to be passed to the podman container. Supported environment variables:

| Environment variable   | Description                                                                                                                                    | Example                                    |
|:-----------------------|:-----------------------------------------------------------------------------------------------------------------------------------------------|:-------------------------------------------|
| `SERVER_ENDPOINT`      | Endpoint of Obstor server in the format `HOST:PORT`; for virtual style `IP:PORT`                                                               | `<your-server>:9000`                       |
| `ACCESS_KEY`           | Access key for `SERVER_ENDPOINT` credentials                                                                                                   | `YOUR_ACCESS_KEY`                          |
| `SECRET_KEY`           | Secret Key for `SERVER_ENDPOINT` credentials                                                                                                   | `YOUR_SECRET_KEY`                          |
| `ENABLE_HTTPS`         | (Optional) Set `1` to indicate to use HTTPS to access `SERVER_ENDPOINT`. Defaults to `0` (HTTP)                                                | `1`                                        |
| `TESTS_MODE`           | (Optional) Set mode indicating what category of tests to be run by values `core`, `full`. Defaults to `core`                                   | `full`                                     |
| `DOMAIN`               | (Optional) Value of OBSTOR_DOMAIN environment variable used in Obstor server                                                                    | `example.com`                              |
| `ENABLE_VIRTUAL_STYLE` | (Optional) Set `1` to indicate virtual style access . Defaults to `0` (Path style)                                                             | `1`                                        |
| `RUN_ON_FAIL`          | (Optional) Set `1` to indicate execute all tests independent of failures (currently implemented for obstor-go and obstor-java) . Defaults to `0` | `1`                                        |
| `SERVER_REGION`        | (Optional) Set custom region for region specific tests                                                                                         | `us-west-1`                                |

### Test virtual style access against Obstor server

To test Obstor server virtual style access with Testing, follow these steps:

- Set a domain in your Obstor server using environment variable OBSTOR_DOMAIN. For example `export OBSTOR_DOMAIN=example.com`.
- Start Obstor server.
- Execute Testing against Obstor server (with `OBSTOR_DOMAIN` set to `example.com`) using this command
```sh
$ podman run -e "SERVER_ENDPOINT=192.168.86.133:9000" -e "DOMAIN=obstor.net"  \
	     -e "ACCESS_KEY=obstor" -e "SECRET_KEY=obstor123" -e "ENABLE_HTTPS=0" \
	     -e "ENABLE_VIRTUAL_STYLE=1" obstor/obstor-testing
```

### Testing log format

All test logs are stored in `/tests/log/log.json` as multiple JSON document.  Below is the JSON format for every entry in the log file.

| JSON field | Type     | Description                                                   | Example                                               |
|:-----------|:---------|:--------------------------------------------------------------|:------------------------------------------------------|
| `name`     | _string_ | Testing tool/SDK name                                         | `"aws-sdk-php"`                                       |
| `function` | _string_ | Test function name                                            | `"getBucketLocation ( array $params = [] )"`          |
| `args`     | _object_ | (Optional) Key/Value map of arguments passed to test function | `{"Bucket":"aws-sdk-php-bucket-20341"}`               |
| `duration` | _int_    | Time taken in milliseconds to run the test                    | `384`                                                 |
| `status`   | _string_ | one of `PASS`, `FAIL` or `NA`                                 | `"PASS"`                                              |
| `alert`    | _string_ | (Optional) Alert message indicating test failure              | `"I/O error on create file"`                          |
| `message`  | _string_ | (Optional) Any log message                                    | `"validating checksum of downloaded object"`          |
| `error`    | _string_ | Detailed error message including stack trace on status `FAIL` | `"Error executing \"CompleteMultipartUpload\" on ...` |

## For Developers

### Running Testing development code

After making changes to Testing source code a local podman image can be built/run by

```sh
$ podman build -t obstor/obstor-testing . -f Dockerfile
$ podman run -e SERVER_ENDPOINT=<your-server>:9000 -e ACCESS_KEY=YOUR_ACCESS_KEY \
             -e SECRET_KEY=YOUR_SECRET_KEY \
             -e ENABLE_HTTPS=1 -e TESTS_MODE=full obstor/obstor-testing:latest
```


### Adding tests with new tool/SDK

Below are the steps need to be followed

- Create new app directory under [build](https://github.com/obstor/obstor/tree/main/tests/build) and [run/core](https://github.com/obstor/obstor/tree/main/tests/run/core) directories.
- Create `install.sh` which does installation of required tool/SDK under app directory.
- Any build and install time dependencies should be added to [install-packages.list](https://github.com/obstor/obstor/tree/main/tests/install-packages.list).
- Build time dependencies should be added to [remove-packages.list](https://github.com/obstor/obstor/tree/main/tests/remove-packages.list) for removal to have clean Testing podman image.
- Add `run.sh` in app directory under `run/core` which execute actual tests.

#### Test data
Tests may use pre-created data set to perform various object operations on Obstor server.  Below data files are available under `/tests/data` directory.

| File name        | Size    |
|:-----------------|:--------|
| datafile-0-b     | 0B      |
| datafile-1-b     | 1B      |
| datafile-1-kB    | 1KiB    |
| datafile-10-kB   | 10KiB   |
| datafile-33-kB   | 33KiB   |
| datafile-100-kB  | 100KiB  |
| datafile-1-MB    | 1MiB    |
| datafile-1.03-MB | 1.03MiB |
| datafile-5-MB    | 5MiB    |
| datafile-6-MB    | 6MiB    |
| datafile-10-MB   | 10MiB   |
| datafile-11-MB   | 11MiB   |
| datafile-65-MB   | 65MiB   |
| datafile-129-MB  | 129MiB  |

### Updating SDKs/binaries in the image

In many cases, updating the SDKs or binaries in the image is just a matter of making a commit updating the corresponding version in this repo. However, in some cases, e.g. when a client whose latest release is pulled in during each testing image build needs to be updated, a sort of "dummy" commit is required as an image rebuild must be triggered. Note that an empty commit does not appear to trigger the image rebuild in the Docker Hub.
