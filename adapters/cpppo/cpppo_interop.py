# SPDX-License-Identifier: GPL-3.0-or-later
"""ethernetip-interop adapter for cpppo (server and client roles).

This adapter imports cpppo, which is GPL-3.0, and is therefore licensed under
the GNU General Public License, version 3 or later (see adapters/LICENSE).

server  serves a fixture with cpppo's EtherNet/IP simulator: identity, tags
        and assembly attributes (cpppo has no Class 1 I/O)
client  runs one operation with cpppo's client (proxy) and prints one JSON
        result document
"""

import argparse
import functools
import json
import os
import signal
import socket
import subprocess
import sys
import tempfile
import time

from cpppo.server.enip import client as enip_client
from cpppo.server.enip.get_attribute import proxy, proxy_simple

ADAPTER = "cpppo"
OPERATIONS = [
    "list-identity", "list-services", "get-attribute", "set-attribute", "get-attributes-all",
    "multiple-get", "read-tag", "write-tag", "read-tags",
]
READY_FILE = "/run/ethernetip-interop/ready"
# The CIP types cpppo's simulator serves, of those the fixture format has.
TAG_TYPES = {"BOOL", "SINT", "INT", "DINT", "LINT", "USINT", "UINT", "UDINT", "ULINT", "REAL", "LREAL"}


def emit(doc, code=0):
    print(json.dumps(doc, sort_keys=True))
    sys.stdout.flush()
    return code


def load_fixture(path):
    with open(path) as f:
        return json.load(f)


def tag_values(t):
    n = t.get("count") or 1
    if "pattern" in t:
        p = t["pattern"]
        vals = [i * p["multiplier"] + p["offset"] for i in range(n)]
    else:
        vals = list(t.get("values") or [0])
    if t["type"] not in ("REAL", "LREAL"):
        vals = [int(round(v)) for v in vals]
    return vals


def own_ip():
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    try:
        s.connect(("10.255.255.255", 1))
        return s.getsockname()[0]
    except OSError:
        return socket.gethostbyname(socket.gethostname())
    finally:
        s.close()


def config_file(fx, ip):
    """cpppo's configuration: the fixture's identity and the interface."""
    i = fx["identity"]
    major, minor = (int(x) for x in i["revision"].split("."))
    return (
        "[Identity]\n"
        "Vendor Number = %d\nDevice Type = %d\nProduct Code Number = %d\n"
        "Product Revision = %d\nStatus Word = 0\nSerial Number = %d\n"
        "Product Name = %s\nState = 3\n\n"
        "[TCPIP]\nInterface Configuration = {\"ip_address\": \"%s\", \"network_mask\": \"255.255.0.0\"}\n"
    ) % (i["vendorId"], i["deviceType"], i["productCode"], major | (minor << 8),
         i["serialNumber"], i["productName"], ip)


def fixture_problem(fx):
    """Why cpppo cannot serve the fixture, or None."""
    if fx.get("schemaVersion") != "1.0":
        return "schemaVersion must be 1.0"
    for k in ("vendorId", "deviceType", "productCode", "revision", "serialNumber", "productName"):
        if k not in fx["identity"]:
            return "identity.%s is missing" % k
    for k in ("vendorId", "deviceType", "productCode"):
        if fx["identity"][k] > 32767:
            # cpppo 5.2.5 packs these UINTs of the ListIdentity reply as signed
            # 16-bit integers and fails to answer with a larger value.
            return "identity.%s %d: cpppo answers ListIdentity only for values up to 32767" % (k, fx["identity"][k])
    if fx.get("echoOutputToInput"):
        # cpppo's assemblies are plain attributes: nothing copies one to another.
        return "echoOutputToInput: cpppo does not copy an output assembly to an input assembly"
    for t in fx.get("tags", []):
        if t.get("type") not in TAG_TYPES:
            return "tag %s: type %s" % (t.get("name"), t.get("type"))
    return None


