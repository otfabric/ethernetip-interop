#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Validate every fixtures/*/fixture.json.

Two layers, both required:

  1. The structural rules of fixtures/schema/ethernetip-fixture.schema.json.
     They are checked with the `jsonschema` package when it is installed; CI
     installs it, so a missing package is an error there (--require-schema).
  2. The cross-field rules a JSON Schema cannot express: unique assembly
     instances and tag names, assembly sizes that fit their kind, an echo
     that has an output and an input of the same size, tag values that fit
     their CIP type, element counts that match the values.

Usage: validate-fixtures.py [--require-schema] [fixture.json ...]
"""

import json
import re
import struct
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SCHEMA = ROOT / "fixtures" / "schema" / "ethernetip-fixture.schema.json"

# CIP elementary types the fixture format has, with their value ranges.
INTEGER_RANGES = {
    "BOOL": (0, 1),
    "SINT": (-(2**7), 2**7 - 1),
    "INT": (-(2**15), 2**15 - 1),
    "DINT": (-(2**31), 2**31 - 1),
    "LINT": (-(2**63), 2**63 - 1),
    "USINT": (0, 2**8 - 1),
    "UINT": (0, 2**16 - 1),
    "UDINT": (0, 2**32 - 1),
    "ULINT": (0, 2**64 - 1),
}
FLOAT_TYPES = {"REAL", "LREAL"}
CIP_TYPES = set(INTEGER_RANGES) | FLOAT_TYPES
ZERO_SIZE_KINDS = {"input-only", "listen-only"}


def is_float32(v):
    try:
        return struct.unpack("<f", struct.pack("<f", v))[0] == v
    except (OverflowError, struct.error):
        return False


def tag_values(t):
    """The initial value of every element, as the adapters compute it."""
    if "pattern" in t:
        p = t["pattern"]
        return [i * p["multiplier"] + p["offset"] for i in range(t.get("count", 1))]
    return list(t.get("values", []))


def value_errors(t):
    errors = []
    typ, name = t.get("type"), t.get("name")
    if typ not in CIP_TYPES:
        return [f"tags/{name}: type {typ!r} is not one of the CIP types {sorted(CIP_TYPES)}"]
    count = t.get("count", 1)
    if "values" in t and len(t["values"]) != count:
        errors.append(f"tags/{name}: {len(t['values'])} values for {count} element(s)")
    for i, v in enumerate(tag_values(t)):
        if isinstance(v, bool) or not isinstance(v, (int, float)):
            errors.append(f"tags/{name}[{i}]: {v!r} is not a number")
            continue
        if typ in FLOAT_TYPES:
            if typ == "REAL" and not is_float32(float(v)):
                errors.append(f"tags/{name}[{i}]: {v} is not exactly representable as a REAL")
            continue
        if v != int(v):
            errors.append(f"tags/{name}[{i}]: {v} is not an integer, but {typ} is")
            continue
        lo, hi = INTEGER_RANGES[typ]
        if not lo <= int(v) <= hi:
            errors.append(f"tags/{name}[{i}]: {int(v)} is outside the range of {typ} ({lo}..{hi})")
    return errors


def semantic_errors(fx):
    errors = []
    rev = fx.get("identity", {}).get("revision", "")
    if re.fullmatch(r"\d+\.\d+", str(rev)) and any(int(x) > 255 for x in rev.split(".")):
        errors.append(f"identity/revision: {rev}: major and minor are USINTs, 0..255")
    seen = set()
    for a in fx.get("assemblies", []):
        inst, kind, size = a.get("instance"), a.get("kind"), a.get("size")
        if inst in seen:
            errors.append(f"assemblies/{inst}: duplicate instance")
        seen.add(inst)
        if kind in ZERO_SIZE_KINDS and size != 0:
            errors.append(f"assemblies/{inst}: a {kind} connection point carries no data: size must be 0")
        if kind not in ZERO_SIZE_KINDS and isinstance(size, int) and size < 1:
            errors.append(f"assemblies/{inst}: a {kind} assembly needs a size of at least 1")
    if fx.get("echoOutputToInput"):
        ins = [a for a in fx.get("assemblies", []) if a.get("kind") == "input"]
        outs = [a for a in fx.get("assemblies", []) if a.get("kind") == "output"]
        if len(ins) != 1 or len(outs) != 1:
            errors.append("echoOutputToInput: needs exactly one input and one output assembly")
        elif ins[0].get("size") != outs[0].get("size"):
            errors.append(f"echoOutputToInput: output {outs[0].get('instance')} and input {ins[0].get('instance')} differ in size")
    names = set()
    for t in fx.get("tags", []):
        n = str(t.get("name", "")).lower()
        if n in names:
            errors.append(f"tags/{t.get('name')}: duplicate name (tag names are case-insensitive)")
        names.add(n)
        errors.extend(value_errors(t))
    return errors


def main(argv):
    require_schema = "--require-schema" in argv
    paths = [Path(a) for a in argv if not a.startswith("--")]
    if not paths:
        paths = sorted((ROOT / "fixtures").glob("*/fixture.json"))
    if not paths:
        print("no fixtures found", file=sys.stderr)
        return 1

    validator = None
    try:
        import jsonschema

        schema = json.loads(SCHEMA.read_text())
        jsonschema.Draft202012Validator.check_schema(schema)
        validator = jsonschema.Draft202012Validator(schema)
    except ImportError:
        if require_schema:
            print("the jsonschema package is required (pip install jsonschema)", file=sys.stderr)
            return 1
        print("note: jsonschema not installed, schema layer skipped (cross-field rules still checked)", file=sys.stderr)

    failed = False
    for path in paths:
        errors = []
        try:
            fx = json.loads(path.read_text())
        except (OSError, ValueError) as e:
            errors.append(str(e))
            fx = None
        if fx is not None:
            if validator is not None:
                for e in sorted(validator.iter_errors(fx), key=lambda e: list(e.absolute_path)):
                    where = "/".join(str(x) for x in e.absolute_path) or "(root)"
                    errors.append(f"{where}: {e.message}")
            if isinstance(fx, dict):
                errors.extend(semantic_errors(fx))
                if path.parent.name != fx.get("name"):
                    errors.append(f"name {fx.get('name')!r} does not match directory {path.parent.name!r}")
        path = path.resolve()
        rel = path.relative_to(ROOT) if path.is_relative_to(ROOT) else path
        if errors:
            failed = True
            print(f"FAIL {rel}")
            for e in errors:
                print(f"     {e}")
        else:
            print(f"ok   {rel}")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
