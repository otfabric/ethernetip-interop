// SPDX-License-Identifier: MIT
package interop;

import java.io.EOFException;
import java.io.IOException;
import java.io.PrintStream;
import java.lang.reflect.Field;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.net.ConnectException;
import java.net.NoRouteToHostException;
import java.net.SocketTimeoutException;
import java.net.UnknownHostException;
import java.nio.file.Files;
import java.nio.file.NoSuchFileException;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.HashMap;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.logging.Level;
import java.util.logging.Logger;

import etherip.EtherNetIP;
import etherip.data.CipException;
import etherip.data.Identity;
import etherip.protocol.ListServicesProtocol.Service;
import etherip.protocol.MRChipReadProtocol;
import etherip.types.CIPData;
import etherip.types.CNClassPath;

/**
 * ethernetip-interop adapter for etherip (client role only).
 *
 * Runs one operation of the container contract (docs/CONTAINER_CONTRACT.md)
 * with etherip (the EtherNet/IP library of EPICS) as the originator and
 * prints one JSON result document on stdout. etherip's java.util.logging
 * output goes to stderr.
 */
public final class EtheripInterop
{
    static final String ADAPTER = "etherip";

    static final List<String> OPERATIONS = Arrays.asList(
            "list-identity", "list-services", "get-attribute", "get-attributes-all",
            "identity", "read-tag", "read-tags", "write-tag");

    /** Flags each operation takes besides --host, --port, --timeout-ms. */
    static final Map<String, Set<String>> FLAGS = new HashMap<>();
    static
    {
        FLAGS.put("list-identity", set("transport"));
        FLAGS.put("list-services", set("transport"));
        FLAGS.put("get-attribute", set("class", "instance", "attribute", "route"));
        FLAGS.put("get-attributes-all", set("class", "instance", "route"));
        FLAGS.put("identity", set("route"));
        FLAGS.put("read-tag", set("tag", "count", "type", "route"));
        FLAGS.put("read-tags", set("tags", "route"));
        FLAGS.put("write-tag", set("tag", "type", "values", "route"));
    }

    static final Set<String> WRITE_TYPES = set("BOOL", "SINT", "INT", "DINT", "REAL", "STRING");

    private static Set<String> set(final String... s)
    {
        return new HashSet<>(Arrays.asList(s));
    }

    static final class Usage extends Exception
    {
        private static final long serialVersionUID = 1L;

        Usage(final String m)
        {
            super(m);
        }
    }

    private static PrintStream out;

    public static void main(final String[] argv)
    {
        // stdout carries JSON only: anything else that prints goes to stderr.
        out = System.out;
        System.setOut(System.err);
        configureLogging();
        int code;
        try
        {
            code = run(argv);
        }
        catch (final Usage u)
        {
            System.err.println("usage: " + u.getMessage());
            code = 2;
        }
        out.flush();
        System.exit(code);
    }

    private static void configureLogging()
    {
        final String lvl = System.getenv("ETHERIP_LOG_LEVEL");
        Level level = Level.WARNING;
        if (lvl != null && !lvl.isEmpty())
        {
            try
            {
                level = Level.parse(lvl);
            }
            catch (final IllegalArgumentException e)
            {
                // keep WARNING
            }
        }
        final Logger root = Logger.getLogger("");
        root.setLevel(level);
        for (final java.util.logging.Handler h : root.getHandlers())
            h.setLevel(level); // the default ConsoleHandler writes to stderr
    }

    private static String env(final String name, final String dflt)
    {
        final String v = System.getenv(name);
        return v == null || v.isEmpty() ? dflt : v;
    }

