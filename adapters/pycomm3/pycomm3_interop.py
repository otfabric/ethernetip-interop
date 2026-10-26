# SPDX-License-Identifier: MIT
"""ethernetip-interop adapter for pycomm3 (client role only).

Runs one operation of the container contract (docs/CONTAINER_CONTRACT.md)
with pycomm3 as the originator and prints one JSON result document.
"""

import argparse
import json
import logging
import os
import sys

import pycomm3
from pycomm3 import CIPDriver, LogixDriver
from pycomm3.cip.status_info import PRODUCT_TYPES, VENDORS

ADAPTER = "pycomm3"
OPERATIONS = [
    "list-identity", "discover", "get-attribute", "set-attribute", "get-attributes-all",
    "identity", "read-tag", "write-tag", "tag-list", "read-tags",
]

# pycomm3 resolves vendor and product type codes to names: the codes are
# recovered from the same tables.
VENDOR_IDS = {name: code for code, name in VENDORS.items() if isinstance(code, int)}
TYPE_IDS = {name: code for code, name in PRODUCT_TYPES.items() if isinstance(code, int)}


def emit(doc, code=0):
    print(json.dumps(doc, sort_keys=True))
    sys.stdout.flush()
    return code


def head(op):
    return {"schemaVersion": "1.0", "adapter": ADAPTER, "operation": op, "ok": False, "error": None}


def identity_doc(d):
    """The identity dict of pycomm3 (list_identity, discover) as a contract document."""
    rev = d.get("revision") or {}
    serial = d.get("serial")
    status = d.get("status")
    if isinstance(status, (bytes, bytearray)):
        status = int.from_bytes(status, "little")
    return {
        "vendorId": VENDOR_IDS.get(d.get("vendor")),
        "vendorName": d.get("vendor"),
        "deviceType": TYPE_IDS.get(d.get("product_type")),
        "productCode": d.get("product_code"),
        "revision": "%s.%s" % (rev.get("major"), rev.get("minor")),
        "status": status,
        "serialNumber": int(serial, 16) if isinstance(serial, str) else serial,
        "productName": d.get("product_name"),
        "state": d.get("state"),
        "socketAddress": "%s:%s" % (d.get("ip_address"), d.get("socket_address", {}).get("port", "")) if isinstance(d.get("socket_address"), dict) else d.get("ip_address"),
    }


def jsonable(v):
    if isinstance(v, (bytes, bytearray)):
        return v.hex()
    if isinstance(v, dict):
        return {str(k): jsonable(x) for k, x in v.items()}
    if isinstance(v, (list, tuple)):
        return [jsonable(x) for x in v]
    return v


def driver(cls, host, timeout):
    """A pycomm3 driver with the operation's timeout; opened by "with"."""
    drv = cls(host)
    drv.socket_timeout = timeout
    return drv


def generic(args, service, data, timeout):
    with driver(CIPDriver, args.host, timeout) as drv:
        kwargs = dict(service=service, class_code=args.cls, instance=args.instance,
                      request_data=data, connected=args.connected, unconnected_send=False,
                      route_path=False, return_response_packet=True)
        if service in (0x0E, 0x10):
            kwargs["attribute"] = args.attribute
        tag = drv.generic_message(**kwargs)
        resp = tag.value
        status = resp.service_status if resp is not None else None
        data = b""
        if resp is not None and getattr(resp, "data", None) is not None:
            data = resp.data
        return status, data, tag.error


def write_values(typ, text):
    """The values of write-tag: numbers of an elementary type, comma-separated;
    BOOL true/false (a bit of an integer, "Flags.3", or of a BOOL array,
    "Bits[5]", which pycomm3 writes with Read Modify Write); one STRING; or
    one JSON value (a structure as an object of its members)."""
    if typ == "STRING":
        return [text]
    if typ == "JSON":
        return [json.loads(text)]
    if typ == "BOOL":
        return [v.strip().lower() in ("1", "true") for v in text.split(",")]
    return [float(v) if typ in ("REAL", "LREAL") else int(v, 0) for v in text.split(",")]


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


