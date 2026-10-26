// SPDX-License-Identifier: MIT
//
// ethernetip-interop adapter for node-ethernet-ip (the npm package
// ethernet-ip), client role only.
//
// Runs one operation of the container contract (docs/CONTAINER_CONTRACT.md)
// with ethernet-ip as the originator and prints one JSON result document.
'use strict';

// stdout carries the one JSON document: anything a library prints goes to
// stderr (ethernet-ip's logger is a no-op unless one is injected).
console.log = console.error;
console.info = console.error;
console.debug = console.error;
console.warn = console.error;

const dgram = require('dgram');
const fs = require('fs');
const net = require('net');

const eip = require('ethernet-ip');
// Internal modules of the package (it has no "exports" map): the encoders
// and parsers it uses itself, for the replies the PLC class does not parse.
const MessageRouter = require('ethernet-ip/dist/cip/message-router');
const { buildBatches, parseMultiServiceResponse } = require('ethernet-ip/dist/cip/batch-builder');
const { parseHeader } = require('ethernet-ip/dist/encapsulation/header');
const { parseCPF } = require('ethernet-ip/dist/encapsulation/common-packet-format');
const { getTypeName, STRING_STRUCT_HANDLE, TYPE_SIZES } = require('ethernet-ip/dist/cip/data-types');
const UPSTREAM_VERSION = require('ethernet-ip/package.json').version;

const ADAPTER = 'nodeeip';
const ENV = process.env;
const ADAPTER_VERSION = ENV.ETHERNETIP_INTEROP_ADAPTER_VERSION || 'dev';
const UPSTREAM_REVISION = ENV.ETHERNETIP_INTEROP_UPSTREAM_REVISION || '';

// The flags of each operation besides --host, --port and --timeout-ms.
// Switches have the value true.
const OPERATIONS = {
  'list-identity': { '--transport': 'string' },
  'get-attribute': { '--class': 'num', '--instance': 'num', '--attribute': 'num', '--connected': true },
  'set-attribute': { '--class': 'num', '--instance': 'num', '--attribute': 'num', '--data': 'string', '--connected': true },
  'get-attributes-all': { '--class': 'num', '--instance': 'num', '--connected': true },
  'multiple-get': { '--path': 'list' },
  identity: { '--connected': true },
  'read-tag': { '--tag': 'string', '--count': 'num', '--type': 'string', '--route': 'string' },
  'read-tags': { '--tags': 'string', '--route': 'string' },
  'write-tag': { '--tag': 'string', '--type': 'string', '--values': 'string', '--route': 'string' },
  'tag-list': { '--route': 'string' },
};

const ELEMENTARY = ['BOOL', 'SINT', 'INT', 'DINT', 'LINT', 'USINT', 'UINT', 'UDINT', 'ULINT', 'REAL', 'LREAL'];

class UsageError extends Error {}

function emit(doc, code) {
  process.stdout.write(JSON.stringify(doc) + '\n', () => process.exit(code));
}

function usage(msg) {
  process.stderr.write(msg + '\n');
  process.exit(2);
}

function parseNum(flag, s) {
  if (!/^(0[xX][0-9a-fA-F]+|[0-9]+)$/.test(s)) throw new UsageError(`${flag}: not a number: ${s}`);
  return Number(s);
}