def serve(argv):
    p = argparse.ArgumentParser(prog="ethernetip-interop server", allow_abbrev=False)
    p.add_argument("--fixture", default="/fixtures/logix/fixture.json")
    p.add_argument("--port", type=int, default=44818)
    p.add_argument("--ready-file", default=READY_FILE)
    try:
        args = p.parse_args(argv)
    except SystemExit:
        return 2
    try:
        fx = load_fixture(args.fixture)
        problem = fixture_problem(fx)
    except (OSError, ValueError, KeyError, TypeError, AttributeError) as e:
        problem = "%s: %s" % (type(e).__name__, e)
    if problem:
        print("fixture %s: %s" % (args.fixture, problem), file=sys.stderr)
        return 4
    ip = own_ip()
    cfg = tempfile.NamedTemporaryFile("w", suffix=".cfg", delete=False)
    cfg.write(config_file(fx, ip))
    cfg.close()
    tags = []
    for t in fx.get("tags", []):
        n = t.get("count") or 1
        tags.append("%s=%s%s" % (t["name"], t["type"], "[%d]" % n if n > 1 else ""))
    for a in fx.get("assemblies", []):
        if a["size"] > 0:
            tags.append("Assembly%d@0x04/%d/3=USINT[%d]" % (a["instance"], a["instance"], a["size"]))
    cmd = [sys.executable, "-I", "-m", "cpppo.server.enip", "--address", "0.0.0.0:%d" % args.port,
           "--config", cfg.name] + tags
    # A session of its own: cpppo signals its whole process group when it stops.
    srv = subprocess.Popen(cmd, stdout=sys.stderr, stderr=sys.stderr, start_new_session=True)

    # The handler only notes the signal; the loops below stop the simulator.
    # (Waiting for it inside the handler would nest in Popen.wait.)
    stopping = []
    signal.signal(signal.SIGTERM, lambda *_: stopping.append(True))
    signal.signal(signal.SIGINT, lambda *_: stopping.append(True))

    def stop():
        if args.ready_file:
            try:
                os.unlink(args.ready_file)
            except OSError:
                pass
        srv.terminate()
        try:
            srv.wait(5)
        except subprocess.TimeoutExpired:
            srv.kill()
            srv.wait()
        emit({"event": "stopped", "adapter": ADAPTER})
        return 0

    deadline = time.time() + 30
    while True:
        if stopping:
            return stop()
        if srv.poll() is not None:
            print("cpppo exited", file=sys.stderr)
            return 1
        try:
            socket.create_connection(("127.0.0.1", args.port), 0.5).close()
            break
        except OSError:
            if time.time() > deadline:
                srv.kill()
                return 1
            time.sleep(0.1)
    # The tags start at zero: write the fixture's values.
    writes = []
    for t in fx.get("tags", []):
        vals = tag_values(t)
        rng = "[0-%d]" % (len(vals) - 1) if (t.get("count") or 1) > 1 else ""
        writes.append("%s%s=(%s)%s" % (t["name"], rng, t["type"], ",".join(str(v) for v in vals)))
    if writes:
        via = proxy_simple("127.0.0.1", port=args.port)
        with via:
            for chunk in range(0, len(writes), 4):
                res = list(via.read(writes[chunk:chunk + 4]))
                if not all(r is True for r in res):
                    print("initial values not written: %r" % res, file=sys.stderr)
                    srv.kill()
                    return 4
    if args.ready_file:
        open(args.ready_file, "w").close()
    emit({"event": "ready", "adapter": ADAPTER, "address": "%s:%d" % (ip, args.port), "fixture": fx["name"]})
    while not stopping:
        if srv.poll() is not None:
            print("cpppo exited", file=sys.stderr)
            return 1
        time.sleep(0.1)
    return stop()


def identity_doc(i):
    rev = i.get("product_revision") or 0
    return {
        "vendorId": i.get("vendor_id"), "deviceType": i.get("device_type"), "productCode": i.get("product_code"),
        "revision": "%d.%d" % (rev & 0xFF, rev >> 8), "status": i.get("status_word"),
        "serialNumber": i.get("serial_number"), "productName": i.get("product_name"), "state": i.get("state"),
        "socketAddress": "%s:%s" % (i.get("sin_addr"), i.get("sin_port")),
    }


def details(via, ops):
    """Runs operations, returning (value, status) per operation."""
    out = []
    with via:
        for val, (sts, _) in via.read_details(ops):
            out.append((val, sts))
    return out