    static int run(final String[] argv) throws Usage
    {
        if (argv.length == 0)
            throw new Usage("ethernetip-interop client <op> --host HOST ... | print-capabilities | print-fixture [NAME] | version");
        switch (argv[0])
        {
        case "print-capabilities":
            if (argv.length != 1)
                throw new Usage("print-capabilities takes no arguments");
            emit(capabilities());
            return 0;
        case "version":
        {
            if (argv.length != 1)
                throw new Usage("version takes no arguments");
            final Map<String, Object> v = new LinkedHashMap<>();
            v.put("adapter", ADAPTER);
            v.put("adapterVersion", env("ETHERNETIP_INTEROP_ADAPTER_VERSION", "dev"));
            v.put("upstream", "etherip");
            v.put("upstreamVersion", env("ETHERNETIP_INTEROP_UPSTREAM_VERSION", "unknown"));
            v.put("upstreamRevision", env("ETHERNETIP_INTEROP_UPSTREAM_REVISION", ""));
            v.put("contract", "1.0");
            emit(v);
            return 0;
        }
        case "print-fixture":
            if (argv.length > 2)
                throw new Usage("print-fixture [NAME]");
            return printFixture(argv.length > 1 ? argv[1] : "adapter");
        case "server":
            throw new Usage("the etherip adapter has no server role");
        case "client":
            break;
        default:
            throw new Usage("unknown command " + argv[0]);
        }
        if (argv.length < 2 || !OPERATIONS.contains(argv[1]))
            throw new Usage("unknown or unsupported operation " + (argv.length < 2 ? "" : argv[1]));
        final String op = argv[1];
        final Args a = Args.parse(op, Arrays.copyOfRange(argv, 2, argv.length));
        return client(op, a);
    }

    // ---- arguments ----

    static final class Args
    {
        String host;
        int port = 44818;
        int timeoutMs = 5000;
        String transport = "tcp";
        int cls, instance, attribute;
        Integer slot; // --route 1/N: Unconnected_Send to backplane slot N
        String tag;
        String tags;
        int count = 1;
        String type;
        String values;

        static Args parse(final String op, final String[] v) throws Usage
        {
            final Args a = new Args();
            final Set<String> allowed = FLAGS.get(op);
            final Set<String> seen = new HashSet<>();
            for (int i = 0; i < v.length; ++i)
            {
                final String f = v[i];
                if (!f.startsWith("--"))
                    throw new Usage("unexpected argument " + f);
                final String name = f.substring(2);
                final boolean common = name.equals("host") || name.equals("port") || name.equals("timeout-ms");
                if (!common && !allowed.contains(name))
                    throw new Usage("unknown flag " + f + " for " + op
                            + (name.equals("connected") ? " (etherip has no Class 3 connections)" : ""));
                if (i + 1 >= v.length)
                    throw new Usage(f + " needs a value");
                final String val = v[++i];
                if (!seen.add(name))
                    throw new Usage(f + " given twice");
                switch (name)
                {
                case "host":
                    if (val.isEmpty())
                        throw new Usage("--host is empty");
                    a.host = val;
                    break;
                case "port":
                    a.port = num(f, val, 1, 65535);
                    break;
                case "timeout-ms":
                    a.timeoutMs = num(f, val, 1, 3_600_000);
                    break;
                case "transport":
                    if (!val.equals("tcp") && !val.equals("udp"))
                        throw new Usage("--transport tcp|udp");
                    a.transport = val;
                    break;
                case "class":
                    a.cls = num(f, val, 0, 0xFFFF);
                    break;
                case "instance":
                    a.instance = num(f, val, 0, 0xFFFF);
                    break;
                case "attribute":
                    a.attribute = num(f, val, 0, 0xFFFF);
                    break;
                case "route":
                {
                    // etherip routes through the backplane only: port 1, one slot.
                    final String[] p = val.split("/");
                    if (p.length != 2 || !p[0].equals("1"))
                        throw new Usage("--route: etherip routes to port 1 (backplane) only: 1/SLOT");
                    a.slot = num(f, p[1], 0, 255);
                    break;
                }
                case "tag":
                    if (val.isEmpty())
                        throw new Usage("--tag is empty");
                    a.tag = val;
                    break;
                case "tags":
                    a.tags = val;
                    break;
                case "count":
                    a.count = num(f, val, 1, 0x7FFF);
                    break;
                case "type":
                    a.type = val;
                    break;
                case "values":
                    a.values = val;
                    break;
                default:
                    throw new Usage("unknown flag " + f);
                }
            }
            if (a.host == null)
                throw new Usage("--host is required");
            switch (op)
            {
            case "read-tag":
                if (a.tag == null)
                    throw new Usage("read-tag needs --tag");
                break;
            case "read-tags":
                if (a.tags == null || a.tags.isEmpty())
                    throw new Usage("read-tags needs --tags");
                for (final String t : a.tags.split(",", -1))
                    if (t.isEmpty())
                        throw new Usage("--tags: empty tag name");
                break;
            case "write-tag":
                if (a.tag == null || a.type == null || a.values == null)
                    throw new Usage("write-tag needs --tag, --type and --values");
                if (!WRITE_TYPES.contains(a.type))
                    throw new Usage("--type: etherip writes " + WRITE_TYPES);
                break;
            default:
                break;
            }
            return a;
        }

