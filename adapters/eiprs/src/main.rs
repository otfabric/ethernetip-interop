// SPDX-License-Identifier: MIT
//
// ethernetip-interop adapter for rseip (Joylei/eip-rs), client role only.
//
// Runs one operation of the container contract (docs/CONTAINER_CONTRACT.md)
// with rseip as the originator and prints one JSON result document on
// stdout. Diagnostics go to stderr.
//
// What rseip does and what the adapter adds:
// - Encapsulation (sessions, SendRRData, SendUnitData, ListIdentity,
//   ListServices, UDP discovery), Unconnected_Send, Forward_Open/Close,
//   EPATH and Logix symbolic segment encoding, the Logix services (Read Tag,
//   Write Tag, the fragmented services, Read-Modify-Write, the Symbol
//   object's instance list, template upload) and the Multiple Service Packet
//   request are rseip's.
// - rseip's own client always wraps unconnected requests in Unconnected_Send.
//   Without --route the adapter sends a plain SendRRData request through
//   rseip's EipContext instead, as the other originators do.
// - rseip turns every non-zero CIP status into an error. The generic
//   operations decode the reply with their own reply type (service, status,
//   additional status, data) so the status and data are reported; the Logix
//   operations recover the status from rseip's error.
// - Logix values (elementary types, STRING, structures) are converted by the
//   adapter: rseip hands over the raw tag data and the template definitions.

use std::{collections::HashMap, future::Future, io, net::Ipv4Addr, net::SocketAddrV4, time::Duration};

use bytes::{Buf, BufMut, Bytes, BytesMut};
use futures_util::{Stream, StreamExt};
use rseip::{
    cip::{
        codec::decode::message_reply::decode_unconnected_send,
        epath::{EPath, PortSegment, Segment},
        identity::IdentityObject,
        service::{CommonServices, MessageService},
        ListServiceItem, MessageReplyInterface, MessageRequest, Status,
    },
    client::{
        ab_eip::{
            AbService, AbTemplateService, PathParser, ReadFragmentedRequest, ReadModifyWriteRequest,
            SymbolInstance, TagType, TagValue, WriteFragmentedRequest,
        },
        AbEipClient, AbEipConnection, EipClient, EipConnection, EipContext, EipDiscovery, OpenOptions,
    },
    ClientError,
};
use rseip_core::codec::{BytesHolder, Decode, Decoder, Encode, LittleEndianDecoder};
use serde_json::{json, Map, Value};
use tokio::net::TcpStream;

const ADAPTER: &str = "eiprs";
const OPERATIONS: &[&str] = &[
    "list-identity", "list-services", "get-attribute", "set-attribute", "get-attributes-all", "multiple-get",
    "read-tag", "read-tags", "write-tag", "tag-list",
];
/// Data octets per Write Tag request; larger writes use Write Tag Fragmented.
const WRITE_CHUNK: usize = 400;

// ---------------------------------------------------------------------------
// Errors

/// A failed operation: the contract's error code and a message.
#[derive(Debug)]
struct OpErr {
    code: &'static str,
    msg: String,
}

impl OpErr {
    fn new(code: &'static str, msg: impl Into<String>) -> Self {
        Self { code, msg: msg.into() }
    }
}

impl From<ClientError> for OpErr {
    fn from(e: ClientError) -> Self {
        let code = match &e {
            ClientError::Io { err, .. } => match err.kind() {
                io::ErrorKind::ConnectionRefused
                | io::ErrorKind::HostUnreachable
                | io::ErrorKind::NetworkUnreachable
                | io::ErrorKind::AddrNotAvailable
                | io::ErrorKind::NotFound => "connect-failed",
                io::ErrorKind::ConnectionReset
                | io::ErrorKind::ConnectionAborted
                | io::ErrorKind::BrokenPipe
                | io::ErrorKind::UnexpectedEof => "connection-lost",
                _ => "failed",
            },
            ClientError::Custom { msg, .. } if msg.contains("transport closed") => "connection-lost",
            _ => "failed",
        };
        Self::new(code, e.to_string())
    }
}

impl From<io::Error> for OpErr {
    fn from(e: io::Error) -> Self {
        OpErr::from(ClientError::from(e))
    }
}

/// The CIP general and extended status of an rseip error for a reply with a
/// non-zero status ("cip error: message reply status CIP general status: N,
/// extended status: M"), if it is one.
fn cip_status(e: &OpErr) -> Option<(u8, Option<u16>)> {
    const G: &str = "CIP general status: ";
    const X: &str = ", extended status: ";
    let i = e.msg.find(G)? + G.len();
    let digits: String = e.msg[i..].chars().take_while(|c| c.is_ascii_digit()).collect();
    let general = digits.parse().ok()?;
    let rest = &e.msg[i + digits.len()..];
    let ext = rest
        .strip_prefix(X)
        .and_then(|r| r.chars().take_while(|c| c.is_ascii_digit()).collect::<String>().parse().ok());
    Some((general, ext))
}

/// Bounds one request (or connection) by the operation's timeout.
async fn tm<T, E: Into<OpErr>>(d: Duration, f: impl Future<Output = Result<T, E>>) -> Result<T, OpErr> {
    match tokio::time::timeout(d, f).await {
        Err(_) => Err(OpErr::new("timeout", format!("no reply within {} ms", d.as_millis()))),
        Ok(r) => r.map_err(Into::into),
    }
}

// ---------------------------------------------------------------------------
// A message reply that keeps whatever the target answered

/// Reply service, general status, additional status words and reply data.
#[derive(Debug, Clone)]
struct RawReply {
    service: u8,
    status: Status,
    ext: Vec<u16>,
    data: Bytes,
}

impl<'de> Decode<'de> for RawReply {
    fn decode<D: Decoder<'de>>(mut d: D) -> Result<Self, D::Error> {
        d.ensure_size(4)?;
        let service = d.decode_u8();
        d.decode_u8();
        let general = d.decode_u8();
        let n = d.decode_u8() as usize;
        d.ensure_size(2 * n)?;
        let ext: Vec<u16> = (0..n).map(|_| d.decode_u16()).collect();
        let rem = d.remaining();
        let data = d.buf_mut().copy_to_bytes(rem);
        let r = Self { service, status: Status { general, extended: ext.first().copied() }, ext, data };
        LAST_REPLY.with(|l| *l.borrow_mut() = Some(r.clone()));
        Ok(r)
    }
}

thread_local! {
    /// The last reply decoded, also when rseip then rejects it: a reply of
    /// the router to Unconnected_Send (0xD2, its error) is an answer, not a
    /// failure, but rseip reports it as an unexpected reply service.
    static LAST_REPLY: std::cell::RefCell<Option<RawReply>> = const { std::cell::RefCell::new(None) };
}

fn take_last_reply() -> Option<RawReply> {
    LAST_REPLY.with(|l| l.borrow_mut().take())
}

impl MessageReplyInterface for RawReply {
    type Value = Bytes;
    fn reply_service(&self) -> u8 {
        self.service
    }
    fn status(&self) -> &Status {
        &self.status
    }
    fn value(&self) -> &Bytes {
        &self.data
    }
    fn into_value(self) -> Bytes {
        self.data
    }
}

impl RawReply {
    fn to_bytes(&self) -> Bytes {
        let mut b = BytesMut::new();
        b.put_u8(self.service);
        b.put_u8(0);
        b.put_u8(self.status.general);
        b.put_u8(self.ext.len() as u8);
        for x in &self.ext {
            b.put_u16_le(*x);
        }
        b.put_slice(&self.data);
        b.freeze()
    }
}

// ---------------------------------------------------------------------------
// Message services

/// Unconnected explicit messaging without Unconnected_Send: the request goes
/// to the target's message router in a SendRRData, through rseip's
/// EipContext (session, encapsulation, common packet format).
struct Direct {
    ctx: EipContext<TcpStream, ClientError>,
}