function parseArgs(op, argv) {
  const allowed = Object.assign({ '--host': 'string', '--port': 'num', '--timeout-ms': 'num' }, OPERATIONS[op]);
  const a = { '--port': 44818, '--timeout-ms': 5000, '--class': 0, '--instance': 0, '--attribute': 0, '--count': 1, '--path': [] };
  for (let i = 0; i < argv.length; i++) {
    const f = argv[i];
    const kind = allowed[f];
    if (kind === undefined) throw new UsageError(`unknown flag ${f}`);
    if (kind === true) { a[f] = true; continue; }
    if (i + 1 >= argv.length) throw new UsageError(`${f} needs a value`);
    const v = argv[++i];
    if (kind === 'num') a[f] = parseNum(f, v);
    else if (kind === 'list') a[f].push(v);
    else a[f] = v;
  }
  if (!a['--host']) throw new UsageError('--host is required');
  if (a['--port'] < 1 || a['--port'] > 65535) throw new UsageError('--port out of range');
  if (a['--timeout-ms'] < 1) throw new UsageError('--timeout-ms must be positive');
  if (a['--transport'] !== undefined && !['tcp', 'udp'].includes(a['--transport'])) throw new UsageError('--transport: tcp or udp');
  if (a['--data'] !== undefined && !/^([0-9a-fA-F]{2})*$/.test(a['--data'])) throw new UsageError('--data: not hex');
  if (a['--route'] !== undefined) {
    // The only route ethernet-ip takes is the backplane slot of the
    // Forward_Open connection path: 1/<slot>.
    const m = /^1\/([0-9]{1,3})$/.exec(a['--route']);
    if (!m || Number(m[1]) > 255) throw new UsageError('--route: only 1/<slot> (backplane port 1)');
    a.slot = Number(m[1]);
  }
  if (op === 'read-tag' && !a['--tag']) throw new UsageError('--tag is required');
  if (op === 'read-tag' && a['--count'] < 1) throw new UsageError('--count must be at least 1');
  if (op === 'read-tags' && !a['--tags']) throw new UsageError('--tags is required');
  if (op === 'write-tag') {
    if (!a['--tag'] || !a['--type'] || a['--values'] === undefined) throw new UsageError('--tag, --type and --values are required');
    if (![...ELEMENTARY, 'STRING', 'JSON'].includes(a['--type'])) throw new UsageError('--type: unknown type');
  }
  if (op === 'multiple-get') {
    if (a['--path'].length === 0) throw new UsageError('--path is required');
    // One Multiple Service Packet of at most 504 octets.
    if (a['--path'].length > 32) throw new UsageError('--path: at most 32');
    a.paths = a['--path'].map((p) => {
      const m = /^@([^/]+)\/([^/]+)\/([^/]+)$/.exec(p);
      if (!m) throw new UsageError(`--path: not @CLASS/INSTANCE/ATTRIBUTE: ${p}`);
      return [parseNum('--path', m[1]), parseNum('--path', m[2]), parseNum('--path', m[3])];
    });
  }
  return a;
}

// write-tag values: numbers of an elementary type, comma-separated (LINT and
// ULINT as BigInt, which ethernet-ip's 64-bit codecs take); BOOL true/false
// or 1/0; one STRING; or one JSON value (a structure as an object).
function writeValues(type, text) {
  if (type === 'STRING') return [text];
  if (type === 'JSON') {
    try { return [JSON.parse(text)]; } catch (e) { throw new UsageError(`--values: not JSON: ${e.message}`); }
  }
  return text.split(',').map((s) => {
    s = s.trim();
    if (type === 'BOOL') {
      if (!['true', 'false', '1', '0'].includes(s.toLowerCase())) throw new UsageError(`--values: not a BOOL: ${s}`);
      return ['true', '1'].includes(s.toLowerCase());
    }
    if (type === 'REAL' || type === 'LREAL') {
      const v = Number(s);
      if (s === '' || Number.isNaN(v)) throw new UsageError(`--values: not a number: ${s}`);
      return v;
    }
    if (!/^-?(0[xX][0-9a-fA-F]+|[0-9]+)$/.test(s)) throw new UsageError(`--values: not an integer: ${s}`);
    const neg = s.startsWith('-');
    const big = BigInt(neg ? s.slice(1) : s) * (neg ? -1n : 1n);
    return type === 'LINT' || type === 'ULINT' ? big : Number(big);
  });
}

// A value as JSON: 64-bit integers as numbers (as strings beyond 2^53),
// unparsed structures (Buffer) as hex.
function jsonable(v) {
  if (typeof v === 'bigint') return Number.isSafeInteger(Number(v)) ? Number(v) : v.toString();
  if (Buffer.isBuffer(v)) return v.toString('hex');
  if (Array.isArray(v)) return v.map(jsonable);
  if (v && typeof v === 'object') {
    const o = {};
    for (const [k, x] of Object.entries(v)) o[k] = jsonable(x);
    return o;
  }
  return v;
}

// ethernet-ip connects to port 44818 always: a transport that connects to
// --port instead (the PLC class takes any ITransport).
class PortTransport extends eip.TCPTransport {
  constructor(port) { super(); this.port = port; }
  connect(host, _port, timeoutMs) { return super.connect(host, this.port, timeoutMs); }
}

