
## `s3select` tests
This directory serves as the location for Testing tests for s3select features.  Top level `testing.sh` calls `run.sh` to execute tests.

## Adding new tests
New tests are added into `s3select/tests.py` as new functions.

The runner uses `obstor.models.SelectObjectContentRequest` from the pinned Python SDK. `select_models.py` maps its nested serialization types and protocol enum values. SQL mismatches and service failures propagate so a failed suite cannot report PASS.

Run `python3 request_models_test.py` to check the installed SDK's request serialization and runner failure handling without a server. For source-only validation, use `python3 request_models_test.py --sdk-source /path/to/obstor-py`. The source mode loads real model modules while bypassing the package initializer's admin crypto imports, so it does not validate a complete installation.

## Running tests manually
- Set environment variables `TESTS_DATA_DIR`, `TESTS_MODE`, `SERVER_ENDPOINT`, `ACCESS_KEY`, `SECRET_KEY`, `SERVER_REGION` and `ENABLE_HTTPS`
- Call `run.sh` with output log file and error log file. for example

```bash
export TESTS_DATA_DIR=~/my-tests-dir
export TESTS_MODE=core
export SERVER_ENDPOINT="demo.obstor.net"
export ACCESS_KEY="Q3AM3UQ867SPQQA43P2F"
export SECRET_KEY="zuf+tfteSlswRu7BJ86wekitnifILbZam1KYY3TG"
export ENABLE_HTTPS=1
export SERVER_REGION=us-east-1
./run.sh /tmp/output.log /tmp/error.log
```