impl Direct {
    async fn connect(addr: SocketAddrV4, t: Duration) -> Result<Self, OpErr> {
        let stream = tm(t, TcpStream::connect(addr))
            .await
            .map_err(|e| if e.code == "timeout" { e } else { OpErr::new("connect-failed", e.msg) })?;
        Ok(Self { ctx: EipContext::new(stream) })
    }
}

#[async_trait::async_trait]
impl MessageService for Direct {
    type Error = ClientError;

    async fn send<'de, P, D, R>(&mut self, mr: MessageRequest<P, D>) -> Result<R, ClientError>
    where
        P: Encode + Send + Sync,
        D: Encode + Send + Sync,
        R: MessageReplyInterface + Decode<'de> + 'static,
    {
        if !self.ctx.has_session() {
            self.ctx.register_session().await?;
        }
        let code = mr.service_code;
        let cpf = self.ctx.send_rrdata(mr).await?;
        let reply: R = decode_unconnected_send(cpf)?;
        reply.expect_service::<ClientError>(code + 0x80)?;
        Ok(reply)
    }

    async fn close(&mut self) -> Result<(), ClientError> {
        self.ctx.unregister_session().await
    }

    fn closed(&self) -> bool {
        !self.ctx.has_session()
    }
}

/// The generic (non-Logix) services: direct, routed through rseip's
/// Unconnected_Send (--route), or over an rseip Class 3 connection
/// (--connected).
enum Gen {
    Direct(Direct),
    Routed(EipClient),
    Conn(EipConnection),
}

#[async_trait::async_trait]
impl MessageService for Gen {
    type Error = ClientError;

    async fn send<'de, P, D, R>(&mut self, mr: MessageRequest<P, D>) -> Result<R, ClientError>
    where
        P: Encode + Send + Sync,
        D: Encode + Send + Sync,
        R: MessageReplyInterface + Decode<'de> + 'static,
    {
        match self {
            Gen::Direct(s) => s.send(mr).await,
            Gen::Routed(s) => s.send(mr).await,
            Gen::Conn(s) => s.send(mr).await,
        }
    }

    async fn close(&mut self) -> Result<(), ClientError> {
        match self {
            Gen::Direct(s) => s.close().await,
            Gen::Routed(s) => s.close().await,
            Gen::Conn(s) => s.close().await,
        }
    }

    fn closed(&self) -> bool {
        match self {
            Gen::Direct(s) => s.closed(),
            Gen::Routed(s) => s.closed(),
            Gen::Conn(s) => s.closed(),
        }
    }
}

/// Wraps a message service and keeps the last raw reply, whatever its
/// status, for the services whose rseip reply decoding drops it (the
/// Multiple Service Packet).
struct Tap<'a, T> {
    inner: &'a mut T,
    raw: Option<RawReply>,
}

#[async_trait::async_trait]
impl<'a, T: MessageService<Error = ClientError>> MessageService for Tap<'a, T> {
    type Error = ClientError;

    async fn send<'de, P, D, R>(&mut self, mr: MessageRequest<P, D>) -> Result<R, ClientError>
    where
        P: Encode + Send + Sync,
        D: Encode + Send + Sync,
        R: MessageReplyInterface + Decode<'de> + 'static,
    {
        let raw: RawReply = self.inner.send(mr).await?;
        let bytes = raw.to_bytes();
        self.raw = Some(raw);
        R::decode(LittleEndianDecoder::<ClientError>::new(bytes))
    }

    async fn close(&mut self) -> Result<(), ClientError> {
        self.inner.close().await
    }

    fn closed(&self) -> bool {
        self.inner.closed()
    }
}

/// The Logix services: rseip's AbEipClient (Unconnected_Send, with the
/// --route path or none) or AbEipConnection (--connected).
enum Ab {
    Client(AbEipClient),
    Conn(AbEipConnection),
}

macro_rules! ab {
    ($s:expr, $c:ident => $body:expr) => {
        match $s {
            Ab::Client($c) => $body,
            Ab::Conn($c) => $body,
        }
    };
}

#[async_trait::async_trait]
impl MessageService for Ab {
    type Error = ClientError;

    async fn send<'de, P, D, R>(&mut self, mr: MessageRequest<P, D>) -> Result<R, ClientError>
    where
        P: Encode + Send + Sync,
        D: Encode + Send + Sync,
        R: MessageReplyInterface + Decode<'de> + 'static,
    {
        ab!(self, c => c.send(mr).await)
    }

    async fn close(&mut self) -> Result<(), ClientError> {
        ab!(self, c => c.close().await)
    }

    fn closed(&self) -> bool {
        ab!(self, c => c.closed())
    }
}

/// Forward_Open options of the Class 3 connection: rseip's defaults
/// (Forward_Open, 504-octet connection size, transport class 3,
/// application trigger), with the connection path --route + @2/1.
fn open_options(route: &Option<EPath>) -> OpenOptions {
    let mut path = route.clone().unwrap_or_default();
    path.push(Segment::Class(2));
    path.push(Segment::Instance(1));
    OpenOptions::default().connection_path(path)
}

// ---------------------------------------------------------------------------
// Arguments

#[derive(Default)]
struct Args {
    host: String,
    port: u16,
    timeout: Duration,
    transport: String,
    class: u16,
    instance: u16,
    attribute: u16,
    data: Vec<u8>,
    connected: bool,
    paths: Vec<(u16, u16, u16)>,
    tag: Option<String>,
    tags: Option<String>,
    count: u16,
    typ: Option<String>,
    values: Option<String>,
    route: Option<EPath>,
}

fn parse_num(flag: &str, s: &str, max: u64) -> Result<u64, String> {
    let v = if let Some(h) = s.strip_prefix("0x").or_else(|| s.strip_prefix("0X")) {
        u64::from_str_radix(h, 16)
    } else {
        s.parse::<u64>()
    };
    match v {
        Ok(v) if v <= max => Ok(v),
        _ => Err(format!("{flag}: invalid value {s}")),
    }
}

fn parse_hex(flag: &str, s: &str) -> Result<Vec<u8>, String> {
    if s.len() % 2 != 0 || !s.bytes().all(|c| c.is_ascii_hexdigit()) {
        return Err(format!("{flag}: not hexadecimal octets"));
    }
    Ok((0..s.len()).step_by(2).map(|i| u8::from_str_radix(&s[i..i + 2], 16).unwrap()).collect())
}

/// `@CLASS/INSTANCE/ATTRIBUTE`
fn parse_cip_path(s: &str) -> Result<(u16, u16, u16), String> {
    let p: Vec<&str> = s.strip_prefix('@').ok_or("--path: expected @CLASS/INSTANCE/ATTRIBUTE")?.split('/').collect();
    if p.len() != 3 {
        return Err("--path: expected @CLASS/INSTANCE/ATTRIBUTE".into());
    }
    Ok((
        parse_num("--path", p[0], 0xFFFF)? as u16,
        parse_num("--path", p[1], 0xFFFF)? as u16,
        parse_num("--path", p[2], 0xFFFF)? as u16,
    ))
}

/// `--route PORT/LINK[/PORT/LINK...]`, for example `1/0` (backplane, slot 0).
fn parse_route(s: &str) -> Result<EPath, String> {
    let p: Vec<&str> = s.split('/').collect();
    if p.is_empty() || p.len() % 2 != 0 {
        return Err("--route: expected PORT/LINK pairs, for example 1/0".into());
    }
    let mut path = EPath::default();
    for pair in p.chunks(2) {
        let port = parse_num("--route", pair[0], 0xFFFF)? as u16;
        let link = parse_num("--route", pair[1], 0xFF)? as u8;
        if port == 0 {
            return Err("--route: port 0".into());
        }
        path.push(Segment::Port(PortSegment { port, link: Bytes::copy_from_slice(&[link]) }));
    }
    Ok(path)
}

fn op_flags(op: &str) -> &'static [&'static str] {
    match op {
        "list-identity" | "list-services" => &["transport"],
        "get-attribute" => &["class", "instance", "attribute", "connected", "route"],
        "set-attribute" => &["class", "instance", "attribute", "data", "connected", "route"],
        "get-attributes-all" => &["class", "instance", "connected", "route"],
        "multiple-get" => &["path", "connected", "route"],
        "read-tag" => &["tag", "count", "type", "connected", "route"],
        "read-tags" => &["tags", "connected", "route"],
        "write-tag" => &["tag", "type", "values", "connected", "route"],
        "tag-list" => &["connected", "route"],
        _ => &[],
    }
}