        static int num(final String flag, final String s, final int min, final int max) throws Usage
        {
            long v;
            try
            {
                v = s.startsWith("0x") || s.startsWith("0X") ? Long.parseLong(s.substring(2), 16) : Long.parseLong(s);
            }
            catch (final NumberFormatException e)
            {
                throw new Usage(flag + ": not a number: " + s);
            }
            if (v < min || v > max)
                throw new Usage(flag + ": out of range: " + s);
            return (int) v;
        }
    }

    // ---- client ----

    private static Map<String, Object> head(final String op)
    {
        final Map<String, Object> d = new LinkedHashMap<>();
        d.put("schemaVersion", "1.0");
        d.put("adapter", ADAPTER);
        d.put("operation", op);
        d.put("ok", false);
        d.put("error", null);
        return d;
    }

    /** An EtherNetIP of etherip on the contract's transport. */
    private static EtherNetIP open(final Args a, final boolean udp, final boolean register) throws Exception
    {
        final EtherNetIP eip = new EtherNetIP(a.host, a.slot == null ? 0 : a.slot);
        final InteropConnection c = new InteropConnection(a.host, a.port, a.timeoutMs, udp);
        // EtherNetIP builds its own TcpConnection in connectTcp(), fixed to port
        // 44818 and 2 s; it has no setter, so its connection field is set here.
        final Field f = EtherNetIP.class.getDeclaredField("connection");
        f.setAccessible(true);
        f.set(eip, c);
        if (register)
        {
            // What connectTcp() does after connecting: EtherNetIP.registerSession().
            final Method m = EtherNetIP.class.getDeclaredMethod("registerSession");
            m.setAccessible(true);
            try
            {
                m.invoke(eip);
            }
            catch (final InvocationTargetException e)
            {
                throw (e.getCause() instanceof Exception) ? (Exception) e.getCause() : e;
            }
        }
        return eip;
    }

    private static void close(final EtherNetIP eip)
    {
        if (eip == null)
            return;
        try
        {
            eip.close(); // UnRegisterSession, then close
        }
        catch (final Exception e)
        {
            Logger.getLogger(EtheripInterop.class.getName()).log(Level.FINE, "close", e);
        }
    }

    /** A CIP object path of any class: CNClassPath's class constructor is protected. */
    static final class AnyClass extends CNClassPath
    {
        AnyClass(final int cls)
        {
            super(cls, String.format("Class 0x%X", cls));
        }
    }