// A PLC session: RegisterSession, and with connected messaging a
// Forward_Open (Large first, then small) to the backplane slot.
async function openPLC(a, connected) {
  const timeout = a['--timeout-ms'];
  const plc = new eip.PLC({ transport: new PortTransport(a['--port']) });
  plc.on('error', () => {}); // reported through the failing request
  await plc.connect(a['--host'], { slot: a.slot ?? 0, connected, timeout, autoReconnect: false });
  // Each request waits at most --timeout-ms: ethernet-ip's own request
  // timeout is 10 s (the tag list and template uploads pass 10 s too).
  const pipeline = plc.session && plc.session.pipeline;
  if (pipeline) {
    const send = pipeline.send.bind(pipeline);
    pipeline.send = (data, t) => send(data, Math.min(t ?? timeout, timeout));
  }
  return plc;
}

// A raw CIP request through the session: ethernet-ip builds generic requests
// (buildGenericCIPMessage) and sends them with the PLC's sendCIP, over the
// Class 3 connection (SendUnitData) when there is one, else SendRRData to the
// target's message router. sendCIP is not part of the typed API.
async function sendRaw(plc, request) {
  return MessageRouter.parse(await plc.sendCIP(request));
}

function statusFields(doc, mr) {
  doc.status = mr.generalStatusCode;
  doc.extendedStatus = mr.extendedStatus;
}

// ListIdentity: ethernet-ip builds the request (buildListIdentityRequest) and
// parses the identity item (parseListIdentityResponse) but has no sender:
// the adapter sends it on one TCP connection or as one UDP datagram.
function listIdentity(a) {
  const host = a['--host'];
  const port = a['--port'];
  const timeout = a['--timeout-ms'];
  const request = eip.buildListIdentityRequest();
  return new Promise((resolve, reject) => {
    let done = false;
    let timer;
    let close = () => {};
    const finish = (err, reply) => {
      if (done) return;
      done = true;
      clearTimeout(timer);
      close();
      if (err) reject(err); else resolve(reply);
    };
    timer = setTimeout(() => finish(Object.assign(new Error('no ListIdentity reply'), { code: 'timeout' })), timeout);
    if (a['--transport'] === 'udp') {
      const sock = dgram.createSocket(net.isIPv6(host) ? 'udp6' : 'udp4');
      close = () => sock.close();
      sock.on('error', (e) => finish(Object.assign(e, { code: 'failed' })));
      sock.on('message', (msg) => finish(null, msg));
      sock.send(request, port, host);
    } else {
      const sock = net.connect({ host, port });
      close = () => sock.destroy();
      let buf = Buffer.alloc(0);
      sock.on('connect', () => sock.write(request));
      sock.on('error', (e) => finish(Object.assign(e, { code: 'connect-failed' })));
      sock.on('close', () => finish(Object.assign(new Error('connection closed before the reply'), { code: 'failed' })));
      sock.on('data', (d) => {
        buf = Buffer.concat([buf, d]);
        if (buf.length >= 24 && buf.length >= 24 + buf.readUInt16LE(2)) finish(null, buf.subarray(0, 24 + buf.readUInt16LE(2)));
      });
    }
  }).then((reply) => {
    const h = parseHeader(reply);
    if (h.statusCode !== 0) throw Object.assign(new Error(`encapsulation status 0x${h.statusCode.toString(16)}`), { code: 'protocol' });
    const item = parseCPF(h.data).find((i) => i.typeId === 0x0c);
    if (!item) throw Object.assign(new Error('no identity item in the reply'), { code: 'protocol' });
    const d = eip.parseListIdentityResponse(item.data);
    return {
      vendorId: d.vendorId,
      deviceType: d.deviceType,
      productCode: d.productCode,
      revision: d.revision,
      status: d.status,
      serialNumber: parseInt(d.serialNumber, 16),
      productName: d.productName,
      state: d.state,
      // ethernet-ip parses the socket address's IP, not its port.
      socketAddress: d.address,
    };
  });
}

