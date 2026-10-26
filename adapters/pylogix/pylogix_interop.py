# SPDX-License-Identifier: MIT
"""ethernetip-interop adapter for pylogix (client role only).

Runs one operation of the container contract (docs/CONTAINER_CONTRACT.md)
with pylogix as the originator and prints one JSON result document.
"""

import argparse
import contextlib
import json
import os
import re
import signal
import socket
import struct
import sys

import pylogix
from pylogix import PLC
from pylogix.lgx_response import cip_error_codes

ADAPTER = "pylogix"
OPERATIONS = [
    "list-identity", "discover", "get-attribute", "set-attribute", "get-attributes-all",
    "identity", "read-tag", "write-tag", "tag-list", "read-tags",
]

# pylogix turns a CIP general status into its text (lgx_response); the code
# is recovered from the same table.
STATUS_CODES = {text: code for code, text in cip_error_codes.items()}


def emit(doc, code=0):
    print(json.dumps(doc, sort_keys=True))
    sys.stdout.flush()
    return code


def head(op):
    return {"schemaVersion": "1.0", "adapter": ADAPTER, "operation": op, "ok": False, "error": None}


def status_of(text):
    """The CIP general status behind pylogix's status text, or -1 when the
    text is not one (pylogix's own messages)."""
    if text in STATUS_CODES:
        return STATUS_CODES[text]
    m = re.fullmatch(r"Unknown error (\d+)", str(text))
    if m and int(m.group(1)) <= 255:
        return int(m.group(1))
    return -1


def error_text(text):
    return None if text == "Success" else str(text)


def identity_doc(dev, with_state=True):
    """A pylogix Device (Discover, GetModuleProperties) as a contract identity."""
    serial = dev.SerialNumber
    doc = {
        "vendorId": dev.VendorID,
        "vendorName": dev.Vendor,
        "deviceType": dev.DeviceID,
        "productCode": dev.ProductCode,
        "revision": dev.Revision,
        "status": dev.Status,
        "serialNumber": int(serial, 16) if isinstance(serial, str) else serial,
        "productName": dev.ProductName,
        "socketAddress": dev.IPAddress,
    }
    if with_state:
        doc["state"] = dev.State
    return doc


def jsonable(v):
    if isinstance(v, (bytes, bytearray)):
        return v.hex()
    if isinstance(v, dict):
        return {str(k): jsonable(x) for k, x in v.items()}
    if isinstance(v, (list, tuple)):
        return [jsonable(x) for x in v]
    return v


def as_list(v):
    return v if isinstance(v, list) else [v]


def parse_route(text):
    """--route PORT/LINK[/PORT/LINK...] as pylogix's Route: [(port, link)]."""
    parts = text.split("/")
    if len(parts) % 2 or not all(parts):
        raise ValueError(text)
    route = []
    for i in range(0, len(parts), 2):
        link = parts[i + 1]
        route.append((int(parts[i], 0), int(link, 0) if re.fullmatch(r"(0x)?[0-9a-fA-F]+", link) and "." not in link else link))
    return route


def write_values(typ, text):
    """The values of write-tag: numbers of an elementary type, comma-separated;
    BOOL true/false (a scalar BOOL, a bit of an integer, "Flags.3", or an
    element of a BOOL array, "Bits[37]", which pylogix writes with Read
    Modify Write); or one STRING."""
    if typ == "STRING":
        return [text]
    if typ == "BOOL":
        return [v.strip().lower() in ("1", "true") for v in text.split(",")]
    return [float(v) if typ in ("REAL", "LREAL") else int(v, 0) for v in text.split(",")]


TYPES = ("BOOL", "SINT", "INT", "DINT", "LINT", "USINT", "UINT", "UDINT", "ULINT", "REAL", "LREAL", "STRING")


