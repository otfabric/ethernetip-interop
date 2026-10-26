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
     their CIP type, element counts that match the values, structure
     templates whose members fit, and structure values that match them.

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
TYPE_SIZES = {"BOOL": 1, "SINT": 1, "USINT": 1, "INT": 2, "UINT": 2, "DINT": 4, "UDINT": 4, "REAL": 4,
              "LINT": 8, "ULINT": 8, "LREAL": 8}
STRING_SIZE, STRING_CAPACITY = 88, 82  # Logix STRING: DINT length and 82 characters, padded


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


def scalar_errors(where, typ, v):
    """The errors of one value of an elementary type."""
    if isinstance(v, bool) or not isinstance(v, (int, float)):
        return [f"{where}: {v!r} is not a number"]
    if typ in FLOAT_TYPES:
        if typ == "REAL" and not is_float32(float(v)):
            return [f"{where}: {v} is not exactly representable as a REAL"]
        return []
    if v != int(v):
        return [f"{where}: {v} is not an integer, but {typ} is"]
    lo, hi = INTEGER_RANGES[typ]
    if not lo <= int(v) <= hi:
        return [f"{where}: {int(v)} is outside the range of {typ} ({lo}..{hi})"]
    return []


def string_errors(where, v):
    if not isinstance(v, str):
        return [f"{where}: {v!r} is not a string"]
    if len(v) > STRING_CAPACITY:
        return [f"{where}: {len(v)} characters, a STRING holds {STRING_CAPACITY}"]
    return []


def template_errors(fx):
    """Checks the templates; returns the errors and the templates by name."""
    errors, by_name, ids, handles = [], {}, set(), set()
    for tp in fx.get("templates", []):
        name = tp.get("name")
        where = f"templates/{name}"
        if str(name).upper() in CIP_TYPES | {"STRING"} or str(name).lower() in {n.lower() for n in by_name}:
            errors.append(f"{where}: duplicate or reserved name")
        for key, seen in (("id", ids), ("handle", handles)):
            if tp.get(key) in seen:
                errors.append(f"{where}: duplicate {key} {tp.get(key)}")
            seen.add(tp.get(key))
        size, members = tp.get("size", 0), set()
        for m in tp.get("members", []):
            mn, mt, off = m.get("name"), m.get("type"), m.get("offset", 0)
            mw = f"{where}/{mn}"
            if str(mn).lower() in members:
                errors.append(f"{mw}: duplicate member")
            members.add(str(mn).lower())
            if mt == "BOOL" and "count" not in m:
                if "bit" not in m:
                    errors.append(f"{mw}: a BOOL member needs its bit in the host octet")
                n = 1
            elif "bit" in m:
                errors.append(f"{mw}: only a scalar BOOL member has a bit")
                n = 0
            elif mt in TYPE_SIZES:
                n = TYPE_SIZES[mt] * m.get("count", 1) if mt != "BOOL" else (m["count"] + 31) // 32 * 4
            elif mt == "STRING":
                n = STRING_SIZE * m.get("count", 1)
            elif mt in by_name:
                n = by_name[mt]["size"] * m.get("count", 1)
            else:
                errors.append(f"{mw}: type {mt!r} is not an elementary type, STRING or an earlier template")
                n = 0
            if off + n > size:
                errors.append(f"{mw}: ends at {off + n}, beyond the size {size}")
        if size % 4:
            errors.append(f"{where}: size {size} is not a multiple of 4, as Logix aligns structures")
        by_name[name] = tp
    return errors, by_name


def field_errors(where, tp, fields, templates):
    """The errors of a structure value of template tp."""
    if not isinstance(fields, dict):
        return [f"{where}: {fields!r} is not an object"]
    errors = []
    members = {m["name"]: m for m in tp.get("members", []) if not m["name"].startswith(("ZZZZZZZZZZ", "__"))}
    for k, v in fields.items():
        m = members.get(k)
        if m is None:
            errors.append(f"{where}/{k}: no such member of {tp['name']}")
            continue
        vs = v if "count" in m else [v]
        if not isinstance(vs, list) or len(vs) != m.get("count", 1):
            errors.append(f"{where}/{k}: needs {m.get('count', 1)} element(s)")
            continue
        for i, x in enumerate(vs):
            xw = f"{where}/{k}[{i}]" if "count" in m else f"{where}/{k}"
            if m["type"] == "STRING":
                errors.extend(string_errors(xw, x))
            elif m["type"] in templates:
                errors.extend(field_errors(xw, templates[m["type"]], x, templates))
            elif m["type"] in CIP_TYPES:
                errors.extend(scalar_errors(xw, m["type"], x))
    return errors


def value_errors(t, templates=None):
    errors = []
    templates = templates or {}
    typ, name = t.get("type"), t.get("name")
    count = t.get("count", 1)
    if typ == "STRING" or typ in templates:
        key = "strings" if typ == "STRING" else "fields"
        vals = t.get(key)
        if vals is None:
            return [f"tags/{name}: a {typ} tag takes its initial value from {key!r}"]
        if len(vals) != count:
            errors.append(f"tags/{name}: {len(vals)} {key} for {count} element(s)")
        for i, v in enumerate(vals):
            if typ == "STRING":
                errors.extend(string_errors(f"tags/{name}[{i}]", v))
            else:
                errors.extend(field_errors(f"tags/{name}[{i}]", templates[typ], v, templates))
        return errors
    if "strings" in t or "fields" in t:
        return [f"tags/{name}: strings and fields are for STRING and structure tags"]
    if typ not in CIP_TYPES:
        return [f"tags/{name}: type {typ!r} is not one of the CIP types {sorted(CIP_TYPES)}"]
    count = t.get("count", 1)
    if "values" in t and len(t["values"]) != count:
        errors.append(f"tags/{name}: {len(t['values'])} values for {count} element(s)")
    for i, v in enumerate(tag_values(t)):
        errors.extend(scalar_errors(f"tags/{name}[{i}]", typ, v))
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
    terrs, templates = template_errors(fx)
    errors.extend(terrs)
    names = set()
    for t in fx.get("tags", []):
        n = str(t.get("name", "")).lower()
        if n in names:
            errors.append(f"tags/{t.get('name')}: duplicate name (tag names are case-insensitive)")
        names.add(n)
        errors.extend(value_errors(t, templates))
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
