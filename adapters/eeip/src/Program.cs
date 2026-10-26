// SPDX-License-Identifier: MIT
//
// ethernetip-interop adapter for EEIP.NET (client role only): runs one
// operation of the container contract (docs/CONTAINER_CONTRACT.md) with
// EEIP.NET's EEIPClient as the originator and prints one JSON result
// document on stdout. Everything else (EEIP.NET's own Console output, the
// adapter's diagnostics) goes to stderr.

using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Globalization;
using System.IO;
using System.Linq;
using System.Net;
using System.Net.Sockets;
using System.Reflection;
using System.Text;
using System.Text.RegularExpressions;
using System.Threading;
using System.Threading.Tasks;
using Sres.Net.EEIP;

namespace EthernetIpInterop.Eeip
{
    sealed class UsageException : Exception
    {
        public UsageException(string m) : base(m) { }
    }

    // A failed operation: ok false with this error code.
    sealed class OpFailure : Exception
    {
        public readonly string Code;
        public OpFailure(string code, string m) : base(m) { Code = code; }
    }

    // Json builds one JSON object in field order.
    sealed class Json
    {
        readonly StringBuilder sb = new StringBuilder("{");
        bool first = true;

        public static string Str(string s)
        {
            if (s == null) return "null";
            var o = new StringBuilder("\"");
            foreach (char c in s)
            {
                switch (c)
                {
                    case '"': o.Append("\\\""); break;
                    case '\\': o.Append("\\\\"); break;
                    case '\n': o.Append("\\n"); break;
                    case '\r': o.Append("\\r"); break;
                    case '\t': o.Append("\\t"); break;
                    default:
                        if (c < 0x20 || c > 0x7e) o.Append("\\u").Append(((int)c).ToString("x4"));
                        else o.Append(c);
                        break;
                }
            }
            return o.Append('"').ToString();
        }

        public Json Raw(string k, string raw)
        {
            sb.Append(first ? "" : ",").Append(Str(k)).Append(':').Append(raw);
            first = false;
            return this;
        }

        public Json S(string k, string v) => Raw(k, Str(v));
        public Json N(string k, long v) => Raw(k, v.ToString(CultureInfo.InvariantCulture));
        public Json B(string k, bool v) => Raw(k, v ? "true" : "false");
        public Json O(string k, Json v) => Raw(k, v.ToString());
        public Json A(string k, IEnumerable<long> v) => Raw(k, "[" + string.Join(",", v) + "]");
        public override string ToString() => sb.ToString() + "}";
    }

    static class Program
    {
        const string Adapter = "eeip";
        static TextWriter stdout;

        static readonly string[] Ops = { "list-identity", "get-attribute", "set-attribute", "get-attributes-all", "identity", "io" };

        static readonly HashSet<string> ValueFlags = new HashSet<string> {
            "host", "port", "timeout-ms", "transport", "class", "instance", "attribute", "data", "config", "output",
            "input", "output-size", "input-size", "rpi-ms", "duration-ms", "output-data" };
        static readonly HashSet<string> NumericFlags = new HashSet<string> {
            "port", "timeout-ms", "class", "instance", "attribute", "config", "output", "input", "output-size",
            "input-size", "rpi-ms", "duration-ms" };
        static readonly HashSet<string> Switches = new HashSet<string> { "large", "multicast" };

        static string Env(string k, string def) => Environment.GetEnvironmentVariable(k) ?? def;

        static int Main(string[] argv)
        {
            stdout = new StreamWriter(Console.OpenStandardOutput(), new UTF8Encoding(false)) { AutoFlush = true };
            // EEIP.NET writes its banner with Console.WriteLine: to stderr.
            Console.SetOut(Console.Error);
            int code;
            try
            {
                code = Run(argv);
            }
            catch (UsageException e)
            {
                Console.Error.WriteLine("ethernetip-interop: " + e.Message);
                code = 2;
            }
            stdout.Flush();
            Console.Error.Flush();
            // EEIP.NET's I/O threads and pending receives must not outlive
            // the operation.
            Environment.Exit(code);
            return code;
        }

