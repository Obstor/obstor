#!/usr/bin/env python3
"""Normalize public Java type filenames in a disposable SDK checkout."""

import argparse
import os
from pathlib import Path
import re
import tempfile


_LITERALS = re.compile(
    r'//[^\n]*|/\*.*?\*/|"""(?:\\.|(?!""").)*"""|"(?:\\.|[^"\\])*"|\'(?:\\.|[^\'\\])*\'',
    re.DOTALL,
)
_TOKENS = re.compile(r"[A-Za-z_$][A-Za-z0-9_$]*|[{}();.@]")
_IDENTIFIER = re.compile(r"[A-Za-z_$][A-Za-z0-9_$]*\Z")


def public_types(source: str) -> list[str]:
    """Read top-level declarations without matching comments or nested types."""
    tokens = _TOKENS.findall(_LITERALS.sub(" ", source))
    braces = parentheses = 0
    public = False
    names = []
    previous = ""
    for index, token in enumerate(tokens):
        if braces == 0 and parentheses == 0:
            if token == "public":
                public = True
            elif token in {"class", "interface", "enum", "record"} and previous != "." and public:
                if index + 1 >= len(tokens) or not _IDENTIFIER.fullmatch(tokens[index + 1]):
                    raise ValueError("public Java declaration has no type name")
                names.append(tokens[index + 1])
                public = False
            elif token in {";", "{"}:
                public = False
        if token == "{":
            braces += 1
        elif token == "}":
            braces -= 1
        elif token == "(":
            parentheses += 1
        elif token == ")":
            parentheses -= 1
        if braces < 0 or parentheses < 0:
            raise ValueError("unbalanced Java source")
        previous = token
    if braces or parentheses:
        raise ValueError("unbalanced Java source")
    return names


def prepare_source(root: Path) -> int:
    """Preflight every rename, including collisions on case-insensitive disks."""
    root = root.resolve(strict=True)
    if not root.is_dir():
        raise ValueError("SDK source root must be a directory")
    entries = {}
    java_files = []
    for path in sorted(root.rglob("*")):
        if ".git" in path.relative_to(root).parts:
            continue
        key = str(path.relative_to(root)).casefold()
        if key in entries:
            raise ValueError(f"case-insensitive path collision: {entries[key]} and {path}")
        entries[key] = path
        if path.suffix == ".java":
            if path.is_symlink() or not path.is_file():
                raise ValueError(f"Java source must be a regular file: {path}")
            java_files.append(path)

    renames = []
    targets = {}
    for source in java_files:
        names = public_types(source.read_text(encoding="utf-8"))
        if len(names) > 1:
            raise ValueError(f"multiple public top-level types in {source}")
        if not names:
            continue
        target = source.with_name(names[0] + ".java")
        key = str(target.relative_to(root)).casefold()
        if key in targets:
            raise ValueError(f"public type filename collision: {targets[key]} and {source}")
        targets[key] = source
        if key in entries and entries[key] != source:
            raise ValueError(f"public type destination already exists: {target}")
        if source != target:
            renames.append((source, target))

    # Vacate every old name. This also supports a case-only rename
    staged = []
    completed = []
    try:
        for source, target in renames:
            descriptor, name = tempfile.mkstemp(prefix=".prepare-java-", suffix=".tmp", dir=source.parent)
            os.close(descriptor)
            temporary = Path(name)
            try:
                source.replace(temporary)
            except BaseException:
                temporary.unlink()
                raise
            staged.append((source, target, temporary))
        for source, target, temporary in staged:
            # link refuses to overwrite a file created after preflight
            os.link(temporary, target)
            completed.append((source, target))
            temporary.unlink()
    except BaseException:
        for source, target in reversed(completed):
            target.rename(source)
        for source, _, temporary in staged:
            if temporary.exists():
                temporary.rename(source)
        raise
    return len(renames)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source_root", type=Path)
    args = parser.parse_args()
    try:
        count = prepare_source(args.source_root)
    except (OSError, UnicodeError, ValueError) as error:
        parser.exit(1, f"cannot prepare Java SDK source: {error}\n")
    print(f"Prepared {count} Java source filenames")


if __name__ == "__main__":
    main()