fn parse_args(op: &str, argv: &[String]) -> Result<Args, String> {
    let mut a = Args { port: 44818, timeout: Duration::from_millis(5000), transport: "tcp".into(), count: 1, ..Default::default() };
    let mut host = None;
    let allowed = op_flags(op);
    let mut i = 0;
    while i < argv.len() {
        let flag = argv[i].strip_prefix("--").ok_or_else(|| format!("unexpected argument {}", argv[i]))?;
        if !["host", "port", "timeout-ms"].contains(&flag) && !allowed.contains(&flag) {
            return Err(format!("unknown flag --{flag} for {op}"));
        }
        if flag == "connected" {
            a.connected = true;
            i += 1;
            continue;
        }
        let v = argv.get(i + 1).ok_or_else(|| format!("--{flag}: missing value"))?.as_str();
        let f = format!("--{flag}");
        match flag {
            "host" => host = Some(v.to_string()),
            "port" => a.port = parse_num(&f, v, 0xFFFF)? as u16,
            "timeout-ms" => a.timeout = Duration::from_millis(parse_num(&f, v, 3_600_000)?.max(1)),
            "transport" => {
                if v != "tcp" && v != "udp" {
                    return Err("--transport: tcp or udp".into());
                }
                a.transport = v.into()
            }
            "class" => a.class = parse_num(&f, v, 0xFFFF)? as u16,
            "instance" => a.instance = parse_num(&f, v, 0xFFFF)? as u16,
            "attribute" => a.attribute = parse_num(&f, v, 0xFFFF)? as u16,
            "data" => a.data = parse_hex(&f, v)?,
            "path" => a.paths.push(parse_cip_path(v)?),
            "tag" => a.tag = Some(v.into()),
            "tags" => a.tags = Some(v.into()),
            "count" => a.count = parse_num(&f, v, 0xFFFF)?.max(1) as u16,
            "type" => a.typ = Some(v.to_ascii_uppercase()),
            "values" => a.values = Some(v.into()),
            "route" => a.route = Some(parse_route(v)?),
            _ => return Err(format!("unknown flag --{flag}")),
        }
        i += 2;
    }
    a.host = host.filter(|h| !h.is_empty()).ok_or("--host is required")?;
    match op {
        "list-services" if a.transport == "udp" => return Err("--transport udp: rseip sends ListServices over TCP only".into()),
        "list-identity" if a.transport == "udp" && a.port != 44818 => {
            return Err("--transport udp: rseip's discovery sends to port 44818 only".into())
        }
        "multiple-get" if a.paths.is_empty() => return Err("--path is required".into()),
        "read-tag" | "write-tag" if a.tag.is_none() => return Err("--tag is required".into()),
        "read-tags" if a.tags.is_none() => return Err("--tags is required".into()),
        "write-tag" if a.typ.is_none() || a.values.is_none() => return Err("--type and --values are required".into()),
        _ => {}
    }
    if let Some(t) = &a.typ {
        if op == "read-tag" && type_code(t).is_none() {
            return Err(format!("--type: unknown type {t}"));
        }
        if op == "write-tag" && type_code(t).is_none() && t != "STRING" && t != "JSON" {
            return Err(format!("--type: unknown type {t}"));
        }
    }
    if let Some(t) = &a.tag {
        EPath::parse_tag(t).map_err(|e| format!("--tag: {e}"))?;
    }
    if let Some(ts) = &a.tags {
        for t in ts.split(',') {
            EPath::parse_tag(t).map_err(|e| format!("--tags: {t}: {e}"))?;
        }
    }
    Ok(a)
}

async fn resolve(a: &Args) -> Result<SocketAddrV4, OpErr> {
    if let Ok(ip) = a.host.parse::<Ipv4Addr>() {
        return Ok(SocketAddrV4::new(ip, a.port));
    }
    let addrs = tm(a.timeout, tokio::net::lookup_host((a.host.as_str(), a.port)))
        .await
        .map_err(|e| OpErr::new("connect-failed", format!("{}: {}", a.host, e.msg)))?;
    addrs
        .filter_map(|x| match x {
            std::net::SocketAddr::V4(v) => Some(v),
            _ => None,
        })
        .next()
        .ok_or_else(|| OpErr::new("connect-failed", format!("{}: no IPv4 address", a.host)))
}

// ---------------------------------------------------------------------------
// Helpers

fn hex(b: &[u8]) -> String {
    b.iter().map(|x| format!("{x:02x}")).collect()
}

fn identity_doc(id: &IdentityObject) -> Value {
    let ip = Ipv4Addr::from(id.socket_addr.sin_addr);
    json!({
        "vendorId": id.vendor_id,
        "deviceType": id.device_type,
        "productCode": id.product_code,
        "revision": format!("{}.{}", id.revision.major, id.revision.minor),
        "status": id.status,
        "serialNumber": id.serial_number,
        "productName": id.product_name,
        "state": id.state,
        "socketAddress": format!("{}:{}", ip, id.socket_addr.sin_port),
    })
}

/// Elementary Logix/CIP data types: name, code, size.
const TYPES: &[(&str, u16, usize)] = &[
    ("BOOL", 0xC1, 1),
    ("SINT", 0xC2, 1),
    ("INT", 0xC3, 2),
    ("DINT", 0xC4, 4),
    ("LINT", 0xC5, 8),
    ("USINT", 0xC6, 1),
    ("UINT", 0xC7, 2),
    ("UDINT", 0xC8, 4),
    ("ULINT", 0xC9, 8),
    ("REAL", 0xCA, 4),
    ("LREAL", 0xCB, 8),
    ("BYTE", 0xD1, 1),
    ("WORD", 0xD2, 2),
    ("DWORD", 0xD3, 4),
    ("LWORD", 0xD4, 8),
];

fn type_code(name: &str) -> Option<u16> {
    TYPES.iter().find(|t| t.0 == name).map(|t| t.1)
}
fn type_name(code: u16) -> Option<&'static str> {
    TYPES.iter().find(|t| t.1 == code).map(|t| t.0)
}
fn type_size(code: u16) -> Option<usize> {
    TYPES.iter().find(|t| t.1 == code).map(|t| t.2)
}

/// rseip's TagType for the fragmented write (it knows fewer types).
fn rseip_tag_type(code: u16, handle: Option<u16>) -> Option<TagType> {
    Some(match code {
        0xC1 => TagType::Bool,
        0xC2 => TagType::Sint,
        0xC3 => TagType::Int,
        0xC4 => TagType::Dint,
        0xC5 => TagType::Lint,
        0xCA => TagType::Real,
        0xD3 => TagType::Dword,
        0x02A0 => TagType::Structure(handle?),
        _ => return None,
    })
}

fn decode_atomic(code: u16, b: &[u8]) -> Value {
    let n = match type_size(code) {
        Some(n) if b.len() >= n => n,
        _ => return Value::Null,
    };
    let mut x = [0u8; 8];
    x[..n].copy_from_slice(&b[..n]);
    match code {
        0xC1 => Value::Bool(b[0] != 0),
        0xC2 => json!(b[0] as i8),
        0xC3 => json!(i16::from_le_bytes([x[0], x[1]])),
        0xC4 => json!(i32::from_le_bytes([x[0], x[1], x[2], x[3]])),
        0xC5 => json!(i64::from_le_bytes(x)),
        0xCA => json!(f32::from_le_bytes([x[0], x[1], x[2], x[3]]) as f64),
        0xCB => json!(f64::from_le_bytes(x)),
        _ => json!(u64::from_le_bytes(x)),
    }
}

