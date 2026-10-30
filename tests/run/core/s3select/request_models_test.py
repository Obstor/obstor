"""Offline request serialization and runner failure regressions.

Normally imports the installed SDK. --sdk-source loads its real model modules
without the package initializer's unrelated admin crypto dependencies; it does
not substitute request models or verify a complete SDK installation.
"""

import argparse
import ast
import importlib.util
from pathlib import Path
import sys
from types import ModuleType, SimpleNamespace
import unittest


def use_source_models(source):
    package_dir = Path(source).resolve() / "obstor"
    package = ModuleType("obstor")
    package.__path__ = [str(package_dir)]
    declarations = ast.parse((package_dir / "__init__.py").read_text())
    for declaration in declarations.body:
        if isinstance(declaration, ast.Assign):
            for target in declaration.targets:
                if isinstance(target, ast.Name) and target.id in ("__title__", "__version__"):
                    setattr(package, target.id, ast.literal_eval(declaration.value))
    sys.modules["obstor"] = package


parser = argparse.ArgumentParser(add_help=False)
parser.add_argument("--sdk-source")
options, test_args = parser.parse_known_args()
if options.sdk_source:
    use_source_models(options.sdk_source)

from obstor.xml import ET, marshal
from select_models import (
    COMPRESSION_TYPE_NONE, FILE_HEADER_INFO_NONE, JSON_TYPE_DOCUMENT,
    QUOTE_FIELDS_ALWAYS, QUOTE_FIELDS_ASNEEDED, CSVInputSerialization,
    CSVOutputSerialization, JSONInputSerialization, JSONOutputSerialization,
    SelectRequest,
)
from sql_ops import test_sql_expressions_custom_input_output


class Response:
    def __init__(self, body):
        self.body = body

    def stream(self, _chunk_size):
        yield self.body


class Client:
    def __init__(self, body=b"ok", error=None):
        self.body, self.error = body, error
        self.cleaned = []

    def make_bucket(self, **_kwargs):
        pass

    def put_object(self, **_kwargs):
        pass

    def select_object_content(self, **kwargs):
        # Serialize as SDK client does before sending
        marshal(kwargs["request"])
        if self.error:
            raise self.error
        return Response(self.body)

    def remove_object(self, **_kwargs):
        self.cleaned.append("object")

    def remove_bucket(self, **_kwargs):
        self.cleaned.append("bucket")


class RequestModelsTest(unittest.TestCase):
    def test_csv_and_json_wire_values(self):
        for quote_fields in (QUOTE_FIELDS_ALWAYS, QUOTE_FIELDS_ASNEEDED):
            body = marshal(SelectRequest(
                "select * from S3Object",
                CSVInputSerialization(compression_type=COMPRESSION_TYPE_NONE,
                                      file_header_info=FILE_HEADER_INFO_NONE),
                CSVOutputSerialization(quote_fields=quote_fields),
            ))
            root = ET.fromstring(body)
            ns = {"s3": "http://s3.amazonaws.com/doc/2006-03-01/"}
            self.assertEqual(root.findtext("s3:InputSerialization/s3:CompressionType", namespaces=ns), "NONE")
            self.assertEqual(root.findtext("s3:InputSerialization/s3:CSV/s3:FileHeaderInfo", namespaces=ns), "NONE")
            self.assertEqual(root.findtext("s3:OutputSerialization/s3:CSV/s3:QuoteFields", namespaces=ns), quote_fields)
            self.assertNotIn(b"CompressionType.", body)
        body = marshal(SelectRequest(
            "select * from S3Object",
            JSONInputSerialization(json_type=JSON_TYPE_DOCUMENT),
            JSONOutputSerialization(record_delimiter="\n"),
            request_progress=True,
        ))
        self.assertIn(b"<Type>DOCUMENT</Type>", body)
        self.assertIn(b"<Enabled>true</Enabled>", body)

    def test_runner_loads_current_model_api(self):
        # Load exact runner csv.py, avoid stdlib name collision
        spec = importlib.util.spec_from_file_location("s3select_csv_runner", Path(__file__).with_name("csv.py"))
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        self.assertIs(module.SelectRequest, SelectRequest)

    def run_sql(self, client):
        log = SimpleNamespace(args={})
        test_sql_expressions_custom_input_output(
            client, "{}", JSONInputSerialization(json_type=JSON_TYPE_DOCUMENT),
            CSVOutputSerialization(), [("example", "select * from S3Object", b"ok")], log,
        )
        return log.args

    def test_sql_mismatch_and_server_error_fail(self):
        for client, error_type in ((Client(body=b"wrong"), ValueError),
                                   (Client(error=RuntimeError("select failed")), RuntimeError)):
            with self.assertRaises(error_type):
                self.run_sql(client)
            self.assertEqual(client.cleaned, ["object", "bucket"])

    def test_sql_success_counts_and_cleans(self):
        client = Client()
        self.assertEqual(self.run_sql(client), {"total_tests": 1, "total_success": 1})
        self.assertEqual(client.cleaned, ["object", "bucket"])


if __name__ == "__main__":
    unittest.main(argv=[sys.argv[0], *test_args])