        static int Run(string[] argv)
        {
            if (argv.Length == 0) throw new UsageException("usage: ethernetip-interop client <op> --host HOST ... | print-capabilities | print-fixture [NAME] | version");
            switch (argv[0])
            {
                case "print-capabilities": return Capabilities();
                case "version":
                    stdout.WriteLine(new Json().S("adapter", Adapter).S("adapterVersion", Env("ETHERNETIP_INTEROP_ADAPTER_VERSION", "dev"))
                        .S("upstream", "EEIP.NET").S("upstreamVersion", Env("ETHERNETIP_INTEROP_UPSTREAM_VERSION", ""))
                        .S("upstreamRevision", Env("ETHERNETIP_INTEROP_UPSTREAM_REVISION", "")).S("contract", "1.0"));
                    return 0;
                case "print-fixture": return PrintFixture(argv.Length > 1 ? argv[1] : "adapter");
                case "server": throw new UsageException("the eeip adapter has no server role");
                case "client": break;
                default: throw new UsageException("unknown command " + argv[0]);
            }
            if (argv.Length < 2) throw new UsageException("client needs an operation");
            var a = Parse(argv);
            try
            {
                return Client(a);
            }
            catch (UsageException) { throw; }
            catch (OpFailure f) { return Fail(a, f.Code, f.Message); }
            catch (Exception e) { return Fail(a, "failed", e.GetType().Name + ": " + e.Message); }
        }

        // ---- arguments ----

        sealed class Args
        {
            public string Op;
            public Dictionary<string, string> Kv = new Dictionary<string, string>();
            public bool Has(string k) => Kv.ContainsKey(k);
            public string Get(string k, string def = "") => Kv.TryGetValue(k, out var v) ? v : def;
            public long Num(string k, long def) => Kv.TryGetValue(k, out var v) ? ParseNum(v) : def;
        }

        static long ParseNum(string v)
        {
            if (v.StartsWith("0x", StringComparison.OrdinalIgnoreCase))
                return long.Parse(v.Substring(2), NumberStyles.AllowHexSpecifier, CultureInfo.InvariantCulture);
            return long.Parse(v, NumberStyles.None, CultureInfo.InvariantCulture);
        }

        static byte[] FromHex(string s)
        {
            if (s.Length % 2 != 0) throw new FormatException("odd hex length");
            var o = new byte[s.Length / 2];
            for (int i = 0; i < o.Length; i++)
                o[i] = byte.Parse(s.Substring(2 * i, 2), NumberStyles.AllowHexSpecifier, CultureInfo.InvariantCulture);
            return o;
        }

        static string Hex(byte[] d, int n = -1)
        {
            if (d == null) return "";
            if (n < 0 || n > d.Length) n = d.Length;
            var sb = new StringBuilder(2 * n);
            for (int i = 0; i < n; i++) sb.Append(d[i].ToString("x2"));
            return sb.ToString();
        }

        static Args Parse(string[] argv)
        {
            var a = new Args { Op = argv[1] };
            if (!Ops.Contains(a.Op)) throw new UsageException("unknown operation " + a.Op);
            for (int i = 2; i < argv.Length; i++)
            {
                if (!argv[i].StartsWith("--")) throw new UsageException("unexpected argument " + argv[i]);
                var k = argv[i].Substring(2);
                if (Switches.Contains(k)) { a.Kv[k] = "1"; continue; }
                if (k == "connected") throw new UsageException("--connected needs a feature EEIP.NET does not have (Class 3)");
                if (k == "config-data") throw new UsageException("--config-data needs a feature EEIP.NET does not have (its connection path has no data segment)");
                if (!ValueFlags.Contains(k)) throw new UsageException("unknown flag --" + k);
                if (i + 1 >= argv.Length) throw new UsageException("flag --" + k + " needs a value");
                var v = argv[++i];
                if (NumericFlags.Contains(k))
                {
                    try { if (ParseNum(v) < 0) throw new FormatException(); }
                    catch (Exception) { throw new UsageException("--" + k + ": not a number: " + v); }
                }
                if (k == "data" || k == "output-data")
                {
                    try { FromHex(v); }
                    catch (Exception) { throw new UsageException("--" + k + ": not hex: " + v); }
                }
                a.Kv[k] = v;
            }
            if (a.Has("transport") && a.Get("transport") != "udp" && a.Get("transport") != "tcp")
                throw new UsageException("--transport must be tcp or udp");
            if (a.Op == "list-identity" && a.Get("transport", "udp") != "udp")
                throw new UsageException("EEIP.NET sends ListIdentity over UDP only");
            if (a.Num("timeout-ms", 5000) == 0) throw new UsageException("--timeout-ms must be positive");
            if (a.Num("port", 44818) > 0xFFFF || a.Num("port", 44818) == 0) throw new UsageException("--port out of range");
            // EEIP.NET packs logical segments of at most 16 bits.
            foreach (var k in new[] { "class", "instance", "attribute" })
                if (a.Num(k, 0) > 0xFFFF) throw new UsageException("--" + k + " above 0xFFFF");
            if (a.Op == "io")
            {
                if (!a.Has("output") || !a.Has("input")) throw new UsageException("io needs --output and --input");
                // EEIP.NET packs the connection points as 8-bit instance segments.
                foreach (var k in new[] { "config", "output", "input" })
                    if (a.Num(k, 0) > 0xFF) throw new UsageException("--" + k + ": EEIP.NET packs 8-bit connection points only");
                // Its I/O buffers hold 505 octets.
                foreach (var k in new[] { "output-size", "input-size" })
                    if (a.Num(k, 0) > 505) throw new UsageException("--" + k + ": EEIP.NET's I/O buffers hold at most 505 octets");
                if (a.Num("rpi-ms", 10) == 0 || a.Num("rpi-ms", 10) > 4294967) throw new UsageException("--rpi-ms out of range");
                if (a.Has("output-data") && a.Get("output-data").Length / 2 != a.Num("output-size", 0))
                    throw new UsageException("--output-data must be --output-size octets of hex");
            }
            if (!a.Has("host")) throw new UsageException("--host is required");
            return a;
        }