    static int client(final String op, final Args a)
    {
        final Map<String, Object> doc = head(op);
        EtherNetIP eip = null;
        boolean connected = false;
        try
        {
            final boolean udp = a.transport.equals("udp");
            final boolean register = !op.equals("list-identity") && !op.equals("list-services") || !udp;
            eip = open(a, udp, register);
            connected = true;
            switch (op)
            {
            case "list-identity":
            {
                final Identity[] ids = eip.listIdentity();
                if (ids == null || ids.length == 0)
                    return fail(doc, "failed", "ListIdentity reply without an identity");
                doc.put("identity", identity(ids[0], true));
                break;
            }
            case "list-services":
            {
                final Service[] svcs = eip.listServices();
                final List<Object> list = new ArrayList<>();
                for (final Service s : svcs)
                {
                    final Map<String, Object> m = new LinkedHashMap<>();
                    m.put("name", s.getName());
                    // Service has no getters for these: read its fields.
                    m.put("version", Short.toUnsignedInt(shortField(s, "version")));
                    m.put("capabilities", Short.toUnsignedInt(shortField(s, "flags")));
                    list.add(m);
                }
                doc.put("services", list);
                break;
            }
            case "get-attribute":
            case "get-attributes-all":
            {
                final CNClassPath path = new AnyClass(a.cls);
                String hex;
                try
                {
                    if (op.equals("get-attribute"))
                        hex = a.slot == null ? eip.getHexStringAttribute(path, a.instance, a.attribute)
                                : eip.getHexStringAttribute(a.slot, path, a.instance, a.attribute);
                    else
                        hex = a.slot == null ? eip.getHexStringAttributeAll(path, a.instance)
                                : eip.getHexStringAttributeAll(a.slot, path, a.instance);
                    doc.put("status", 0);
                    doc.put("extendedStatus", new ArrayList<>());
                    doc.put("data", hex == null ? "" : hex.replace(" ", "").toLowerCase());
                }
                catch (final CipException e)
                {
                    cipStatus(doc, e);
                    doc.put("data", "");
                }
                break;
            }
            case "identity":
            {
                try
                {
                    final Identity id = a.slot == null ? eip.getIdentity() : eip.getSlotIdentity(a.slot);
                    doc.put("identity", identity(id, false));
                }
                catch (final CipException e)
                {
                    return fail(doc, "failed", "CIP error: " + e.getMessage());
                }
                break;
            }
            case "read-tag":
            {
                final MRChipReadProtocol read = new MRChipReadProtocol(a.tag, (short) a.count);
                try
                {
                    eip.executeRequest(read); // Unconnected_Send to the slot (default 0)
                    doc.put("status", read.getStatus() & 0xFF); // 0, or 6 (partial transfer)
                    putData(doc, read.getData());
                }
                catch (final CipException e)
                {
                    cipStatus(doc, e);
                }
                break;
            }
            case "read-tags":
            {
                final String[] names = a.tags.split(",");
                final List<Object> results = new ArrayList<>();
                try
                {
                    final CIPData[] data = eip.readTags(names); // Multiple Service Packet
                    for (int i = 0; i < names.length; ++i)
                    {
                        final Map<String, Object> r = new LinkedHashMap<>();
                        r.put("tag", names[i]);
                        putData(r, data[i]);
                        r.put("status", 0);
                        results.add(r);
                    }
                    doc.put("status", 0);
                }
                catch (final CipException e)
                {
                    // etherip throws on the packet status (0x1E when an embedded service
                    // failed) before it decodes any embedded reply.
                    cipStatus(doc, e);
                    for (final String n : names)
                    {
                        final Map<String, Object> r = new LinkedHashMap<>();
                        r.put("tag", n);
                        r.put("values", new ArrayList<>());
                        r.put("status", -1);
                        r.put("errorText", "etherip: " + e.getMessage());
                        results.add(r);
                    }
                }
                doc.put("results", results);
                break;
            }
            case "write-tag":
            {
                try
                {
                    eip.writeTag(a.tag, writeData(eip, a));
                    doc.put("status", 0);
                }
                catch (final CipException e)
                {
                    cipStatus(doc, e);
                }
                catch (final Usage u)
                {
                    throw u;
                }
                break;
            }
            default:
                return fail(doc, "failed", "operation not implemented: " + op);
            }
            doc.put("ok", true);
            emit(doc);
            return 0;
        }
        catch (final Usage u)
        {
            System.err.println("usage: " + u.getMessage());
            return 2;
        }
        catch (final Exception e)
        {
            return fail(doc, errorCode(e, connected), e.getClass().getSimpleName() + ": " + e.getMessage());
        }
        finally
        {
            close(eip);
        }
    }

    private static String errorCode(final Throwable e, final boolean connected)
    {
        if (!connected && (e instanceof ConnectException || e instanceof UnknownHostException
                || e instanceof NoRouteToHostException || e instanceof SocketTimeoutException
                || e instanceof IOException))
            return "connect-failed";
        if (e instanceof SocketTimeoutException)
            return "timeout";
        if (e instanceof EOFException || e instanceof java.net.SocketException)
            return "connection-lost";
        return "failed";
    }

    private static int fail(final Map<String, Object> doc, final String code, final String message)
    {
        final Map<String, Object> err = new LinkedHashMap<>();
        err.put("code", code);
        err.put("message", message == null ? "" : message);
        doc.put("ok", false);
        doc.put("error", err);
        emit(doc);
        return 1;
    }

    /** etherip reports a non-zero general status as a CipException: the status and the first extended status word. */
    private static void cipStatus(final Map<String, Object> doc, final CipException e)
    {
        doc.put("status", e.getStatusCode());
        final List<Object> ext = new ArrayList<>();
        if (e.getExtendedStatusCode() != 0)
            ext.add(e.getExtendedStatusCode() & 0xFFFF);
        doc.put("extendedStatus", ext);
        doc.put("errorText", e.getMessage());
    }

    private static short shortField(final Object o, final String name) throws Exception
    {
        final Field f = o.getClass().getDeclaredField(name);
        f.setAccessible(true);
        return f.getShort(o);
    }