// The type name of a registry entry: elementary, STRING, or the template's.
function typeName(plc, tag) {
  const base = tag.replace(/\.\d{1,2}$/, '');
  const entry = plc.registry.lookup(tag) || plc.registry.lookup(base);
  if (!entry) return null;
  if (/\.\d{1,2}$/.test(tag) && !entry.isStruct) return 'BOOL';
  if (entry.isStruct) {
    if (entry.type === STRING_STRUCT_HANDLE) return 'STRING';
    const t = plc.registry.lookupTemplateByHandle(entry.type) || plc.registry.lookupTemplate(entry.type);
    return t ? t.name : null;
  }
  return getTypeName(entry.type);
}

// Element tags of an array from --tag on: "Samples" (from element 0) or
// "Samples[10]" (from element 10).
function elements(tag, n) {
  const m = /^(.*)\[(\d+)\]$/.exec(tag);
  const base = m ? m[1] : tag;
  const first = m ? Number(m[2]) : 0;
  return Array.from({ length: n }, (_, i) => `${base}[${first + i}]`);
}

// ethernet-ip learns a tag's type by reading it before it reads it in a
// batch or writes it. The first element is read (or, for a write, its type
// learnt) through the library; the other elements get the same type through
// its public registry (registry.define), so they need no reads of their own.
function defineLike(plc, first, others) {
  const e = plc.registry.lookup(first);
  if (!e) return;
  for (const t of others) {
    if (!plc.registry.has(t)) plc.registry.define(t, e.type, e.size, e.isStruct);
  }
}

// Element reads and writes go to the library CHUNK at a time. It packs a
// batch into Multiple Service Packets by size only (up to about 220 element
// reads in 4002 octets); a target may bound the services of one packet (the
// Go interop node answers more than 200 with 0x02).
const CHUNK = 100;
function chunks(list) {
  const out = [];
  for (let i = 0; i < list.length; i += CHUNK) out.push(list.slice(i, i + CHUNK));
  return out;
}

// A batch read. ethernet-ip sizes the replies of a batch from the tags'
// templates, also for STRING (0x0FCE), whose template its lazy discovery
// does not fetch: it fails with "Missing template for struct type 0xfce"
// before it sends anything. Then the adapter uploads the tag list and the
// templates (plc.discover(), as connect({ discover: true }) would) and
// reads again.
async function readBatch(plc, tags) {
  try {
    return await plc.read(tags);
  } catch (e) {
    if (!/^Missing template/.test(e.message)) throw e;
    await plc.discover();
    return plc.read(tags);
  }
}