def run_client(op, argv):
    p = argparse.ArgumentParser(prog="ethernetip-interop client " + op, allow_abbrev=False)
    p.add_argument("--host", required=True)
    p.add_argument("--port", type=int, default=44818)
    p.add_argument("--timeout-ms", type=int, default=5000)
    p.add_argument("--transport", default="tcp", choices=["tcp", "udp"])
    p.add_argument("--class", dest="cls", type=lambda s: int(s, 0), default=0)
    p.add_argument("--instance", type=lambda s: int(s, 0), default=0)
    p.add_argument("--attribute", type=lambda s: int(s, 0), default=0)
    p.add_argument("--data", default="")
    p.add_argument("--path", action="append", default=[])
    p.add_argument("--route", default="")
    p.add_argument("--tag", default="")
    p.add_argument("--tags", default="")
    p.add_argument("--count", type=int, default=1)
    p.add_argument("--type", default="")
    p.add_argument("--values", default="")
    try:
        a = p.parse_args(argv)
    except SystemExit:
        return 2
    timeout = a.timeout_ms / 1000.0
    doc = {"schemaVersion": "1.0", "adapter": ADAPTER, "operation": op, "ok": False, "error": None}
    try:
        if op == "list-identity":
            if a.transport == "udp":
                conn = enip_client.client(host=a.host, port=a.port, udp=True, timeout=timeout)
                with conn:
                    conn.list_identity(timeout=timeout)
                    rsp, _ = enip_client.await_response(conn, timeout=timeout)
                if not rsp:
                    doc["error"] = {"code": "timeout", "message": "no ListIdentity reply"}
                    return emit(doc, 1)
                ident = rsp.enip.CIP.list_identity.CPF.item[0].identity_object
            else:
                ident = proxy(a.host, port=a.port, timeout=timeout).list_identity()
            doc["identity"] = identity_doc(dict(ident))
        elif op == "list-services":
            conn = enip_client.client(host=a.host, port=a.port, udp=(a.transport == "udp"), timeout=timeout)
            with conn:
                conn.list_services(timeout=timeout)
                rsp, _ = enip_client.await_response(conn, timeout=timeout)
            if not rsp:
                doc["error"] = {"code": "timeout", "message": "no ListServices reply"}
                return emit(doc, 1)
            doc["services"] = [{"name": i.get("communications_service.service_name"),
                                "version": i.get("communications_service.version"),
                                "capabilities": i.get("communications_service.capability")}
                               for i in rsp.enip.CIP.list_services.CPF.item]
        elif op in ("get-attribute", "get-attributes-all", "set-attribute", "multiple-get"):
            if op == "get-attribute":
                ops = [("@%d/%d/%d" % (a.cls, a.instance, a.attribute), "USINT")]
            elif op == "get-attributes-all":
                ops = [("@%d/%d" % (a.cls, a.instance), "USINT")]
            elif op == "set-attribute":
                data = bytes.fromhex(a.data)
                # With a type, cpppo uses the attribute services (Set_Attribute_Single);
                # a bare string would be sent as Write Tag.
                ops = [("@%d/%d/%d=(USINT)%s" % (a.cls, a.instance, a.attribute, ",".join(str(b) for b in data)), "USINT")]
            else:
                ops = [(x, "USINT") for x in a.path]
            via = proxy_simple(a.host, port=a.port, timeout=timeout, multiple=(op == "multiple-get"))
            res = details(via, ops)
            if op == "multiple-get":
                doc["replies"] = [{"status": sts, "data": bytes(v).hex() if isinstance(v, list) else ""} for v, sts in res]
                doc["status"] = 0
            else:
                val, sts = res[0]
                doc["status"] = sts
                doc["data"] = bytes(val).hex() if isinstance(val, list) else ""
        else:
            route = a.route or None
            via = proxy(a.host, port=a.port, timeout=timeout, route_path=route) if route else proxy(a.host, port=a.port, timeout=timeout)
            if op == "read-tag":
                rng = "[0-%d]" % (a.count - 1) if a.count > 1 else ""
                if a.count > 1:
                    # Arrays are read with Read Tag Fragmented (cpppo's -f).
                    # cpppo sends one request per operation and leaves the
                    # continuation to its caller, which gives the byte offset
                    # ("Tag[0-299]+476"): the adapter continues while the
                    # target answers partial transfer (0x06).
                    via.operations_parser = functools.partial(enip_client.parse_operations, fragment=True)
                    vals, sts, offset = [], 6, 0
                    while sts == 6:
                        val, sts = details(via, ["%s%s+%d" % (a.tag, rng, offset)])[0]
                        if not isinstance(val, list) or not val:
                            break
                        vals += val
                        offset = len(vals) * {"SINT": 1, "INT": 2, "DINT": 4, "REAL": 4, "LINT": 8, "LREAL": 8}.get(a.type or "DINT", 4)
                    val = vals
                else:
                    val, sts = details(via, [a.tag + rng])[0]
                doc.update(status=sts, values=val if isinstance(val, list) else [])
            elif op == "read-tags":
                res = details(via, a.tags.split(","))
                doc["results"] = [{"tag": t, "status": sts, "values": v if isinstance(v, list) else []}
                                  for t, (v, sts) in zip(a.tags.split(","), res)]
            elif op == "write-tag":
                vals = a.values.split(",")
                rng = "[0-%d]" % (len(vals) - 1) if len(vals) > 1 else ""
                val, sts = details(via, ["%s%s=(%s)%s" % (a.tag, rng, a.type, ",".join(vals))])[0]
                doc["status"] = sts
        doc["ok"] = True
        return emit(doc)
    except Exception as e:  # pylint: disable=broad-except
        doc["error"] = {"code": "failed", "message": "%s: %s" % (type(e).__name__, e)}
        return emit(doc, 1)


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
        print("usage: ethernetip-interop server|client <op>|print-capabilities|print-fixture|version", file=sys.stderr)
        return 2
    cmd = argv[0]
    if cmd == "server":
        return serve(argv[1:])
    if cmd == "client":
        if len(argv) < 2 or argv[1] not in OPERATIONS:
            print("unknown operation", file=sys.stderr)
            return 2
        return run_client(argv[1], argv[2:])
    if cmd == "print-capabilities":
        return emit({
            "schemaVersion": "1.0", "adapter": ADAPTER,
            "adapterVersion": os.environ.get("ETHERNETIP_INTEROP_ADAPTER_VERSION", "dev"), "protocol": "ethernetip",
            "upstream": {"name": "cpppo", "version": os.environ.get("ETHERNETIP_INTEROP_UPSTREAM_VERSION", ""),
                         "revision": os.environ.get("ETHERNETIP_INTEROP_UPSTREAM_REVISION", ""),
                         "license": "GPL-3.0", "language": "Python", "independentEngine": True},
            "roles": {"server": True, "client": True}, "fixtures": ["logix"],
            "clientOperations": OPERATIONS,
            "clientFeatures": {"listIdentityTcp": True, "listIdentityUdp": True, "listServices": True,
                               "getAttributeSingle": True, "setAttributeSingle": True, "getAttributesAll": True,
                               "multipleService": True, "unconnectedSend": True, "class3": False, "class1": False,
                               "largeForwardOpen": False, "multicast": False, "inputOnly": False, "listenOnly": False,
                               "logixTags": True, "readTagFragmented": True, "tagList": False},
            "serverFeatures": {"listIdentityTcp": True, "listIdentityUdp": True, "listServices": True,
                               "listInterfaces": True, "getAttributeSingle": True, "setAttributeSingle": True,
                               "getAttributesAll": True, "getAttributeList": True, "multipleService": True,
                               "unconnectedSend": True, "class3": True, "class1": False, "largeForwardOpen": True,
                               "multicast": False, "inputOnly": False, "listenOnly": False,
                               "logixTags": True, "readTagFragmented": True, "writeTagFragmented": True,
                               "readModifyWrite": False, "tagList": False, "templates": False,
                               "tcpipObject": False, "ethernetLinkObject": False, "errorsCloseSession": True},
        })
    if cmd == "print-fixture":
        return print_fixture(argv[1] if len(argv) > 1 else "logix")
    if cmd == "version":
        return emit({"adapter": ADAPTER, "adapterVersion": os.environ.get("ETHERNETIP_INTEROP_ADAPTER_VERSION", "dev"),
                     "upstream": "cpppo",
                     "upstreamVersion": os.environ.get("ETHERNETIP_INTEROP_UPSTREAM_VERSION", ""),
                     "upstreamRevision": os.environ.get("ETHERNETIP_INTEROP_UPSTREAM_REVISION", ""), "contract": "1.0"})
    print("unknown command %s" % cmd, file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