    /** etherip's Identity as a contract identity. etherip does not decode the socket address or the state. */
    private static Map<String, Object> identity(final Identity id, final boolean withState)
    {
        final Map<String, Object> m = new LinkedHashMap<>();
        m.put("vendorId", id.getVendorId());
        m.put("deviceType", id.getDeviceTypeRaw());
        m.put("productCode", id.getProductCode());
        m.put("revision", id.getMajorRevision() + "." + id.getMinorRevision());
        m.put("status", hexInt(id.getStatusRaw()));
        final Long serial = hexLong(id.getSerialNumberRaw());
        m.put("serialNumber", serial);
        m.put("productName", id.getProductName());
        if (withState)
            m.put("state", null);
        m.put("socketAddress", null);
        return m;
    }

    private static Integer hexInt(final String s)
    {
        final Long v = hexLong(s);
        return v == null ? null : v.intValue();
    }

    private static Long hexLong(final String s)
    {
        if (s == null)
            return null;
        final String t = s.startsWith("0x") || s.startsWith("0X") ? s.substring(2) : s;
        return Long.parseLong(t, 16);
    }

    /** etherip's CIPData of a Read Tag reply: type and values. */
    private static void putData(final Map<String, Object> m, final CIPData d) throws Exception
    {
        if (d == null)
        {
            m.put("type", null);
            m.put("values", new ArrayList<>());
            return;
        }
        m.put("type", d.getType().name());
        final List<Object> vals = new ArrayList<>();
        if (d.isNumeric())
        {
            for (int i = 0; i < d.getElementCount(); ++i)
            {
                final Number n = d.getNumber(i);
                if (n instanceof Float)
                {
                    final float f = n.floatValue();
                    vals.add(Float.isFinite(f) ? new java.math.BigDecimal(Float.toString(f)) : null);
                }
                else
                    vals.add(n);
            }
        }
        else
        {
            try
            {
                vals.add(d.getString());
            }
            catch (final Exception e)
            {
                // A structure other than STRING: etherip has no templates.
                m.put("errorText", "etherip: " + e.getMessage());
            }
        }
        m.put("values", vals);
    }

    /** The CIPData to write: elementary values, or a STRING the way etherip writes one (read, setString, write). */
    private static CIPData writeData(final EtherNetIP eip, final Args a) throws Exception
    {
        if (a.type.equals("STRING"))
        {
            final CIPData cur = eip.readTag(a.tag);
            if (cur == null || cur.getType() != CIPData.Type.STRUCT)
                throw new Exception("etherip writes a STRING over the structure it read, and " + a.tag
                        + " read as " + (cur == null ? "nothing" : cur.getType()));
            cur.setString(a.values);
            return cur;
        }
        final String[] parts = a.values.split(",", -1);
        final CIPData.Type t = CIPData.Type.valueOf(a.type);
        final CIPData d = new CIPData(t, parts.length);
        for (int i = 0; i < parts.length; ++i)
        {
            final String p = parts[i].trim();
            try
            {
                switch (t)
                {
                case BOOL:
                    if (p.equalsIgnoreCase("true") || p.equals("1"))
                        d.set(i, 1);
                    else if (p.equalsIgnoreCase("false") || p.equals("0"))
                        d.set(i, 0);
                    else
                        throw new Usage("--values: BOOL true/false or 1/0, not " + p);
                    break;
                case REAL:
                    d.set(i, Float.parseFloat(p));
                    break;
                default:
                {
                    final long v = p.startsWith("0x") || p.startsWith("-0x") ? Long.decode(p) : Long.parseLong(p);
                    final long lo = t == CIPData.Type.SINT ? Byte.MIN_VALUE : t == CIPData.Type.INT ? Short.MIN_VALUE : Integer.MIN_VALUE;
                    final long hi = t == CIPData.Type.SINT ? Byte.MAX_VALUE : t == CIPData.Type.INT ? Short.MAX_VALUE : Integer.MAX_VALUE;
                    if (v < lo || v > hi)
                        throw new Usage("--values: " + p + " out of range for " + t.name());
                    d.set(i, v);
                }
                }
            }
            catch (final NumberFormatException e)
            {
                throw new Usage("--values: not a number: " + p);
            }
        }
        return d;
    }

    // ---- capabilities, fixtures ----

