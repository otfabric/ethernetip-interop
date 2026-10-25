#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Validate the documents the self-tests kept against schemas/.

Every *.json under the given directories (default: reports) that is a
capability document (it has "clientOperations") is checked against
schemas/capabilities.schema.json; every client result document (it has
"operation") against schemas/client-result.schema.json. Other files are
ignored.

Usage: validate-documents.py [DIR ...]
"""

import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent


def main(argv):
    try:
        import jsonschema
    except ImportError:
        print("the jsonschema package is required (pip install jsonschema)", file=sys.stderr)
        return 1
    validators = {}
    for kind, name in (("capabilities", "capabilities.schema.json"), ("result", "client-result.schema.json")):
        schema = json.loads((ROOT / "schemas" / name).read_text())
        jsonschema.Draft202012Validator.check_schema(schema)
        validators[kind] = jsonschema.Draft202012Validator(schema)
    dirs = [Path(a) for a in argv] or [ROOT / "reports"]
    checked = failed = 0
    for d in dirs:
        for path in sorted(d.rglob("*.json")):
            try:
                doc = json.loads(path.read_text())
            except ValueError as e:
                print(f"FAIL {path}: not JSON: {e}")
                failed += 1
                continue
            if not isinstance(doc, dict):
                continue
            kind = "capabilities" if "clientOperations" in doc else "result" if "operation" in doc else None
            if kind is None:
                continue
            checked += 1
            errors = sorted(validators[kind].iter_errors(doc), key=lambda e: list(e.absolute_path))
            if errors:
                failed += 1
                print(f"FAIL {path} ({kind})")
                for e in errors:
                    print(f"     {'/'.join(str(x) for x in e.absolute_path) or '(root)'}: {e.message}")
    print(f"{checked} documents checked, {failed} invalid")
    return 1 if failed or not checked else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