fn encode_atomic(code: u16, v: &Value, out: &mut [u8]) -> Result<(), String> {
    let n = type_size(code).ok_or("unknown type")?;
    if out.len() < n {
        return Err("value out of the tag's data".into());
    }
    let bad = || format!("{v} is not a {}", type_name(code).unwrap_or("?"));
    let b: Vec<u8> = match code {
        0xCA => (v.as_f64().ok_or_else(bad)? as f32).to_le_bytes().to_vec(),
        0xCB => v.as_f64().ok_or_else(bad)?.to_le_bytes().to_vec(),
        0xC1 => vec![match v {
            Value::Bool(x) => *x as u8,
            _ => (v.as_i64().ok_or_else(bad)? != 0) as u8,
        }],
        _ => {
            let i: i128 = match v {
                Value::Bool(x) => *x as i128,
                _ => v.as_i64().map(|x| x as i128).or_else(|| v.as_u64().map(|x| x as i128)).ok_or_else(bad)?,
            };
            let signed = matches!(code, 0xC2 | 0xC3 | 0xC4 | 0xC5);
            let bits = 8 * n as u32;
            let (lo, hi) = if signed {
                (-(1i128 << (bits - 1)), (1i128 << (bits - 1)) - 1)
            } else {
                (0, (1i128 << bits) - 1)
            };
            if i < lo || i > hi {
                return Err(format!("{i} out of range for {}", type_name(code).unwrap_or("?")));
            }
            i.to_le_bytes()[..n].to_vec()
        }
    };
    out[..n].copy_from_slice(&b);
    Ok(())
}