        // ---- output ----

        static Json Head(Args a) => new Json().S("schemaVersion", "1.0").S("adapter", Adapter).S("operation", a.Op);

        static Json Ok(Args a) => Head(a).B("ok", true).Raw("error", "null");

        static int Fail(Args a, string code, string msg)
        {
            stdout.WriteLine(Head(a).B("ok", false).O("error", new Json().S("code", code).S("message", msg)));
            return 1;
        }

        static int Capabilities()
        {
            var ops = string.Join(",", Ops.Select(Json.Str));
            var features = new Json()
                .B("listIdentityTcp", false).B("listIdentityUdp", true).B("listServices", false)
                .B("getAttributeSingle", true).B("setAttributeSingle", true).B("getAttributesAll", true)
                .B("multipleService", false).B("unconnectedSend", false).B("class3", false).B("class1", true)
                .B("largeForwardOpen", true).B("multicast", true).B("inputOnly", true).B("listenOnly", true)
                .B("logixTags", false).B("readTagFragmented", false).B("tagList", false).B("configData", false);
            var doc = new Json().S("schemaVersion", "1.0").S("adapter", Adapter)
                .S("adapterVersion", Env("ETHERNETIP_INTEROP_ADAPTER_VERSION", "dev")).S("protocol", "ethernetip")
                .O("upstream", new Json().S("name", "EEIP.NET").S("version", Env("ETHERNETIP_INTEROP_UPSTREAM_VERSION", ""))
                    .S("revision", Env("ETHERNETIP_INTEROP_UPSTREAM_REVISION", "")).S("license", "MIT").S("language", "C#")
                    .B("independentEngine", true))
                .O("roles", new Json().B("server", false).B("client", true))
                .Raw("fixtures", "[]").Raw("clientOperations", "[" + ops + "]")
                .O("clientFeatures", features).Raw("serverFeatures", "{}");
            stdout.WriteLine(doc);
            return 0;
        }

        static int PrintFixture(string name)
        {
            if (name.Length == 0 || name.Contains('/') || name.Contains('\\') || name.Contains(".."))
                throw new UsageException("invalid fixture name " + name);
            var path = "/fixtures/" + name + "/fixture.json";
            if (!File.Exists(path)) throw new UsageException("no fixture " + name);
            using (var o = Console.OpenStandardOutput())
            using (var f = File.OpenRead(path))
                f.CopyTo(o);
            return 0;
        }

        // ---- EEIP.NET's status reporting ----
        //
        // EEIP.NET throws a CIPException for a non-zero general status, with
        // the status as text from its own table (GeneralStatusCodes). The
        // code is recovered from that same table; a text it does not have
        // ("unknown", for codes above 0x2B) gives -1.

        static Dictionary<string, int> statusByText;

        static Dictionary<string, int> StatusTable()
        {
            if (statusByText != null) return statusByText;
            statusByText = new Dictionary<string, int>();
            var t = typeof(EEIPClient).Assembly.GetType("Sres.Net.EEIP.GeneralStatusCodes");
            var m = t?.GetMethod("GetStatusCode", BindingFlags.Static | BindingFlags.NonPublic | BindingFlags.Public);
            if (m == null) return statusByText;
            for (int c = 1; c < 0x100; c++)
            {
                var s = (string)m.Invoke(null, new object[] { (byte)c });
                if (s != "unknown" && !statusByText.ContainsKey(s)) statusByText[s] = c;
            }
            return statusByText;
        }