def main(argv):
    if not argv:
        print("usage: ethernetip-interop client <op> --host HOST ... | print-capabilities | print-fixture [NAME] | version", file=sys.stderr)
        return 2
    cmd = argv[0]
    if cmd == "print-capabilities":
        return emit({
            "schemaVersion": "1.0", "adapter": ADAPTER,
            "adapterVersion": os.environ.get("ETHERNETIP_INTEROP_ADAPTER_VERSION", "dev"), "protocol": "ethernetip",
            "upstream": {"name": "pycomm3", "version": pycomm3.__version__,
                         "revision": os.environ.get("ETHERNETIP_INTEROP_UPSTREAM_REVISION", ""),
                         "license": "MIT", "language": "Python", "independentEngine": True},
            "roles": {"server": False, "client": True}, "fixtures": [],
            "clientOperations": OPERATIONS,
            "clientFeatures": {"listIdentityTcp": True, "listIdentityUdp": True, "listServices": False,
                               "getAttributeSingle": True, "setAttributeSingle": True, "getAttributesAll": True,
                               "multipleService": False, "unconnectedSend": True, "class3": True, "class1": False,
                               "largeForwardOpen": True, "multicast": False, "inputOnly": False, "listenOnly": False,
                               "logixTags": True, "readTagFragmented": True, "tagList": True,
                               "readModifyWrite": True, "templates": True},
            "serverFeatures": {},
        })
    if cmd == "version":
        return emit({"adapter": ADAPTER, "adapterVersion": os.environ.get("ETHERNETIP_INTEROP_ADAPTER_VERSION", "dev"),
                     "upstream": "pycomm3", "upstreamVersion": pycomm3.__version__,
                     "upstreamRevision": os.environ.get("ETHERNETIP_INTEROP_UPSTREAM_REVISION", ""), "contract": "1.0"})
    if cmd == "print-fixture":
        return print_fixture(argv[1] if len(argv) > 1 else "adapter")
    if cmd == "server":
        print("the pycomm3 adapter has no server role", file=sys.stderr)
        return 2
    if cmd != "client" or len(argv) < 2 or argv[1] not in OPERATIONS:
        print("unknown command or operation", file=sys.stderr)
        return 2
    op = argv[1]
    p = argparse.ArgumentParser(prog="ethernetip-interop client " + op, allow_abbrev=False)
    p.add_argument("--host", required=True)
    p.add_argument("--port", type=int, default=44818)
    p.add_argument("--timeout-ms", type=int, default=5000)
    p.add_argument("--transport", default="tcp", choices=["tcp", "udp"])
    p.add_argument("--class", dest="cls", type=lambda s: int(s, 0), default=0)
    p.add_argument("--instance", type=lambda s: int(s, 0), default=0)
    p.add_argument("--attribute", type=lambda s: int(s, 0), default=0)
    p.add_argument("--data", default="")
    p.add_argument("--connected", action="store_true")
    p.add_argument("--tag", default="")
    p.add_argument("--tags", default="")
    p.add_argument("--count", type=int, default=1)
    p.add_argument("--type", default="")
    p.add_argument("--values", default="")
    try:
        args = p.parse_args(argv[2:])
    except SystemExit:
        return 2
    if args.port != 44818:
        args.host = "%s:%d" % (args.host, args.port)
    timeout = args.timeout_ms / 1000.0
    doc = head(op)
    try:
        if op == "discover" or (op == "list-identity" and args.transport == "udp"):
            # pycomm3's discover(): ListIdentity over UDP to port 44818 of
            # the address (unicast here), every reply within its timeout.
            found = [x for x in CIPDriver.discover(args.host)]
            if not found:
                doc["error"] = {"code": "timeout", "message": "no reply"}
                return emit(doc, 1)
            doc["identity"] = identity_doc(found[0])
        elif op == "list-identity":
            d = CIPDriver.list_identity(args.host)
            if not d:
                doc["error"] = {"code": "failed", "message": "no ListIdentity reply"}
                return emit(doc, 1)
            doc["identity"] = identity_doc(d)
        elif op in ("get-attribute", "get-attributes-all", "set-attribute"):
            service = {"get-attribute": 0x0E, "get-attributes-all": 0x01, "set-attribute": 0x10}[op]
            try:
                payload = bytes.fromhex(args.data)
            except ValueError:
                print("--data: not hex", file=sys.stderr)
                return 2
            status, data, err = generic(args, service, payload, timeout)
            if status is None:
                doc["error"] = {"code": "failed", "message": err or "no reply"}
                return emit(doc, 1)
            doc.update(status=status, data=bytes(data).hex(), errorText=err)
        elif op == "identity":
            with driver(CIPDriver, args.host, timeout) as drv:
                doc["module"] = jsonable(drv.get_module_info(0))
        else:
            with driver(LogixDriver, args.host, timeout) as plc:
                if op == "tag-list":
                    doc["tags"] = [{"name": n, "type": d.get("data_type_name"), "dims": [x for x in (d.get("dimensions") or []) if x]}
                                   for n, d in sorted(plc.tags.items())]
                    doc["programName"] = plc.info.get("name")
                elif op == "read-tag":
                    name = args.tag if args.count == 1 else "%s{%d}" % (args.tag, args.count)
                    t = plc.read(name)
                    doc.update(status=0 if t.error is None else -1, errorText=t.error, type=t.type,
                               values=jsonable(t.value if isinstance(t.value, list) else [t.value]))
                elif op == "read-tags":
                    res = plc.read(*args.tags.split(","))
                    doc["results"] = [{"tag": t.tag, "values": jsonable(t.value if isinstance(t.value, list) else [t.value]),
                                       "errorText": t.error} for t in res]
                elif op == "write-tag":
                    vals = write_values(args.type, args.values)
                    name = args.tag if len(vals) == 1 else "%s{%d}" % (args.tag, len(vals))
                    t = plc.write((name, vals[0] if len(vals) == 1 else vals))
                    doc.update(status=0 if t.error is None else -1, errorText=t.error)
        doc["ok"] = True
        return emit(doc)
    except Exception as e:  # pylint: disable=broad-except
        doc["error"] = {"code": "failed", "message": "%s: %s" % (type(e).__name__, e)}
        return emit(doc, 1)


if __name__ == "__main__":
    logging.basicConfig(stream=sys.stderr, level=logging.WARNING)
    sys.exit(main(sys.argv[1:]))