def print_fixture(name):
    """Writes /fixtures/<name>/fixture.json; a bad or unknown name is a usage error."""
    if not name or "/" in name or "\\" in name or ".." in name:
        print("invalid fixture name %s" % name, file=sys.stderr)
        return 2
    try:
        with open("/fixtures/%s/fixture.json" % name, "rb") as f:
            sys.stdout.buffer.write(f.read())
        return 0
    except OSError:
        print("no fixture %s" % name, file=sys.stderr)
        return 2


def discover(args):
    """pylogix's Discover(): ListIdentity broadcast over UDP (255.255.255.255,
    --port) from every local address; the reply from --host."""
    try:
        want = socket.gethostbyname(args.host)
    except OSError:
        want = args.host
    plc = PLC(port=args.port, timeout=args.timeout_ms / 1000.0)
    devices = plc.Discover().Value or []
    for dev in devices:
        if dev.IPAddress == want:
            return dev
    return None


def generic(plc, args, service, data):
    """pylogix's Message(): an unconnected request (SendRRData), through
    Unconnected_Send when --route is given. Returns (status, data) or None."""
    attribute = args.attribute if service in (0x0E, 0x10) else None
    try:
        resp = plc.Message(service, args.cls, args.instance, attribute, data)
    except struct.error:
        # pylogix reads the CIP status at a fixed offset: a reply without
        # one (an encapsulation error, header only) does not parse.
        return None, "reply without a CIP reply (encapsulation error)"
    raw = resp.Value
    if not raw or len(raw) < 44:
        return None, resp.Status
    # SendRRData reply: encapsulation header (24), interface handle, timeout,
    # item count, null address item, data item header: the CIP reply at 40.
    ext = raw[43]
    return (raw[42], raw[44 + 2 * ext:]), resp.Status