        static readonly Regex ConnFailure = new Regex(@"^Connection failure, General Status Code: (\d+)(?: Additional Status Code: (\d+))?");

        // StatusOf returns the general status and the additional status words
        // EEIP.NET's exception carries.
        static (int status, List<long> ext) StatusOf(CIPException e)
        {
            var m = ConnFailure.Match(e.Message);
            if (m.Success)
            {
                var ext = new List<long>();
                if (m.Groups[2].Success) ext.Add(long.Parse(m.Groups[2].Value, CultureInfo.InvariantCulture));
                return (int.Parse(m.Groups[1].Value, CultureInfo.InvariantCulture), ext);
            }
            return (StatusTable().TryGetValue(e.Message, out var c) ? c : -1, new List<long>());
        }

        // ---- bounded calls ----
        //
        // EEIP.NET has no timeouts: its TcpClient connect and stream reads
        // block. Every call into it runs on a worker and is abandoned (the
        // process exits after the result) when --timeout-ms passes.

        static T Bounded<T>(Func<T> f, int ms, string what)
        {
            var t = Task.Run(f);
            try
            {
                if (!t.Wait(ms)) throw new OpFailure("timeout", what + ": no reply within " + ms + " ms");
            }
            catch (AggregateException ae) when (ae.InnerException != null)
            {
                System.Runtime.ExceptionServices.ExceptionDispatchInfo.Capture(ae.InnerException).Throw();
            }
            return t.Result;
        }

        static void Bounded(Action f, int ms, string what) => Bounded(() => { f(); return 0; }, ms, what);

        static IPAddress Resolve(string host)
        {
            if (IPAddress.TryParse(host, out var ip) && ip.AddressFamily == AddressFamily.InterNetwork) return ip;
            try
            {
                var v4 = Dns.GetHostAddresses(host).FirstOrDefault(x => x.AddressFamily == AddressFamily.InterNetwork);
                if (v4 != null) return v4;
            }
            catch (Exception e)
            {
                throw new OpFailure("connect-failed", "cannot resolve " + host + ": " + e.Message);
            }
            throw new OpFailure("connect-failed", "no IPv4 address for " + host);
        }

        static EEIPClient Session(Args a, IPAddress ip, int timeout)
        {
            var c = new EEIPClient();
            try
            {
                Bounded(() => c.RegisterSession(ip.ToString(), (ushort)a.Num("port", 44818)), timeout, "RegisterSession");
            }
            catch (OpFailure f) when (f.Code == "timeout")
            {
                throw new OpFailure("connect-failed", f.Message);
            }
            catch (SocketException e)
            {
                throw new OpFailure("connect-failed", e.Message);
            }
            catch (IOException e)
            {
                throw new OpFailure("connect-failed", e.Message);
            }
            return c;
        }

        static void Close(EEIPClient c, int timeout)
        {
            try { Bounded(c.UnRegisterSession, timeout, "UnRegisterSession"); }
            catch (Exception e) { Console.Error.WriteLine("eeip: UnRegisterSession: " + e.Message); }
        }

        // ---- operations ----

