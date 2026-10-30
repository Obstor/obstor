#!/usr/bin/env python3
"""Offline source normalization regressions, including real Java compilation."""

import importlib.util
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


_SPEC = importlib.util.spec_from_file_location("prepare_source", Path(__file__).with_name("prepare-source.py"))
_HELPER = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(_HELPER)


class PrepareSourceTest(unittest.TestCase):
    def test_declarations_ignore_comments_literals_and_nested_types(self):
        source = '''
            /* public class Wrong {} */
            @SuppressWarnings({"public class Wrong {}"})
            public final class Correct {
                String text = "public interface Wrong {}";
                String block = """\npublic record Wrong(int value) {}\n""";
                public static class Nested {}
            }
        '''
        self.assertEqual(_HELPER.public_types(source), ["Correct"])
        for declaration in ("public interface Interface {}", "public enum Enum { ONE }", "public record Record(int value) {}", "public @interface Annotation {}"):
            self.assertEqual(len(_HELPER.public_types(declaration)), 1)

    def test_case_insensitive_collision_fails_before_any_rename(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            first = root / "wrong.java"
            first.write_text("public class Correct {}")
            other = root / "occupied.java"
            other.write_text("public class correct {}")
            with self.assertRaisesRegex(ValueError, "filename collision"):
                _HELPER.prepare_source(root)
            self.assertEqual(sorted(path.name for path in root.iterdir()), ["occupied.java", "wrong.java"])

    def test_existing_destination_is_preserved(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "wrong.java").write_text("public class Correct {}")
            (root / "Correct.java").write_text("// keep this file")
            with self.assertRaisesRegex(ValueError, "destination already exists"):
                _HELPER.prepare_source(root)
            self.assertEqual((root / "Correct.java").read_text(), "// keep this file")
            self.assertTrue((root / "wrong.java").exists())

    @unittest.skipUnless(shutil.which("javac") and shutil.which("java"), "Java compiler/runtime unavailable")
    def test_repaired_sources_compile_and_run(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            sources = {
                "obstorClient.java": 'public class ObstorClient { public static void main(String[] args) { System.out.println(new Response(Status.READY).message()); } }',
                "result.java": "public interface Result { String message(); }",
                "status.java": "public enum Status { READY }",
                "response.java": "public record Response(Status status) implements Result { public String message() { return status().name(); } }",
            }
            for name, source in sources.items():
                (root / name).write_text(source)
            before = subprocess.run(["javac", *map(str, root.glob("*.java"))], capture_output=True, text=True)
            self.assertNotEqual(before.returncode, 0)
            self.assertIn("should be declared in a file named", before.stderr)
            self.assertEqual(_HELPER.prepare_source(root), 4)
            self.assertEqual(_HELPER.prepare_source(root), 0)
            subprocess.run(["javac", *map(str, root.glob("*.java"))], check=True, capture_output=True, text=True)
            result = subprocess.run(["java", "-cp", str(root), "ObstorClient"], check=True, capture_output=True, text=True)
            self.assertEqual(result.stdout.strip(), "READY")


if __name__ == "__main__":
    unittest.main()