async function run(op, a) {
  const doc = { schemaVersion: '1.0', adapter: ADAPTER, operation: op, ok: false, error: null };
  if (op === 'list-identity') {
    doc.identity = await listIdentity(a);
    doc.ok = true;
    return doc;
  }
  const generic = ['get-attribute', 'set-attribute', 'get-attributes-all', 'multiple-get', 'identity'].includes(op);
  // Generic CIP: unconnected unless --connected. Logix tag services: the
  // library's default, connected messaging over a Forward_Open.
  const plc = await openPLC(a, generic ? !!a['--connected'] : true);
  try {
    switch (op) {
      case 'get-attribute':
      case 'set-attribute':
      case 'get-attributes-all': {
        const service = { 'get-attribute': 0x0e, 'set-attribute': 0x10, 'get-attributes-all': 0x01 }[op];
        const req = eip.buildGenericCIPMessage(service, a['--class'], a['--instance'],
          op === 'get-attributes-all' ? undefined : a['--attribute'],
          op === 'set-attribute' ? Buffer.from(a['--data'], 'hex') : undefined);
        const mr = await sendRaw(plc, req);
        statusFields(doc, mr);
        doc.data = mr.data.toString('hex');
        break;
      }
      case 'multiple-get': {
        // The library's batch builder packs the requests into a Multiple
        // Service Packet to the message router (one request alone it sends
        // as it is).
        const reqs = a.paths.map(([c, i, at]) => ({ serviceData: eip.buildGenericCIPMessage(0x0e, c, i, at), estimatedResponseSize: 0 }));
        const batches = buildBatches(reqs, 504, plc.session.connectionId !== 0);
        if (batches.length !== 1) throw new Error('the requests do not fit one Multiple Service Packet');
        const mr = await sendRaw(plc, batches[0].data);
        statusFields(doc, mr);
        if (reqs.length === 1) {
          doc.replies = [{ status: mr.generalStatusCode, data: mr.data.toString('hex') }];
        } else if (mr.generalStatusCode === 0 || mr.generalStatusCode === 0x1e) {
          doc.replies = parseMultiServiceResponse(mr.data).map((r) => ({ status: r.generalStatusCode, data: r.data.toString('hex') }));
        } else {
          doc.replies = [];
        }
        break;
      }
      case 'identity': {
        // Get_Attributes_All of the Identity object @1/1, parsed by the
        // library's parseControllerProps.
        const mr = await sendRaw(plc, eip.buildGetControllerPropsRequest());
        statusFields(doc, mr);
        if (mr.generalStatusCode === 0) doc.module = eip.parseControllerProps(mr.data);
        else doc.module = {};
        break;
      }
      case 'read-tag': {
        try {
          if (a['--count'] === 1) {
            const v = await plc.read(a['--tag']);
            doc.values = [jsonable(v)];
            doc.type = typeName(plc, a['--tag']);
          } else {
            const tags = elements(a['--tag'], a['--count']);
            const first = await plc.read(tags[0]);
            defineLike(plc, tags[0], tags.slice(1));
            const rest = [];
            for (const part of chunks(tags.slice(1))) rest.push(...(await readBatch(plc, part)));
            doc.values = jsonable([first, ...rest]);
            doc.type = typeName(plc, tags[0]);
          }
          doc.status = 0;
          doc.errorText = null;
        } catch (e) {
          if (!(e instanceof eip.CIPError)) throw e;
          doc.status = e.generalStatusCode;
          doc.extendedStatus = e.extendedStatus;
          doc.errorText = e.statusMessage;
          doc.values = [];
        }
        break;
      }
      case 'read-tags': {
        const tags = a['--tags'].split(',');
        try {
          const vals = await readBatch(plc, tags);
          doc.results = tags.map((t, i) => ({ tag: t, values: [jsonable(vals[i])], type: typeName(plc, t) }));
          doc.status = 0;
          doc.errorText = null;
        } catch (e) {
          // ethernet-ip fails the whole read on the first tag in error.
          if (!(e instanceof eip.CIPError)) throw e;
          doc.results = [];
          doc.status = e.generalStatusCode;
          doc.extendedStatus = e.extendedStatus;
          doc.errorText = e.statusMessage;
        }
        break;
      }
      case 'write-tag': {
        const vals = writeValues(a['--type'], a['--values']);
        try {
          if (vals.length === 1) {
            await plc.write(a['--tag'], vals[0]);
          } else {
            const tags = elements(a['--tag'], vals.length);
            await plc.write(tags[0], vals[0]);
            defineLike(plc, tags[0], tags.slice(1));
            let i = 1;
            for (const part of chunks(tags.slice(1))) {
              const rest = {};
              for (const t of part) rest[t] = vals[i++];
              await plc.write(rest);
            }
          }
          doc.status = 0;
          doc.errorText = null;
        } catch (e) {
          if (!(e instanceof eip.CIPError)) throw e;
          doc.status = e.generalStatusCode;
          doc.extendedStatus = e.extendedStatus;
          doc.errorText = e.statusMessage;
        }
        break;
      }
      case 'tag-list': {
        const tags = await plc.discover();
        doc.tags = tags.map((t) => {
          const name = t.program ? `Program:${t.program}.${t.name}` : t.name;
          let type;
          if (!t.type.isStruct) type = getTypeName(t.type.code);
          else if (t.type.code === STRING_STRUCT_HANDLE || (t.template && t.template.name === 'STRING')) type = 'STRING';
          else type = t.template ? t.template.name : null;
          return { name, type, dims: t.type.dimSizes };
        }).sort((x, y) => (x.name < y.name ? -1 : x.name > y.name ? 1 : 0));
        // ethernet-ip reads no controller or program name.
        doc.programName = null;
        break;
      }
      default:
        throw new UsageError(`unknown operation ${op}`);
    }
    doc.ok = true;
    return doc;
  } finally {
    // Forward_Close (when connected) and UnregisterSession, bounded.
    await Promise.race([plc.disconnect().catch(() => {}), new Promise((r) => setTimeout(r, a['--timeout-ms']).unref())]);
  }
}