def main(argv):
    if not argv:
        print("usage: ethernetip-interop client <op> --host HOST ... | print-capabilities | print-fixture [NAME] | version", file=sys.stderr)
        return 2
    cmd = argv[0]
    if cmd == "print-capabilities":
        return emit({
            "schemaVersion": "1.0", "adapter": ADAPTER,
            "adapterVersion": os.environ.get("ETHERNETIP_INTEROP_ADAPTER_VERSION", "dev"), "protocol": "ethernetip",
            "upstream": {"name": "pylogix", "version": pylogix.__version__,
                         "revision": os.environ.get("ETHERNETIP_INTEROP_UPSTREAM_REVISION", ""),
                         "license": "Apache-2.0", "language": "Python", "independentEngine": True},
            "roles": {"server": False, "client": True}, "fixtures": [],
            "clientOperations": OPERATIONS,
            "clientFeatures": {"listIdentityTcp": False, "listIdentityUdp": True, "listServices": False,
                               "getAttributeSingle": True, "setAttributeSingle": True, "getAttributesAll": True,
                               "multipleService": False, "unconnectedSend": True, "class3": False, "class1": False,
                               "largeForwardOpen": True, "multicast": False, "inputOnly": False, "listenOnly": False,
                               "logixTags": True, "readTagFragmented": True, "tagList": True,
                               "readModifyWrite": True, "templates": False},
            "serverFeatures": {},
        })
    if cmd == "version":
        return emit({"adapter": ADAPTER, "adapterVersion": os.environ.get("ETHERNETIP_INTEROP_ADAPTER_VERSION", "dev"),
                     "upstream": "pylogix", "upstreamVersion": pylogix.__version__,
                     "upstreamRevision": os.environ.get("ETHERNETIP_INTEROP_UPSTREAM_REVISION", ""), "contract": "1.0"})
    if cmd == "print-fixture":
        return print_fixture(argv[1] if len(argv) > 1 else "adapter")
    if cmd == "server":
        print("the pylogix adapter has no server role", file=sys.stderr)
        return 2
    if cmd != "client" or len(argv) < 2 or argv[1] not in OPERATIONS:
        print("unknown command or operation", file=sys.stderr)
        return 2
    op = argv[1]
    p = argparse.ArgumentParser(prog="ethernetip-interop client " + op, allow_abbrev=False)
    p.add_argument("--host", required=True)
    p.add_argument("--port", type=int, default=44818)
    p.add_argument("--timeout-ms", type=int, default=5000)
    p.add_argument("--transport", default="udp", choices=["tcp", "udp"])
    p.add_argument("--class", dest="cls", type=lambda s: int(s, 0), default=0)
    p.add_argument("--instance", type=lambda s: int(s, 0), default=0)
    p.add_argument("--attribute", type=lambda s: int(s, 0), default=0)
    p.add_argument("--data", default="")
    p.add_argument("--connected", action="store_true")
    p.add_argument("--route", default="")
    p.add_argument("--tag", default="")
    p.add_argument("--tags", default="")
    p.add_argument("--count", type=int, default=1)
    p.add_argument("--type", default="")
    p.add_argument("--values", default="")
    try:
        args = p.parse_args(argv[2:])
    except SystemExit:
        return 2
    # Usage errors: nothing is sent.
    if args.connected:
        print("--connected: pylogix sends generic messages unconnected only", file=sys.stderr)
        return 2
    if op == "list-identity" and args.transport == "tcp":
        print("--transport tcp: pylogix has no ListIdentity over TCP", file=sys.stderr)
        return 2
    route = None
    if args.route:
        if op in ("list-identity", "discover"):
            print("--route does not apply to %s" % op, file=sys.stderr)
            return 2
        try:
            route = parse_route(args.route)
        except ValueError:
            print("--route: not PORT/LINK[/PORT/LINK...]", file=sys.stderr)
            return 2
    payload = b""
    if op in ("get-attribute", "get-attributes-all", "set-attribute"):
        try:
            payload = bytes.fromhex(args.data)
        except ValueError:
            print("--data: not hex", file=sys.stderr)
            return 2
        if args.attribute > 255 and op != "get-attributes-all":
            print("--attribute: pylogix packs 8-bit attribute segments only", file=sys.stderr)
            return 2
    if op in ("read-tag", "write-tag") and not args.tag or op == "read-tags" and not args.tags:
        print("--tag/--tags is required", file=sys.stderr)
        return 2
    vals = None
    if op == "write-tag":
        if args.type not in TYPES:
            print("--type: one of %s (pylogix writes no structures)" % ", ".join(TYPES), file=sys.stderr)
            return 2
        try:
            vals = write_values(args.type, args.values)
        except ValueError:
            print("--values: not %s" % args.type, file=sys.stderr)
            return 2
    if args.count < 1:
        print("--count: at least 1", file=sys.stderr)
        return 2

    timeout = args.timeout_ms / 1000.0
    doc = head(op)
    # Every wait is bounded: --timeout-ms bounds each request, the watchdog
    # the operation (pylogix can loop without end on replies it does not
    # expect, see CAPABILITIES.md).
    limit = max(10, int(4 * timeout + 0.999))
    signal.signal(signal.SIGALRM, watchdog)
    signal.alarm(limit)
    # pylogix prints some diagnostics to stdout: stdout is for the document.
    try:
        with contextlib.redirect_stdout(sys.stderr):
            code = run(op, args, route, payload, vals, timeout, doc)
    except Watchdog:
        doc["ok"] = False
        doc["error"] = {"code": "timeout", "message": "operation not complete after %d s (watchdog)" % limit}
        code = 1
    except Exception as e:  # pylint: disable=broad-except
        doc["ok"] = False
        doc["error"] = {"code": "failed", "message": "%s: %s" % (type(e).__name__, e)}
        code = 1
    signal.alarm(0)
    if code == 0:
        doc["ok"] = True
        doc["error"] = None
    return emit(doc, code)


class Watchdog(BaseException):
    """Raised by SIGALRM; a BaseException, so that pylogix's own
    "except Exception" handlers do not swallow it."""


def watchdog(_signum, _frame):
    raise Watchdog()


def fail(doc, code, message):
    doc["error"] = {"code": code, "message": str(message)}
    return 1