/// A text value of --values as JSON, for the elementary type.
fn text_value(code: u16, s: &str) -> Result<Value, String> {
    let s = s.trim();
    match code {
        0xC1 => match s.to_ascii_lowercase().as_str() {
            "true" | "1" => Ok(Value::Bool(true)),
            "false" | "0" => Ok(Value::Bool(false)),
            _ => Err(format!("--values: {s} is not a BOOL")),
        },
        0xCA | 0xCB => s.parse::<f64>().map(|f| json!(f)).map_err(|_| format!("--values: {s} is not a number")),
        _ => {
            let v = if let Some(h) = s.strip_prefix("0x") {
                i128::from_str_radix(h, 16)
            } else {
                s.parse::<i128>()
            };
            match v {
                Ok(v) if v >= i64::MIN as i128 && v <= i64::MAX as i128 => Ok(json!(v as i64)),
                Ok(v) if v >= 0 && v <= u64::MAX as i128 => Ok(json!(v as u64)),
                _ => Err(format!("--values: {s} is not an integer")),
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Logix: tags, templates and values

#[derive(Clone, Debug)]
struct Member {
    name: String,
    /// array size; the bit number for a BOOL
    info: u16,
    typ: u16,
    offset: u32,
}

#[derive(Clone, Debug)]
struct Tpl {
    name: String,
    size: u32,
    members: Vec<Member>,
}

impl Tpl {
    fn member(&self, name: &str) -> Option<&Member> {
        self.members.iter().find(|m| m.name.eq_ignore_ascii_case(name))
    }
    /// A Logix STRING (or any string type): LEN and DATA members.
    fn string_layout(&self) -> Option<(u32, u32, usize)> {
        let len = self.member("LEN")?;
        let data = self.member("DATA")?;
        Some((len.offset, data.offset, data.info as usize))
    }
}

/// Logix names its STRING type's template ASCIISTRING82; it is shown (and
/// typed in RSLogix) as STRING.
fn display_name(template: &str) -> String {
    if template == "ASCIISTRING82" { "STRING".into() } else { template.into() }
}

fn is_struct(typ: u16) -> bool {
    typ & 0x8000 != 0
}
fn template_id(typ: u16) -> u16 {
    typ & 0x0FFF
}
fn hidden(name: &str) -> bool {
    name.starts_with("ZZZZZZZZZZ") || name.starts_with("__")
}

/// The tag type a read or write needs: elementary code, or structure with
/// handle, as the Read Tag reply carries it.
#[derive(Clone, Copy, Debug)]
struct WireType {
    code: u16,
    handle: Option<u16>,
}

impl WireType {
    fn split(b: &Bytes) -> Result<(WireType, Bytes), OpErr> {
        if b.len() < 2 {
            return Err(OpErr::new("protocol", "Read Tag reply without a type"));
        }
        let code = u16::from_le_bytes([b[0], b[1]]);
        if code == 0x02A0 {
            if b.len() < 4 {
                return Err(OpErr::new("protocol", "Read Tag reply without a structure handle"));
            }
            Ok((WireType { code, handle: Some(u16::from_le_bytes([b[2], b[3]])) }, b.slice(4..)))
        } else {
            Ok((WireType { code, handle: None }, b.slice(2..)))
        }
    }
    fn put(&self, out: &mut BytesMut) {
        out.put_u16_le(self.code);
        if let Some(h) = self.handle {
            out.put_u16_le(h);
        }
    }
}

struct Logix {
    ab: Ab,
    t: Duration,
    symbols: Option<Vec<(String, u16, u8)>>,
    tpls: HashMap<u16, Tpl>,
}

/// The outcome of one Logix request: done, or answered with a CIP error.
enum Answer<T> {
    Done(T),
    Status(u8, Option<u16>, String),
}

impl Logix {
    /// Read Tag of `count` elements; arrays with Read Tag Fragmented.
    async fn read(&mut self, path: &EPath, count: u16) -> Result<Answer<(WireType, Bytes)>, OpErr> {
        if count > 1 {
            let mut buf = BytesMut::new();
            let mut typ;
            loop {
                let req = ReadFragmentedRequest::new().tag(path.clone()).count(count).offset(buf.len() as u16);
                let t = self.t;
                let r = tm(t, ab!(&mut self.ab, c => c.read_tag_fragmented(req))).await;
                match r {
                    Ok((more, tv)) => {
                        let tv: TagValue<Bytes> = tv;
                        typ = WireType { code: tv.tag_type.type_code(), handle: tv.tag_type.structure_handle() };
                        if tv.value.is_empty() && more {
                            return Err(OpErr::new("protocol", "Read Tag Fragmented: empty partial reply"));
                        }
                        buf.put_slice(&tv.value);
                        if !more {
                            break;
                        }
                    }
                    // rseip's TagType does not know every elementary type
                    // (LREAL, the unsigned types): read the array with one
                    // Read Tag instead.
                    Err(e) if e.msg.contains("unexpected type code") && buf.is_empty() => {
                        eprintln!("eiprs: rseip cannot decode the type of a fragmented reply ({}); Read Tag instead", e.msg);
                        return self.read_one(path, count).await;
                    }
                    Err(e) => {
                        return Ok(match cip_status(&e) {
                            Some((g, x)) => Answer::Status(g, x, e.msg),
                            None => return Err(e),
                        })
                    }
                }
            }
            return Ok(Answer::Done((typ, buf.freeze())));
        }
        self.read_one(path, count).await
    }

    async fn read_one(&mut self, path: &EPath, count: u16) -> Result<Answer<(WireType, Bytes)>, OpErr> {
        let req = (path.clone(), count);
        let t = self.t;
        let r: Result<BytesHolder, OpErr> = tm(t, ab!(&mut self.ab, c => c.read_tag(req))).await;
        match r {
            Ok(b) => Ok(Answer::Done(WireType::split(&Bytes::from(b))?)),
            Err(e) => match cip_status(&e) {
                Some((g, x)) => Ok(Answer::Status(g, x, e.msg)),
                None => Err(e),
            },
        }
    }

    /// Write Tag of `count` elements of `data`; Write Tag Fragmented when it
    /// does not fit one request.
    async fn write(&mut self, path: &EPath, wt: WireType, count: u16, data: Bytes) -> Result<Answer<()>, OpErr> {
        if data.len() <= WRITE_CHUNK || count <= 1 {
            let mut b = BytesMut::new();
            wt.put(&mut b);
            b.put_u16_le(count);
            b.put_slice(&data);
            let b = b.freeze();
            let t = self.t;
            let r = tm(t, ab!(&mut self.ab, c => c.write_tag(path.clone(), b.clone()))).await;
            return Self::answer(r);
        }
        let tt = rseip_tag_type(wt.code, wt.handle)
            .ok_or_else(|| OpErr::new("failed", "rseip's Write Tag Fragmented has no tag type for this type"))?;
        let elem = data.len() / count as usize;
        let chunk = (WRITE_CHUNK / elem.max(1)).max(1) * elem.max(1);
        let mut off = 0;
        while off < data.len() {
            let end = (off + chunk).min(data.len());
            let req = WriteFragmentedRequest::new(data.slice(off..end)).tag(path.clone()).tag_type(tt).count(count).offset(off as u16);
            let t = self.t;
            match Self::answer(tm(t, ab!(&mut self.ab, c => c.write_tag_fragmented(req))).await)? {
                Answer::Done(_) => {}
                Answer::Status(g, x, m) => return Ok(Answer::Status(g, x, m)),
            }
            off = end;
        }
        Ok(Answer::Done(()))
    }

    fn answer<T>(r: Result<T, OpErr>) -> Result<Answer<()>, OpErr> {
        match r {
            Ok(_) => Ok(Answer::Done(())),
            Err(e) => match cip_status(&e) {
                Some((g, x)) => Ok(Answer::Status(g, x, e.msg)),
                None => Err(e),
            },
        }
    }

    /// Read-Modify-Write Tag of one bit of an integer of `size` octets.
    async fn rmw(&mut self, path: &EPath, size: usize, bit: u32, value: bool) -> Result<Answer<()>, OpErr> {
        let byte = (bit / 8) as usize;
        let mask = 1u8 << (bit % 8);
        macro_rules! go {
            ($n:literal) => {{
                let mut req = ReadModifyWriteRequest::<$n>::new().tag(path.clone());
                if value {
                    req.or_mask_mut()[byte] = mask;
                } else {
                    req.and_mask_mut()[byte] = !mask;
                }
                let t = self.t;
                Self::answer(tm(t, ab!(&mut self.ab, c => c.read_modify_write(req))).await)
            }};
        }
        if byte >= size {
            return Err(OpErr::new("failed", format!("bit {bit} beyond a {size}-octet integer")));
        }
        match size {
            1 => go!(1),
            2 => go!(2),
            4 => go!(4),
            8 => go!(8),
            _ => Err(OpErr::new("failed", format!("no Read-Modify-Write for {size}-octet tags"))),
        }
    }

    /// The controller's tags from rseip's list_tag (Symbol object 0x6B,
    /// Get_Instance_Attribute_List 0x55): name, type, number of dimensions.
    async fn symbols(&mut self) -> Result<&Vec<(String, u16, u8)>, OpErr> {
        if self.symbols.is_none() {
            let t = self.t;
            let list = ab!(&mut self.ab, c => collect_symbols(t, c.list_tag().call()).await)?;
            self.symbols = Some(list);
        }
        Ok(self.symbols.as_ref().unwrap())
    }

    /// Uploads a template and the templates of its structure members:
    /// rseip's find_template (Get_Attribute_List on 0x6C) and read_template
    /// (Read Template 0x4C).
    async fn ensure_template(&mut self, id: u16) -> Result<(), OpErr> {
        let mut todo = vec![id];
        while let Some(id) = todo.pop() {
            if self.tpls.contains_key(&id) {
                continue;
            }
            let t = self.t;
            let tpl = tm(t, self.ab.find_template(id)).await?;
            let def = {
                let mut rd = self.ab.read_template(&tpl);
                let d = tm(t, rd.call()).await?;
                let mut members: Vec<Member> = d
                    .iter()
                    .map(|(_, m)| Member {
                        name: m.name.to_string(),
                        info: m.array_size,
                        typ: u16::from(m.type_info),
                        offset: m.offset,
                    })
                    .collect();
                members.sort_by_key(|m| (m.offset, m.info, m.name.clone()));
                Tpl { name: d.name().to_string(), size: tpl.struct_size, members }
            };
            for m in &def.members {
                if is_struct(m.typ) {
                    todo.push(template_id(m.typ));
                }
            }
            self.tpls.insert(id, def);
        }
        Ok(())
    }

    /// The symbol type of a tag path (`Pump`, `Fans[1].Label`), from the tag
    /// list and the templates.
    async fn path_type(&mut self, tag: &str) -> Result<u16, OpErr> {
        let path = EPath::parse_tag(tag).map_err(|e| OpErr::new("failed", e.to_string()))?;
        let mut segs = path.iter();
        let base = match segs.next() {
            Some(Segment::Symbol(s)) => s.to_string(),
            _ => return Err(OpErr::new("failed", "tag path without a symbol")),
        };
        let sym = self.symbols().await?.iter().find(|s| s.0.eq_ignore_ascii_case(&base)).cloned();
        let mut typ = sym.ok_or_else(|| OpErr::new("failed", format!("{base} is not in the controller's tag list")))?.1;
        for seg in segs {
            if let Segment::Symbol(m) = seg {
                if !is_struct(typ) {
                    return Err(OpErr::new("failed", format!("{tag}: member of a tag that is not a structure")));
                }
                let id = template_id(typ);
                self.ensure_template(id).await?;
                let tpl = &self.tpls[&id];
                typ = tpl.member(m).ok_or_else(|| OpErr::new("failed", format!("{tag}: no member {m}")))?.typ;
            }
        }
        if is_struct(typ) {
            self.ensure_template(template_id(typ)).await?;
        }
        Ok(typ)
    }

    fn decode_struct(&self, id: u16, b: &[u8]) -> Value {
        let tpl = match self.tpls.get(&id) {
            Some(t) => t,
            None => return Value::Null,
        };
        if let Some((len_off, data_off, cap)) = tpl.string_layout() {
            let (lo, d) = (len_off as usize, data_off as usize);
            if b.len() < lo + 4 || b.len() < d {
                return Value::Null;
            }
            let n = (i32::from_le_bytes([b[lo], b[lo + 1], b[lo + 2], b[lo + 3]]).max(0) as usize).min(cap).min(b.len() - d);
            return Value::String(String::from_utf8_lossy(&b[d..d + n]).into_owned());
        }
        let mut obj = Map::new();
        for m in tpl.members.iter().filter(|m| !hidden(&m.name)) {
            obj.insert(m.name.clone(), self.decode_member(m, b));
        }
        Value::Object(obj)
    }

    fn decode_member(&self, m: &Member, b: &[u8]) -> Value {
        let off = m.offset as usize;
        if off > b.len() {
            return Value::Null;
        }
        if is_struct(m.typ) {
            let id = template_id(m.typ);
            let size = self.tpls.get(&id).map(|t| t.size as usize).unwrap_or(0);
            if m.info == 0 {
                return self.decode_struct(id, &b[off..]);
            }
            return Value::Array(
                (0..m.info as usize)
                    .map(|i| b.get(off + i * size..).map(|s| self.decode_struct(id, s)).unwrap_or(Value::Null))
                    .collect(),
            );
        }
        let code = m.typ & 0xFF;
        if code == 0xC1 {
            return Value::Bool(b[off] >> (m.info & 7) & 1 == 1);
        }
        let size = type_size(code).unwrap_or(1);
        if m.info == 0 {
            return decode_atomic(code, &b[off..]);
        }
        Value::Array((0..m.info as usize).map(|i| b.get(off + i * size..).map(|s| decode_atomic(code, s)).unwrap_or(Value::Null)).collect())
    }

    fn encode_struct(&self, id: u16, v: &Value, out: &mut [u8]) -> Result<(), String> {
        let tpl = self.tpls.get(&id).ok_or("no template")?;
        if let Some((len_off, data_off, cap)) = tpl.string_layout() {
            let s = v.as_str().ok_or_else(|| format!("{v} is not a string"))?;
            if s.len() > cap {
                return Err(format!("string of {} octets, {} fit", s.len(), cap));
            }
            let (lo, d) = (len_off as usize, data_off as usize);
            if out.len() < lo + 4 || out.len() < d + cap {
                return Err("string out of the tag's data".into());
            }
            out[lo..lo + 4].copy_from_slice(&(s.len() as i32).to_le_bytes());
            out[d..d + cap].fill(0);
            out[d..d + s.len()].copy_from_slice(s.as_bytes());
            return Ok(());
        }
        let obj = v.as_object().ok_or_else(|| format!("{v} is not an object of {}'s members", tpl.name))?;
        for (k, x) in obj {
            let m = tpl.member(k).ok_or_else(|| format!("{} has no member {k}", tpl.name))?;
            let off = m.offset as usize;
            if off >= out.len() {
                return Err(format!("member {k} out of the tag's data"));
            }
            if is_struct(m.typ) {
                let mid = template_id(m.typ);
                let size = self.tpls.get(&mid).map(|t| t.size as usize).unwrap_or(0);
                if m.info == 0 {
                    self.encode_struct(mid, x, &mut out[off..])?;
                } else {
                    let a = x.as_array().ok_or_else(|| format!("{k}: not an array"))?;
                    for (i, e) in a.iter().enumerate().take(m.info as usize) {
                        self.encode_struct(mid, e, out.get_mut(off + i * size..).ok_or("out of range")?)?;
                    }
                }
                continue;
            }
            let code = m.typ & 0xFF;
            if code == 0xC1 {
                let bit = 1u8 << (m.info & 7);
                let on = match x {
                    Value::Bool(b) => *b,
                    _ => x.as_i64().ok_or_else(|| format!("{k}: not a BOOL"))? != 0,
                };
                if on {
                    out[off] |= bit
                } else {
                    out[off] &= !bit
                }
                continue;
            }
            let size = type_size(code).ok_or_else(|| format!("{k}: unknown type {code:#x}"))?;
            if m.info == 0 {
                encode_atomic(code, x, &mut out[off..])?;
            } else {
                let a = x.as_array().ok_or_else(|| format!("{k}: not an array"))?;
                for (i, e) in a.iter().enumerate().take(m.info as usize) {
                    encode_atomic(code, e, out.get_mut(off + i * size..).ok_or("out of range")?)?;
                }
            }
        }
        Ok(())
    }

    /// The type name of a tag type: elementary, or the template's name.
    async fn type_name_of(&mut self, typ: u16) -> Result<String, OpErr> {
        if is_struct(typ) {
            let id = template_id(typ);
            self.ensure_template(id).await?;
            return Ok(display_name(&self.tpls[&id].name));
        }
        Ok(type_name(typ & 0xFF).map(str::to_string).unwrap_or_else(|| format!("{:#06x}", typ & 0xFF)))
    }

    /// Values of a Read Tag reply. Structures and STRING need the tag list
    /// and templates (the reply only carries the structure handle).
    async fn values(&mut self, tag: &str, wt: WireType, data: &Bytes) -> Result<(Vec<Value>, String), OpErr> {
        if wt.handle.is_none() {
            let size = type_size(wt.code).ok_or_else(|| OpErr::new("failed", format!("unknown type {:#06x} in the reply", wt.code)))?;
            let vals = data.chunks(size).filter(|c| c.len() == size).map(|c| decode_atomic(wt.code, c)).collect();
            return Ok((vals, type_name(wt.code).unwrap_or("?").to_string()));
        }
        let typ = self.path_type(tag).await?;
        if !is_struct(typ) {
            return Err(OpErr::new("failed", format!("{tag}: the reply is a structure, the tag list says {typ:#06x}")));
        }
        let id = template_id(typ);
        let tpl = self.tpls[&id].clone();
        let size = (tpl.size as usize).max(1);
        let vals = data.chunks(size).map(|c| self.decode_struct(id, c)).collect();
        Ok((vals, display_name(&tpl.name)))
    }
}

async fn collect_symbols<'a>(
    t: Duration,
    s: impl Stream<Item = Result<SymbolInstance<'a>, ClientError>>,
) -> Result<Vec<(String, u16, u8)>, OpErr> {
    let mut s = Box::pin(s);
    let mut out = Vec::new();
    loop {
        match tokio::time::timeout(t, s.next()).await {
            Err(_) => return Err(OpErr::new("timeout", "tag list: no reply")),
            Ok(None) => return Ok(out),
            Ok(Some(Err(e))) => return Err(e.into()),
            Ok(Some(Ok(sym))) => out.push((sym.name.to_string(), u16::from(sym.symbol_type), sym.symbol_type.dims())),
        }
    }
}

/// `Tag.N`: bit N of an integer tag.
fn split_bit(tag: &str) -> Option<(&str, u32)> {
    let (base, n) = tag.rsplit_once('.')?;
    if n.is_empty() || !n.bytes().all(|c| c.is_ascii_digit()) {
        return None;
    }
    Some((base, n.parse().ok()?))
}

/// `Tag[N]`: element N of a one-dimensional array tag.
fn split_index(tag: &str) -> Option<(&str, u32)> {
    let s = tag.strip_suffix(']')?;
    let (base, n) = s.rsplit_once('[')?;
    if n.is_empty() || !n.bytes().all(|c| c.is_ascii_digit()) {
        return None;
    }
    Some((base, n.parse().ok()?))
}

fn tag_path(tag: &str) -> Result<EPath, OpErr> {
    EPath::parse_tag(tag).map_err(|e| OpErr::new("failed", format!("{tag}: {e}")))
}

// ---------------------------------------------------------------------------
// Operations

async fn run(op: &str, a: &Args, doc: &mut Map<String, Value>) -> Result<(), OpErr> {
    let t = a.timeout;
    match op {
        "list-identity" if a.transport == "udp" => {
            let ip = *resolve(a).await?.ip();
            // rseip's EipDiscovery: one ListIdentity datagram to port 44818 of
            // the address (its "broadcast" address; here unicast), replies
            // until the interval ends.
            let stream = EipDiscovery::new(Ipv4Addr::UNSPECIFIED)
                .broadcast(ip)
                .repeat(1)
                .interval(t)
                .run::<IdentityObject>()
                .await?;
            let mut stream = Box::pin(stream);
            match tokio::time::timeout(t + Duration::from_millis(200), stream.next()).await {
                Ok(Some((id, _from))) => {
                    doc.insert("identity".into(), identity_doc(&id));
                }
                _ => return Err(OpErr::new("timeout", "no ListIdentity reply")),
            }
        }
        "list-identity" | "list-services" => {
            let addr = resolve(a).await?;
            let mut d = Direct::connect(addr, t).await?;
            if op == "list-identity" {
                let mut cpf = tm(t, d.ctx.list_identity()).await?;
                let item = cpf.next_typed::<IdentityObject>().ok_or_else(|| OpErr::new("protocol", "empty ListIdentity reply"))??;
                doc.insert("identity".into(), identity_doc(&item.data));
            } else {
                let mut cpf = tm(t, d.ctx.list_service()).await?;
                let mut services = Vec::new();
                while let Some(item) = cpf.next_typed::<ListServiceItem>() {
                    let s = item?.data;
                    services.push(json!({
                        "name": s.name.trim_end_matches('\0'),
                        "version": s.protocol_version,
                        "capabilities": s.capability,
                    }));
                }
                doc.insert("services".into(), Value::Array(services));
            }
        }
        "get-attribute" | "set-attribute" | "get-attributes-all" | "multiple-get" => {
            let addr = resolve(a).await?;
            let mut g = if a.connected {
                Gen::Conn(EipConnection::new(addr, open_options(&a.route)))
            } else if let Some(r) = &a.route {
                Gen::Routed(EipClient::new(addr).with_connection_path(r.clone()))
            } else {
                let mut d = Direct::connect(addr, t).await?;
                tm(t, d.ctx.register_session()).await?;
                Gen::Direct(d)
            };
            let res = generic(op, a, &mut g, doc).await;
            let _ = tokio::time::timeout(t, g.close()).await;
            res?;
        }
        _ => {
            let addr = resolve(a).await?;
            let ab = if a.connected {
                Ab::Conn(AbEipConnection::new(addr, open_options(&a.route)))
            } else {
                let mut c = AbEipClient::new(addr);
                if let Some(r) = &a.route {
                    c = c.with_connection_path(r.clone());
                }
                Ab::Client(c)
            };
            let mut lgx = Logix { ab, t, symbols: None, tpls: HashMap::new() };
            let res = logix(op, a, &mut lgx, doc).await;
            let _ = tokio::time::timeout(t, lgx.ab.close()).await;
            res?;
        }
    }
    Ok(())
}

async fn generic(op: &str, a: &Args, g: &mut Gen, doc: &mut Map<String, Value>) -> Result<(), OpErr> {
    let t = a.timeout;
    if op == "multiple-get" {
        // rseip's multiple_service() builds and sends the Multiple Service
        // Packet (0x0A to @2/1); its reply iterator stops at the first
        // embedded error and cannot size variable-length replies, so the
        // adapter keeps the raw reply and splits it at the offsets.
        let mut tap = Tap { inner: g, raw: None };
        let mut mp = tap.multiple_service();
        for (c, i, at) in &a.paths {
            mp = mp.push(MessageRequest::new(0x0E, EPath::default().with_class(*c).with_instance(*i).with_attribute(*at), ()));
        }
        take_last_reply();
        let res = tm(t, mp.call()).await;
        let raw = match (tap.raw.take(), res) {
            (Some(raw), _) => raw,
            (None, Err(e)) => take_last_reply().ok_or(e)?,
            (None, Ok(_)) => return Err(OpErr::new("protocol", "no reply")),
        };
        doc.insert("status".into(), json!(raw.status.general));
        doc.insert("extendedStatus".into(), json!(raw.ext));
        let b = &raw.data[..];
        if raw.status.general != 0 && raw.status.general != 0x1E || b.len() < 2 {
            doc.insert("data".into(), json!(hex(b)));
            return Ok(());
        }
        let n = u16::from_le_bytes([b[0], b[1]]) as usize;
        if b.len() < 2 + 2 * n {
            return Err(OpErr::new("protocol", "Multiple Service reply: short offset table"));
        }
        let offs: Vec<usize> = (0..n).map(|i| u16::from_le_bytes([b[2 + 2 * i], b[3 + 2 * i]]) as usize).collect();
        let mut replies = Vec::new();
        for i in 0..n {
            let start = offs[i];
            let end = if i + 1 < n { offs[i + 1] } else { b.len() };
            if start + 4 > end || end > b.len() {
                return Err(OpErr::new("protocol", "Multiple Service reply: bad offsets"));
            }
            let r = &b[start..end];
            let ext = r[3] as usize;
            let data = r.get(4 + 2 * ext..).unwrap_or(&[]);
            replies.push(json!({"status": r[2], "data": hex(data)}));
        }
        doc.insert("replies".into(), Value::Array(replies));
        return Ok(());
    }
    let mut path = EPath::default().with_class(a.class).with_instance(a.instance);
    let service = match op {
        "get-attribute" => {
            path = path.with_attribute(a.attribute);
            0x0E
        }
        "set-attribute" => {
            path = path.with_attribute(a.attribute);
            0x10
        }
        _ => 0x01,
    };
    let data = Bytes::from(if op == "set-attribute" { a.data.clone() } else { Vec::new() });
    take_last_reply();
    let r: RawReply = match tm(t, g.send(MessageRequest::new(service, path, data))).await {
        Ok(r) => r,
        Err(e) => take_last_reply().ok_or(e)?,
    };
    doc.insert("status".into(), json!(r.status.general));
    doc.insert("extendedStatus".into(), json!(r.ext));
    doc.insert("data".into(), json!(hex(&r.data)));
    Ok(())
}

fn status_fields(doc: &mut Map<String, Value>, g: u8, x: Option<u16>, msg: &str) {
    doc.insert("status".into(), json!(g));
    doc.insert("extendedStatus".into(), json!(x.into_iter().collect::<Vec<_>>()));
    doc.insert("errorText".into(), json!(msg));
}

async fn logix(op: &str, a: &Args, lgx: &mut Logix, doc: &mut Map<String, Value>) -> Result<(), OpErr> {
    match op {
        "tag-list" => {
            let syms = lgx.symbols().await?.clone();
            let mut tags = Vec::new();
            for (name, typ, dims) in syms {
                if name.starts_with("__") || name.contains(':') {
                    continue;
                }
                let tn = lgx.type_name_of(typ).await?;
                tags.push(json!({"name": name, "type": tn, "dims": [], "dimCount": dims}));
            }
            doc.insert("tags".into(), Value::Array(tags));
            doc.insert("programName".into(), Value::Null);
        }
        "read-tag" => {
            let tag = a.tag.as_deref().unwrap();
            read_tag(lgx, tag, a.count, doc).await?;
        }
        "read-tags" => {
            let mut results = Vec::new();
            for tag in a.tags.as_deref().unwrap().split(',') {
                let mut d = Map::new();
                read_tag(lgx, tag, 1, &mut d).await?;
                let mut r = Map::new();
                r.insert("tag".into(), json!(tag));
                r.insert("values".into(), d.remove("values").unwrap_or(json!([])));
                r.insert("status".into(), d.remove("status").unwrap_or(json!(0)));
                if let Some(e) = d.remove("errorText") {
                    r.insert("errorText".into(), e);
                }
                results.push(Value::Object(r));
            }
            doc.insert("results".into(), Value::Array(results));
        }
        "write-tag" => {
            let tag = a.tag.as_deref().unwrap();
            let ans = write_tag(lgx, tag, a.typ.as_deref().unwrap(), a.values.as_deref().unwrap()).await?;
            match ans {
                Answer::Done(()) => {
                    doc.insert("status".into(), json!(0));
                }
                Answer::Status(g, x, m) => status_fields(doc, g, x, &m),
            }
        }
        _ => unreachable!(),
    }
    Ok(())
}

async fn read_tag(lgx: &mut Logix, tag: &str, count: u16, doc: &mut Map<String, Value>) -> Result<(), OpErr> {
    // Tag[N] of a BOOL array: Logix holds and serves BOOL arrays as DWORDs
    // (element N of the request is DWORD N): read the DWORD holding bit N.
    if count == 1 {
        if let Some((base, n)) = split_index(tag) {
            if let Answer::Done((wt, _)) = lgx.read(&tag_path(base)?, 1).await? {
                if wt.code == 0xD3 {
                    match lgx.read(&tag_path(&format!("{base}[{}]", n / 32))?, 1).await? {
                        Answer::Status(g, x, m) => status_fields(doc, g, x, &m),
                        Answer::Done((_, data)) if data.len() >= 4 => {
                            let w = u32::from_le_bytes([data[0], data[1], data[2], data[3]]);
                            doc.insert("status".into(), json!(0));
                            doc.insert("type".into(), json!("BOOL"));
                            doc.insert("values".into(), json!([w >> (n % 32) & 1 == 1]));
                        }
                        Answer::Done(_) => return Err(OpErr::new("protocol", "short DWORD in the reply")),
                    }
                    return Ok(());
                }
            }
        }
    }
    let path = tag_path(tag)?;
    match lgx.read(&path, count).await? {
        Answer::Status(g, x, m) => status_fields(doc, g, x, &m),
        Answer::Done((wt, data)) => {
            let (vals, typ) = lgx.values(tag, wt, &data).await?;
            doc.insert("status".into(), json!(0));
            doc.insert("type".into(), json!(typ));
            doc.insert("values".into(), Value::Array(vals));
        }
    }
    Ok(())
}

async fn write_tag(lgx: &mut Logix, tag: &str, typ: &str, values: &str) -> Result<Answer<()>, OpErr> {
    let usage = |m: String| OpErr::new("usage", m);
    match typ {
        "STRING" | "JSON" => {
            let v: Value = if typ == "STRING" {
                Value::String(values.to_string())
            } else {
                serde_json::from_str(values).map_err(|e| usage(format!("--values: not JSON: {e}")))?
            };
            let path = tag_path(tag)?;
            // The current value gives the structure handle and the octets of
            // the members the value leaves out.
            let (wt, cur) = match lgx.read(&path, 1).await? {
                Answer::Done(x) => x,
                Answer::Status(g, x, m) => return Ok(Answer::Status(g, x, m)),
            };
            if wt.handle.is_none() {
                return Err(OpErr::new("failed", format!("{tag} is not a structure")));
            }
            let st = lgx.path_type(tag).await?;
            let mut buf = cur.to_vec();
            lgx.encode_struct(template_id(st), &v, &mut buf).map_err(|m| OpErr::new("failed", format!("--values: {m}")))?;
            lgx.write(&path, wt, 1, Bytes::from(buf)).await
        }
        "BOOL" => {
            let vals: Vec<Value> = values.split(',').map(|s| text_value(0xC1, s)).collect::<Result<_, _>>().map_err(usage)?;
            let on = vals[0].as_bool().unwrap();
            if vals.len() == 1 {
                // Tag.N: a bit of an integer, with Read-Modify-Write.
                if let Some((base, bit)) = split_bit(tag) {
                    let bpath = tag_path(base)?;
                    let (wt, _) = match lgx.read(&bpath, 1).await? {
                        Answer::Done(x) => x,
                        Answer::Status(g, x, m) => return Ok(Answer::Status(g, x, m)),
                    };
                    if wt.code != 0xC1 {
                        let size = type_size(wt.code).filter(|_| wt.handle.is_none())
                            .ok_or_else(|| OpErr::new("failed", format!("{base} is not an integer")))?;
                        return lgx.rmw(&bpath, size, bit, on).await;
                    }
                }
                // Tag[N] of a BOOL array, which Logix holds (and serves) as
                // DWORDs: Read-Modify-Write of the DWORD holding the bit.
                if let Some((base, n)) = split_index(tag) {
                    let bpath = tag_path(base)?;
                    if let Answer::Done((wt, _)) = lgx.read(&bpath, 1).await? {
                        if wt.code == 0xD3 {
                            let dpath = tag_path(&format!("{base}[{}]", n / 32))?;
                            return lgx.rmw(&dpath, 4, n % 32, on).await;
                        }
                    }
                }
            }
            let data: Vec<u8> = vals.iter().map(|v| v.as_bool().unwrap() as u8).collect();
            let path = tag_path(tag)?;
            lgx.write(&path, WireType { code: 0xC1, handle: None }, data.len() as u16, Bytes::from(data)).await
        }
        _ => {
            let code = type_code(typ).unwrap();
            let size = type_size(code).unwrap();
            let mut data = Vec::new();
            for s in values.split(',') {
                let v = text_value(code, s).map_err(usage)?;
                let mut b = vec![0u8; size];
                encode_atomic(code, &v, &mut b).map_err(|m| usage(format!("--values: {m}")))?;
                data.extend(b);
            }
            let n = (data.len() / size) as u16;
            let path = tag_path(tag)?;
            lgx.write(&path, WireType { code, handle: None }, n, Bytes::from(data)).await
        }
    }
}

// ---------------------------------------------------------------------------
// Commands

fn env(name: &str, default: &str) -> String {
    std::env::var(name).ok().filter(|v| !v.is_empty()).unwrap_or_else(|| default.to_string())
}

fn emit(v: &Value) {
    println!("{}", serde_json::to_string(v).unwrap());
}

fn capabilities() -> Value {
    json!({
        "schemaVersion": "1.0",
        "adapter": ADAPTER,
        "adapterVersion": env("ETHERNETIP_INTEROP_ADAPTER_VERSION", "dev"),
        "protocol": "ethernetip",
        "upstream": {
            "name": "rseip",
            "version": env("ETHERNETIP_INTEROP_UPSTREAM_VERSION", "unknown"),
            "revision": env("ETHERNETIP_INTEROP_UPSTREAM_REVISION", ""),
            "license": "MIT",
            "language": "Rust",
            "independentEngine": true,
        },
        "roles": {"server": false, "client": true},
        "fixtures": [],
        "clientOperations": OPERATIONS,
        "clientFeatures": {
            "listIdentityTcp": true, "listIdentityUdp": true, "listServices": true,
            "getAttributeSingle": true, "setAttributeSingle": true, "getAttributesAll": true,
            "multipleService": true, "unconnectedSend": true, "class3": true,
            "class1": false, "largeForwardOpen": false, "multicast": false, "inputOnly": false, "listenOnly": false,
            "logixTags": true, "readTagFragmented": true, "writeTagFragmented": true, "readModifyWrite": true,
            "tagList": true, "templates": true, "configData": false,
        },
        "serverFeatures": {},
    })
}

fn print_fixture(name: &str) -> u8 {
    if name.is_empty() || name.contains('/') || name.contains('\\') || name.contains("..") {
        eprintln!("invalid fixture name {name}");
        return 2;
    }
    match std::fs::read(format!("/fixtures/{name}/fixture.json")) {
        Ok(b) => {
            use std::io::Write;
            let mut o = std::io::stdout();
            let _ = o.write_all(&b);
            let _ = o.flush();
            0
        }
        Err(_) => {
            eprintln!("no fixture {name}");
            2
        }
    }
}

fn main() -> std::process::ExitCode {
    std::process::ExitCode::from(real_main())
}

fn real_main() -> u8 {
    let argv: Vec<String> = std::env::args().skip(1).collect();
    let usage = "usage: ethernetip-interop client <op> --host HOST ... | print-capabilities | print-fixture [NAME] | version";
    let Some(cmd) = argv.first() else {
        eprintln!("{usage}");
        return 2;
    };
    match cmd.as_str() {
        "print-capabilities" if argv.len() == 1 => {
            emit(&capabilities());
            0
        }
        "version" if argv.len() == 1 => {
            emit(&json!({
                "adapter": ADAPTER,
                "adapterVersion": env("ETHERNETIP_INTEROP_ADAPTER_VERSION", "dev"),
                "upstream": "rseip",
                "upstreamVersion": env("ETHERNETIP_INTEROP_UPSTREAM_VERSION", "unknown"),
                "upstreamRevision": env("ETHERNETIP_INTEROP_UPSTREAM_REVISION", ""),
                "contract": "1.0",
            }));
            0
        }
        "print-fixture" if argv.len() <= 2 => print_fixture(argv.get(1).map(String::as_str).unwrap_or("adapter")),
        "server" => {
            eprintln!("the eiprs adapter has no server role");
            2
        }
        "client" => {
            let Some(op) = argv.get(1).filter(|o| OPERATIONS.contains(&o.as_str())) else {
                eprintln!("unknown or unsupported operation; operations: {}", OPERATIONS.join(", "));
                return 2;
            };
            let args = match parse_args(op, &argv[2..]) {
                Ok(a) => a,
                Err(m) => {
                    eprintln!("{m}");
                    return 2;
                }
            };
            let rt = match tokio::runtime::Builder::new_current_thread().enable_all().build() {
                Ok(rt) => rt,
                Err(e) => {
                    eprintln!("runtime: {e}");
                    return 1;
                }
            };
            let mut doc = Map::new();
            doc.insert("schemaVersion".into(), json!("1.0"));
            doc.insert("adapter".into(), json!(ADAPTER));
            doc.insert("operation".into(), json!(op));
            let res = rt.block_on(run(op, &args, &mut doc));
            match res {
                Ok(()) => {
                    doc.insert("ok".into(), json!(true));
                    doc.insert("error".into(), Value::Null);
                    emit(&Value::Object(doc));
                    0
                }
                Err(e) if e.code == "usage" => {
                    eprintln!("{}", e.msg);
                    2
                }
                Err(e) => {
                    eprintln!("eiprs: {}: {}", e.code, e.msg);
                    let mut d = Map::new();
                    for k in ["schemaVersion", "adapter", "operation"] {
                        d.insert(k.into(), doc[k].clone());
                    }
                    d.insert("ok".into(), json!(false));
                    d.insert("error".into(), json!({"code": e.code, "message": e.msg}));
                    emit(&Value::Object(d));
                    1
                }
            }
        }
        _ => {
            eprintln!("{usage}");
            2
        }
    }
}