function errorCode(e) {
  if (typeof e.code === 'string' && ['connect-failed', 'timeout', 'protocol', 'failed'].includes(e.code)) return e.code;
  if (e instanceof eip.TimeoutError) return 'timeout';
  if (e instanceof eip.SessionError) return 'connect-failed';
  if (e instanceof eip.ConnectionError) {
    if (/^TCP connect failed/.test(e.message)) return /timed out/.test(e.message) ? 'timeout' : 'connect-failed';
    if (/Transport closed/.test(e.message)) return 'connection-lost';
    if (/Request timed out|timed out/.test(e.message)) return 'timeout';
  }
  return 'failed';
}

function printFixture(name) {
  if (!name || name.includes('/') || name.includes('\\') || name.includes('..')) usage(`invalid fixture name ${name}`);
  let data;
  try {
    data = fs.readFileSync(`/fixtures/${name}/fixture.json`);
  } catch (e) {
    usage(`no fixture ${name}`);
  }
  process.stdout.write(data, () => process.exit(0));
}

function capabilities() {
  return {
    schemaVersion: '1.0',
    adapter: ADAPTER,
    adapterVersion: ADAPTER_VERSION,
    protocol: 'ethernetip',
    upstream: { name: 'node-ethernet-ip', version: UPSTREAM_VERSION, revision: UPSTREAM_REVISION, license: 'MIT', language: 'TypeScript', independentEngine: true },
    roles: { server: false, client: true },
    fixtures: [],
    clientOperations: Object.keys(OPERATIONS),
    clientFeatures: {
      listIdentityTcp: true, listIdentityUdp: true, listServices: false,
      getAttributeSingle: true, setAttributeSingle: true, getAttributesAll: true,
      multipleService: true, unconnectedSend: false, class3: true, class1: false,
      largeForwardOpen: true, multicast: false, inputOnly: false, listenOnly: false,
      logixTags: true, readTagFragmented: false, writeTagFragmented: false,
      readModifyWrite: true, tagList: true, templates: true, configData: false,
    },
    serverFeatures: {},
  };
}

function main(argv) {
  if (argv.length === 0) usage('usage: ethernetip-interop client <op> --host HOST ... | print-capabilities | print-fixture [NAME] | version');
  const cmd = argv[0];
  if (cmd === 'print-capabilities') return emit(capabilities(), 0);
  if (cmd === 'version') {
    return emit({ adapter: ADAPTER, adapterVersion: ADAPTER_VERSION, upstream: 'node-ethernet-ip', upstreamVersion: UPSTREAM_VERSION, upstreamRevision: UPSTREAM_REVISION, contract: '1.0' }, 0);
  }
  if (cmd === 'print-fixture') return printFixture(argv.length > 1 ? argv[1] : 'adapter');
  if (cmd === 'server') usage('the nodeeip adapter has no server role');
  if (cmd !== 'client' || argv.length < 2 || !Object.prototype.hasOwnProperty.call(OPERATIONS, argv[1])) usage('unknown command or operation');
  const op = argv[1];
  let a;
  try {
    a = parseArgs(op, argv.slice(2));
    if (op === 'write-tag') writeValues(a['--type'], a['--values']);
  } catch (e) {
    usage(e.message);
  }
  // Every wait is bounded: a watchdog in case a request is never settled.
  const watchdog = setTimeout(() => {
    emit({ schemaVersion: '1.0', adapter: ADAPTER, operation: op, ok: false, error: { code: 'timeout', message: 'operation did not complete' } }, 1);
  }, a['--timeout-ms'] * 10 + 5000);
  watchdog.unref();
  run(op, a).then(
    (doc) => emit(doc, 0),
    (e) => {
      if (e instanceof UsageError) usage(e.message);
      emit({ schemaVersion: '1.0', adapter: ADAPTER, operation: op, ok: false, error: { code: errorCode(e), message: `${e.name || 'Error'}: ${e.message}` } }, 1);
    },
  );
}

process.on('unhandledRejection', (e) => process.stderr.write(`unhandled rejection: ${e && e.stack}\n`));
main(process.argv.slice(2));