def run(op, args, route, payload, vals, timeout, doc):
    if op in ("discover", "list-identity"):
        dev = discover(args)
        if dev is None:
            return fail(doc, "timeout", "no ListIdentity reply from %s" % args.host)
        doc["identity"] = identity_doc(dev)
        return 0

    with PLC(args.host, port=args.port, timeout=timeout) as plc:
        if route:
            plc.Route = route
        if op in ("get-attribute", "get-attributes-all", "set-attribute", "identity"):
            ok, msg = plc.conn.connect(False)
            if not ok:
                return fail(doc, "connect-failed", msg)
            if op == "identity":
                # GetModuleProperties(slot): Get_Attributes_All of the
                # Identity object through Unconnected_Send to backplane slot
                # (--route, default 1/0).
                slot = route[-1][1] if route else 0
                resp = plc.GetModuleProperties(slot)
                if not plc.conn.SocketConnected:
                    return fail(doc, "connection-lost", resp.Status)
                if resp.Status != "Success":
                    return fail(doc, "failed", "GetModuleProperties: %s" % resp.Status)
                # pylogix takes the last octet of the reply as the state:
                # Get_Attributes_All has none, so it is left out.
                doc["identity"] = identity_doc(resp.Value, with_state=False)
                return 0
            service = {"get-attribute": 0x0E, "get-attributes-all": 0x01, "set-attribute": 0x10}[op]
            reply, text = generic(plc, args, service, payload)
            if reply is None:
                return fail(doc, "failed", text or "no reply")
            status, data = reply
            doc.update(status=status, data=bytes(data).hex(), errorText=error_text(cip_error_codes.get(status, text) if status else "Success"))
            return 0

        # Logix: a Class 3 connection (Large_Forward_Open, then Forward_Open).
        ok, msg = plc.conn.connect(True)
        if not ok:
            return fail(doc, "connect-failed", msg)
        if op == "tag-list":
            resp = plc.GetTagList(True)
            if not plc.conn.SocketConnected:
                return fail(doc, "connection-lost", resp.Status)
            tags = resp.Value or []
            doc["tags"] = [{"name": t.TagName, "type": t.DataType or None, "dims": [t.Size] if t.Array else []}
                           for t in tags]
            doc["programName"] = None
            doc["programs"] = list(plc.ProgramNames)
            doc["status"] = status_of(resp.Status)
            doc["errorText"] = error_text(resp.Status)
        elif op == "read-tag":
            resp = plc.Read(args.tag, args.count)
            if resp.Value is None and not plc.conn.SocketConnected:
                return fail(doc, "connection-lost", resp.Status)
            base = re.sub(r"\[[^\]]*\]$", "", re.sub(r"\.\d+$", "", args.tag))
            known = plc.KnownTags.get(base)
            typ = plc.CIPTypes.get(known[0], (0, None))[1] if known else None
            if typ == "STRUCT" and isinstance(resp.Value, (str, list)) and all(isinstance(x, str) for x in as_list(resp.Value)):
                typ = "STRING"
            doc.update(status=status_of(resp.Status), errorText=error_text(resp.Status), type=typ,
                       values=jsonable(as_list(resp.Value)) if resp.Value is not None else [])
        elif op == "read-tags":
            res = plc.Read(args.tags.split(","))
            if not plc.conn.SocketConnected:
                return fail(doc, "connection-lost", "connection closed")
            doc["results"] = [{"tag": r.TagName, "values": jsonable(as_list(r.Value)) if r.Value is not None else [],
                               "status": status_of(r.Status), "errorText": error_text(r.Status)} for r in res]
        elif op == "write-tag":
            resp = plc.Write(args.tag, vals if args.type != "STRING" else vals[0])
            if not plc.conn.SocketConnected:
                return fail(doc, "connection-lost", resp.Status)
            doc.update(status=status_of(resp.Status), errorText=error_text(resp.Status))
        return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