    private static Map<String, Object> capabilities()
    {
        final Map<String, Object> d = new LinkedHashMap<>();
        d.put("schemaVersion", "1.0");
        d.put("adapter", ADAPTER);
        d.put("adapterVersion", env("ETHERNETIP_INTEROP_ADAPTER_VERSION", "dev"));
        d.put("protocol", "ethernetip");
        final Map<String, Object> up = new LinkedHashMap<>();
        up.put("name", "etherip");
        up.put("version", env("ETHERNETIP_INTEROP_UPSTREAM_VERSION", "unknown"));
        up.put("revision", env("ETHERNETIP_INTEROP_UPSTREAM_REVISION", ""));
        up.put("license", "EPL-1.0");
        up.put("language", "Java");
        up.put("independentEngine", true);
        d.put("upstream", up);
        final Map<String, Object> roles = new LinkedHashMap<>();
        roles.put("server", false);
        roles.put("client", true);
        d.put("roles", roles);
        d.put("fixtures", new ArrayList<>());
        d.put("clientOperations", OPERATIONS);
        final Map<String, Object> f = new LinkedHashMap<>();
        f.put("listIdentityTcp", true);
        f.put("listIdentityUdp", true);
        f.put("listServices", true);
        f.put("getAttributeSingle", true);
        f.put("setAttributeSingle", false);
        f.put("getAttributesAll", true);
        f.put("multipleService", false);
        f.put("unconnectedSend", true);
        f.put("class3", false);
        f.put("class1", false);
        f.put("largeForwardOpen", false);
        f.put("multicast", false);
        f.put("inputOnly", false);
        f.put("listenOnly", false);
        f.put("logixTags", true);
        f.put("readTagFragmented", false);
        f.put("writeTagFragmented", false);
        f.put("readModifyWrite", false);
        f.put("tagList", false);
        f.put("templates", false);
        f.put("configData", false);
        d.put("clientFeatures", f);
        d.put("serverFeatures", new LinkedHashMap<>());
        return d;
    }

    private static int printFixture(final String name)
    {
        if (name.isEmpty() || name.contains("/") || name.contains("\\") || name.contains(".."))
        {
            System.err.println("invalid fixture name " + name);
            return 2;
        }
        try
        {
            final byte[] b = Files.readAllBytes(Paths.get("/fixtures", name, "fixture.json"));
            out.write(b, 0, b.length);
            out.flush();
            return 0;
        }
        catch (final NoSuchFileException e)
        {
            System.err.println("no fixture " + name);
            return 2;
        }
        catch (final IOException e)
        {
            System.err.println("cannot read fixture " + name + ": " + e.getMessage());
            return 2;
        }
    }

    // ---- JSON ----

    private static void emit(final Map<String, Object> doc)
    {
        final StringBuilder b = new StringBuilder();
        json(b, doc);
        out.print(b.append('\n'));
        out.flush();
    }

    @SuppressWarnings("unchecked")
    private static void json(final StringBuilder b, final Object v)
    {
        if (v == null)
            b.append("null");
        else if (v instanceof String)
            str(b, (String) v);
        else if (v instanceof Boolean || v instanceof Number)
            b.append(v.toString());
        else if (v instanceof Map)
        {
            b.append('{');
            boolean first = true;
            for (final Map.Entry<String, Object> e : ((Map<String, Object>) v).entrySet())
            {
                if (!first)
                    b.append(',');
                first = false;
                str(b, e.getKey());
                b.append(':');
                json(b, e.getValue());
            }
            b.append('}');
        }
        else if (v instanceof List)
        {
            b.append('[');
            boolean first = true;
            for (final Object o : (List<Object>) v)
            {
                if (!first)
                    b.append(',');
                first = false;
                json(b, o);
            }
            b.append(']');
        }
        else
            str(b, v.toString());
    }

    private static void str(final StringBuilder b, final String s)
    {
        b.append('"');
        for (int i = 0; i < s.length(); ++i)
        {
            final char c = s.charAt(i);
            switch (c)
            {
            case '"':
                b.append("\\\"");
                break;
            case '\\':
                b.append("\\\\");
                break;
            case '\n':
                b.append("\\n");
                break;
            case '\r':
                b.append("\\r");
                break;
            case '\t':
                b.append("\\t");
                break;
            default:
                if (c < 0x20 || c > 0x7E)
                    b.append(String.format("\\u%04x", (int) c));
                else
                    b.append(c);
            }
        }
        b.append('"');
    }
}