        static int Client(Args a)
        {
            int timeout = (int)Math.Min(a.Num("timeout-ms", 5000), int.MaxValue);
            var ip = Resolve(a.Get("host"));
            if (a.Op == "list-identity") return ListIdentity(a, ip, timeout);
            var c = Session(a, ip, timeout);
            try
            {
                switch (a.Op)
                {
                    case "identity":
                        {
                            var id = Bounded(() => c.IdentityObject.InstanceAttributes, timeout, "Get_Attributes_All @1/1");
                            stdout.WriteLine(Ok(a).O("identity", new Json().N("vendorId", id.VendorID).N("deviceType", id.DeviceType)
                                .N("productCode", id.ProductCode).S("revision", id.Revision.MajorRevision + "." + id.Revision.MinorRevision)
                                .N("status", id.Status).N("serialNumber", id.SerialNumber).S("productName", id.ProductName)
                                .S("socketAddress", ip.ToString())));
                            return 0;
                        }
                    case "get-attribute":
                    case "set-attribute":
                    case "get-attributes-all":
                        {
                            int cls = (int)a.Num("class", 0), inst = (int)a.Num("instance", 0), attr = (int)a.Num("attribute", 0);
                            Func<byte[]> call;
                            if (a.Op == "get-attribute") call = () => c.GetAttributeSingle(cls, inst, attr);
                            else if (a.Op == "set-attribute")
                            {
                                var data = FromHex(a.Get("data"));
                                call = () => c.SetAttributeSingle(cls, inst, attr, data);
                            }
                            else call = () => c.GetAttributeAll(cls, inst);
                            byte[] reply;
                            try
                            {
                                reply = Bounded(call, timeout, a.Op);
                            }
                            catch (CIPException e)
                            {
                                var (st, ext) = StatusOf(e);
                                Console.Error.WriteLine("eeip: " + e.Message);
                                stdout.WriteLine(Ok(a).N("status", st).S("data", "").S("errorText", e.Message));
                                return 0;
                            }
                            stdout.WriteLine(Ok(a).N("status", 0).S("data", Hex(reply)));
                            return 0;
                        }
                    case "io":
                        return Io(a, c, timeout);
                }
            }
            finally
            {
                if (a.Op != "io") Close(c, timeout);
            }
            throw new UsageException("unknown operation " + a.Op);
        }

        // ListIdentity: EEIP.NET's ListIdentity() broadcasts the request on
        // the directed broadcast address of every Ethernet interface, to UDP
        // port 44818, and does not set SO_BROADCAST (Linux refuses the send).
        // The contract's list-identity is one unicast datagram to
        // --host:--port: the adapter sends EEIP.NET's 24-octet request
        // (command 0x63, the rest zero) itself and parses the reply with
        // EEIP.NET's CIPIdentityItem.getCIPIdentityItem, as EEIP.NET's
        // receive callback does.
        static int ListIdentity(Args a, IPAddress ip, int timeout)
        {
            var req = new Encapsulation { Command = Encapsulation.CommandsEnum.ListIdentity, Length = 0 }.toBytes();
            using (var u = new UdpClient(AddressFamily.InterNetwork))
            {
                var target = new IPEndPoint(ip, (int)a.Num("port", 44818));
                u.Send(req, req.Length, target);
                var deadline = Stopwatch.StartNew();
                while (true)
                {
                    int left = timeout - (int)deadline.ElapsedMilliseconds;
                    if (left <= 0) throw new OpFailure("timeout", "no ListIdentity reply");
                    var t = u.ReceiveAsync();
                    if (!t.Wait(left)) throw new OpFailure("timeout", "no ListIdentity reply");
                    var r = t.Result;
                    if (!r.RemoteEndPoint.Address.Equals(ip) || r.Buffer.Length < 26) continue;
                    if ((r.Buffer[0] | r.Buffer[1] << 8) != 0x63) continue;
                    var id = Encapsulation.CIPIdentityItem.getCIPIdentityItem(24, r.Buffer);
                    var sock = Encapsulation.CIPIdentityItem.getIPAddress(id.SocketAddress.SIN_Address) + ":" + id.SocketAddress.SIN_port;
                    stdout.WriteLine(Ok(a).O("identity", new Json().N("vendorId", id.VendorID1).N("deviceType", id.DeviceType1)
                        .N("productCode", id.ProductCode1).S("revision", id.Revision1[0] + "." + id.Revision1[1])
                        .N("status", id.Status1).N("serialNumber", id.SerialNumber1).S("productName", id.ProductName1)
                        .S("socketAddress", sock).N("state", id.State1)));
                    return 0;
                }
            }
        }

        // Io: a Class 1 connection with EEIP.NET's ForwardOpen/ForwardClose.
        // EEIP.NET sends O->T from its own thread every RPI and receives T->O
        // on UDP 2222 (OriginatorUDPPort); it keeps no counters, so the
        // adapter reads the sequence count EEIP.NET keeps for O->T ("sent")
        // and counts the updates of LastReceivedImplicitMessage ("received").
        static int Io(Args a, EEIPClient c, int timeout)
        {
            long outSize = a.Num("output-size", 0), inSize = a.Num("input-size", 0);
            long rpiMs = a.Num("rpi-ms", 10);
            bool large = a.Has("large"), multicast = a.Has("multicast");
            c.ConfigurationAssemblyInstanceID = (byte)a.Num("config", 0);
            c.O_T_InstanceID = (byte)a.Num("output", 0);
            c.T_O_InstanceID = (byte)a.Num("input", 0);
            c.O_T_Length = (ushort)outSize;
            c.T_O_Length = (ushort)inSize;
            // O->T with the 32-bit run/idle header; without output data a
            // heartbeat: the sequence count alone. EEIP.NET's own Heartbeat
            // format announces an O->T size of 0 (no sequence count), which
            // targets refuse (0x01/0x0127); its Modeless format with no data
            // announces 2 and sends the sequence count. T->O modeless.
            c.O_T_RealTimeFormat = outSize > 0 ? RealTimeFormat.Header32Bit : RealTimeFormat.Modeless;
            c.T_O_RealTimeFormat = RealTimeFormat.Modeless;
            c.O_T_ConnectionType = ConnectionType.Point_to_Point;
            c.T_O_ConnectionType = multicast ? ConnectionType.Multicast : ConnectionType.Point_to_Point;
            c.O_T_OwnerRedundant = false;
            c.T_O_OwnerRedundant = false;
            c.O_T_VariableLength = false;
            c.T_O_VariableLength = false;
            c.O_T_Priority = Priority.Scheduled;
            c.T_O_Priority = Priority.Scheduled;
            c.RequestedPacketRate_O_T = (uint)(rpiMs * 1000);
            c.RequestedPacketRate_T_O = (uint)(rpiMs * 1000);
            c.O_T_IOData = a.Has("output-data") ? FromHex(a.Get("output-data")) : new byte[outSize];

            try
            {
                Bounded(() => c.ForwardOpen(large), timeout, large ? "Large_Forward_Open" : "Forward_Open");
            }
            catch (CIPException e)
            {
                var (st, ext) = StatusOf(e);
                Console.Error.WriteLine("eeip: Forward_Open refused: " + e.Message);
                stdout.WriteLine(Ok(a).O("forwardOpen", new Json().N("status", st).A("extendedStatus", ext)).S("errorText", e.Message));
                Close(c, timeout);
                return 0;
            }

            // EEIP.NET parses the Forward_Open reply without checking that one
            // arrived: a target that closed the connection instead leaves a
            // zeroed buffer, read as success with connection IDs 0. Both IDs
            // 0 means there was no reply.
            uint IdOf(string f) => typeof(EEIPClient).GetField(f, BindingFlags.Instance | BindingFlags.NonPublic)?.GetValue(c) is uint v ? v : 1;
            if (IdOf("connectionID_O_T") == 0 && IdOf("connectionID_T_O") == 0)
            {
                Close(c, timeout);
                throw new OpFailure("connection-lost", "no Forward_Open reply: the target closed the connection");
            }

            var seqField = typeof(EEIPClient).GetField("sequence", BindingFlags.Instance | BindingFlags.NonPublic);
            int seq0 = seqField != null ? (int)seqField.GetValue(c) : 0;
            // EEIP.NET sends the timeout multiplier 3: the connection times
            // out after 32 RPIs without data.
            var connTimeout = TimeSpan.FromMilliseconds(32 * rpiMs);
            long received = 0;
            byte[] first = null, last = null;
            DateTime prev = c.LastReceivedImplicitMessage;
            var start = DateTime.Now;
            DateTime lastAt = start;
            bool timedOut = false;
            var sw = Stopwatch.StartNew();
            long duration = a.Num("duration-ms", 1000);
            while (sw.ElapsedMilliseconds < duration)
            {
                var at = c.LastReceivedImplicitMessage;
                var now = DateTime.Now;
                if (at != prev)
                {
                    prev = at;
                    received++;
                    var d = c.T_O_IOData;
                    if (first == null) first = d.Take((int)inSize).ToArray();
                    last = d.Take((int)inSize).ToArray();
                    lastAt = at;
                }
                if (now - lastAt > connTimeout) timedOut = true;
                Thread.Sleep(1);
            }
            int sent = seqField != null ? (int)seqField.GetValue(c) - seq0 : 0;

            var fc = new Json();
            try
            {
                Bounded(c.ForwardClose, timeout, "Forward_Close");
                fc.N("status", 0);
            }
            catch (CIPException e)
            {
                fc.N("status", StatusOf(e).status);
            }
            catch (Exception e)
            {
                fc.N("status", -1).S("error", e.Message);
            }
            stdout.WriteLine(Ok(a).O("forwardOpen", new Json().N("status", 0)).N("sent", Math.Max(sent, 0)).N("received", received)
                .S("firstInput", Hex(first)).S("lastInput", Hex(last)).B("timedOut", timedOut).O("forwardClose", fc));
            Close(c, timeout);
            return 0;
        }
    }
}
