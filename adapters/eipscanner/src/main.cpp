// SPDX-License-Identifier: MIT
//
// ethernetip-interop adapter for EIPScanner (client role only): runs one
// operation of the container contract (docs/CONTAINER_CONTRACT.md) with
// EIPScanner as the originator and prints one JSON result document.

#include <atomic>
#include <cctype>
#include <chrono>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <map>
#include <memory>
#include <mutex>
#include <set>
#include <sstream>
#include <stdexcept>
#include <string>
#include <thread>
#include <vector>

#include "ConnectionManager.h"
#include "DiscoveryManager.h"
#include "IdentityObject.h"
#include "MessageRouter.h"
#include "SessionInfo.h"
#include "cip/EPath.h"
#include "cip/MessageRouterRequest.h"
#include "cip/Services.h"
#include "cip/connectionManager/NetworkConnectionParams.h"
#include "utils/Buffer.h"
#include "utils/Logger.h"

using eipScanner::ConnectionManager;
using eipScanner::DiscoveryManager;
using eipScanner::IdentityObject;
using eipScanner::MessageRouter;
using eipScanner::SessionInfo;
using eipScanner::cip::EPath;
using eipScanner::cip::MessageRouterResponse;
using eipScanner::cip::connectionManager::ConnectionParameters;
using eipScanner::cip::connectionManager::NetworkConnectionParams;
using eipScanner::utils::Buffer;
using eipScanner::utils::LogAppenderIf;
using eipScanner::utils::Logger;
using eipScanner::utils::LogLevel;

namespace {

// Log lines go to stderr and are kept for the result document: EIPScanner
// reports a refused Forward_Open only in its log.
std::mutex logMu;
std::vector<std::string> logLines;

class StderrAppender : public LogAppenderIf {
public:
	void print(LogLevel, const std::string &msg) override {
		std::lock_guard<std::mutex> lk(logMu);
		std::cerr << msg << std::endl;
		if (logLines.size() < 200) logLines.push_back(msg);
	}
};

std::string jsonEscape(const std::string &s) {
	std::ostringstream o;
	for (unsigned char c : s) {
		switch (c) {
		case '"': o << "\\\""; break;
		case '\\': o << "\\\\"; break;
		case '\n': o << "\\n"; break;
		case '\r': o << "\\r"; break;
		case '\t': o << "\\t"; break;
		default:
			if (c < 0x20) {
				char b[8];
				std::snprintf(b, sizeof b, "\\u%04x", c);
				o << b;
			} else {
				o << c;
			}
		}
	}
	return o.str();
}

std::string str(const std::string &s) { return "\"" + jsonEscape(s) + "\""; }

std::string hexOf(const std::vector<uint8_t> &d) {
	static const char *h = "0123456789abcdef";
	std::string s;
	for (auto b : d) {
		s += h[b >> 4];
		s += h[b & 15];
	}
	return s;
}

std::vector<uint8_t> fromHex(const std::string &s) {
	if (s.size() % 2) throw std::invalid_argument("odd hex length");
	for (unsigned char c : s)
		if (!std::isxdigit(c)) throw std::invalid_argument("not hex");
	std::vector<uint8_t> out;
	for (size_t i = 0; i < s.size(); i += 2) out.push_back(static_cast<uint8_t>(std::stoul(s.substr(i, 2), nullptr, 16)));
	return out;
}

struct Args {
	std::string op;
	std::map<std::string, std::string> kv;
	std::vector<std::string> paths;
	bool has(const std::string &k) const { return kv.count(k) > 0; }
	std::string get(const std::string &k, const std::string &def = "") const {
		auto it = kv.find(k);
		return it == kv.end() ? def : it->second;
	}
	long num(const std::string &k, long def) const {
		auto it = kv.find(k);
		return it == kv.end() ? def : std::stol(it->second, nullptr, 0);
	}
};

const char *kOps[] = {"list-identity", "get-attribute", "set-attribute", "get-attributes-all", "multiple-get", "identity", "io"};

int usage(const std::string &msg) {
	std::cerr << "ethernetip-interop: " << msg << std::endl;
	return 2;
}

std::string head(const Args &a) {
	return "{\"schemaVersion\":\"1.0\",\"adapter\":\"eipscanner\",\"operation\":" + str(a.op);
}

std::string logsField() {
	std::lock_guard<std::mutex> lk(logMu);
	std::string s = ",\"log\":[";
	for (size_t i = 0; i < logLines.size(); i++) s += (i ? "," : "") + str(logLines[i]);
	return s + "]";
}

int fail(const Args &a, const std::string &code, const std::string &msg) {
	std::cout << head(a) << ",\"ok\":false,\"error\":{\"code\":" << str(code) << ",\"message\":" << str(msg) << "}" << logsField() << "}" << std::endl;
	return 1;
}

std::string statusFields(const MessageRouterResponse &r) {
	std::ostringstream o;
	o << ",\"status\":" << static_cast<int>(r.getGeneralStatusCode()) << ",\"extendedStatus\":[";
	auto ext = r.getAdditionalStatus();
	for (size_t i = 0; i < ext.size(); i++) o << (i ? "," : "") << ext[i];
	o << "],\"data\":" << str(hexOf(r.getData()));
	return o.str();
}

std::string identityJSON(const IdentityObject &id, const std::string &socket) {
	std::ostringstream o;
	o << "{\"vendorId\":" << id.getVendorId() << ",\"deviceType\":" << id.getDeviceType()
	  << ",\"productCode\":" << id.getProductCode() << ",\"revision\":\""
	  << static_cast<int>(id.getRevision().getMajorRevision()) << "." << static_cast<int>(id.getRevision().getMinorRevision())
	  << "\",\"status\":" << id.getStatus() << ",\"serialNumber\":" << id.getSerialNumber()
	  << ",\"productName\":" << str(id.getProductName()) << ",\"socketAddress\":" << str(socket) << "}";
	return o.str();
}

// path parses "@class/instance/attribute".
EPath path(const std::string &p) {
	if (p.empty() || p[0] != '@') throw std::invalid_argument("path " + p);
	std::vector<unsigned long> v;
	std::stringstream ss(p.substr(1));
	std::string part;
	while (std::getline(ss, part, '/')) v.push_back(std::stoul(part, nullptr, 0));
	if (v.size() == 3) return EPath(v[0], v[1], v[2]);
	if (v.size() == 2) return EPath(v[0], v[1]);
	throw std::invalid_argument("path " + p);
}

int run(const Args &a) {
	const std::string host = a.get("host");
	const int port = static_cast<int>(a.num("port", 44818));
	const auto timeout = std::chrono::milliseconds(a.num("timeout-ms", 5000));
	if (a.op == "list-identity") {
		// EIPScanner discovers over UDP; a unicast address addresses one device.
		DiscoveryManager dm(host, port, timeout);
		auto items = dm.discover();
		if (items.empty()) return fail(a, "timeout", "no ListIdentity reply");
		std::cout << head(a) << ",\"ok\":true,\"error\":null,\"identity\":"
		          << identityJSON(items[0].identityObject, items[0].socketAddress.toString()) << "}" << std::endl;
		return 0;
	}
	std::shared_ptr<SessionInfo> si;
	try {
		si = std::make_shared<SessionInfo>(host, port, timeout);
	} catch (const std::exception &e) {
		return fail(a, "connect-failed", e.what());
	}
	MessageRouter mr;
	using eipScanner::cip::ServiceCodes;
	if (a.op == "identity") {
		IdentityObject id(1, si);
		std::cout << head(a) << ",\"ok\":true,\"error\":null,\"identity\":" << identityJSON(id, host) << "}" << std::endl;
		return 0;
	}
	if (a.op == "get-attribute" || a.op == "set-attribute" || a.op == "get-attributes-all") {
		EPath p = a.op == "get-attributes-all"
		              ? EPath(a.num("class", 0), a.num("instance", 0))
		              : EPath(a.num("class", 0), a.num("instance", 0), a.num("attribute", 0));
		std::vector<uint8_t> data;
		eipScanner::cip::CipUsint svc = ServiceCodes::GET_ATTRIBUTE_SINGLE;
		if (a.op == "set-attribute") {
			svc = ServiceCodes::SET_ATTRIBUTE_SINGLE;
			data = fromHex(a.get("data"));
		} else if (a.op == "get-attributes-all") {
			svc = ServiceCodes::GET_ATTRIBUTE_ALL;
		}
		auto r = mr.sendRequest(si, svc, p, data);
		std::cout << head(a) << ",\"ok\":true,\"error\":null" << statusFields(r) << "}" << std::endl;
		return 0;
	}
	if (a.op == "multiple-get") {
		// EIPScanner has no helper for Multiple Service Packet: the count, the
		// offsets and the embedded requests are packed here.
		std::vector<std::vector<uint8_t>> reqs;
		for (auto &p : a.paths)
			reqs.push_back(eipScanner::cip::MessageRouterRequest(ServiceCodes::GET_ATTRIBUTE_SINGLE, path(p), {}).pack());
		Buffer buf;
		buf << static_cast<eipScanner::cip::CipUint>(reqs.size());
		eipScanner::cip::CipUint off = static_cast<eipScanner::cip::CipUint>(2 + 2 * reqs.size());
		for (auto &r : reqs) {
			buf << off;
			off = static_cast<eipScanner::cip::CipUint>(off + r.size());
		}
		for (auto &r : reqs) buf << r;
		auto resp = mr.sendRequest(si, ServiceCodes::MULTIPLE_SERVICE_PACKET, EPath(2, 1), buf.data());
		std::ostringstream o;
		o << head(a) << ",\"ok\":true,\"error\":null,\"status\":" << static_cast<int>(resp.getGeneralStatusCode()) << ",\"replies\":[";
		const auto &d = resp.getData();
		if (d.size() >= 2) {
			size_t n = d[0] | (d[1] << 8);
			std::vector<size_t> offs;
			for (size_t i = 0; i < n && 3 + 2 * i < d.size(); i++) offs.push_back(d[2 + 2 * i] | (d[3 + 2 * i] << 8));
			offs.push_back(d.size());
			for (size_t i = 0; i + 1 < offs.size(); i++) {
				size_t s = offs[i], e = offs[i + 1];
				if (s + 4 > e || e > d.size()) break;
				size_t ext = d[s + 3];
				std::vector<uint8_t> body(d.begin() + std::min(e, s + 4 + 2 * ext), d.begin() + e);
				o << (i ? "," : "") << "{\"status\":" << static_cast<int>(d[s + 2]) << ",\"data\":" << str(hexOf(body)) << "}";
			}
		}
		o << "]}";
		std::cout << o.str() << std::endl;
		return 0;
	}
	if (a.op == "io") {
		ConnectionManager cm;
		ConnectionParameters p;
		std::vector<uint8_t> cp = {0x20, 0x04};
		auto push = [&](uint8_t seg, long v) {
			if (v > 0xFF) {
				cp.push_back(seg | 1);
				cp.push_back(0);
				cp.push_back(v & 0xFF);
				cp.push_back((v >> 8) & 0xFF);
			} else {
				cp.push_back(seg);
				cp.push_back(static_cast<uint8_t>(v));
			}
		};
		if (a.num("config", 0) > 0) push(0x24, a.num("config", 0));
		push(0x2C, a.num("output", 0));
		push(0x2C, a.num("input", 0));
		if (a.has("config-data")) {
			// Configuration data: a simple data segment (0x80, size in
			// words) at the end of the connection path.
			const std::vector<uint8_t> cd = fromHex(a.get("config-data"));
			if (cd.size() % 2 || cd.size() > 510) return usage("--config-data: an even number of octets, at most 510");
			cp.push_back(0x80);
			cp.push_back(static_cast<uint8_t>(cd.size() / 2));
			cp.insert(cp.end(), cd.begin(), cd.end());
		}
		p.connectionPath = cp;
		const long outSize = a.num("output-size", 0), inSize = a.num("input-size", 0);
		p.o2tRealTimeFormat = outSize > 0; // 32-bit run/idle header O->T, none for heartbeats
		p.originatorVendorId = 0x0102;
		p.originatorSerialNumber = 0x0BADCAFE;
		p.connectionTimeoutMultiplier = 1;
		const bool large = a.has("large");
		p.t2oNetworkConnectionParams = NetworkConnectionParams::P2P | NetworkConnectionParams::SCHEDULED_PRIORITY;
		p.o2tNetworkConnectionParams = NetworkConnectionParams::P2P | NetworkConnectionParams::SCHEDULED_PRIORITY;
		if (large) {
			p.t2oNetworkConnectionParams = (2u << 29) | (2u << 26);
			p.o2tNetworkConnectionParams = (2u << 29) | (2u << 26);
		}
		p.t2oNetworkConnectionParams |= static_cast<eipScanner::cip::CipUdint>(inSize);
		p.o2tNetworkConnectionParams |= static_cast<eipScanner::cip::CipUdint>(outSize);
		const auto rpi = static_cast<eipScanner::cip::CipUdint>(a.num("rpi-ms", 10) * 1000);
		p.o2tRPI = rpi;
		p.t2oRPI = rpi;
		p.transportTypeTrigger = NetworkConnectionParams::CLASS1;
		p.priorityTimeTick = 0x0A;
		p.timeoutTicks = 0x05;
		auto wio = cm.forwardOpen(si, p, large);
		auto io = wio.lock();
		if (!io) {
			std::cout << head(a) << ",\"ok\":true,\"error\":null,\"forwardOpen\":{\"status\":-1,\"refused\":true}" << logsField() << "}" << std::endl;
			return 0;
		}
		std::vector<uint8_t> out = a.has("output-data") ? fromHex(a.get("output-data")) : std::vector<uint8_t>(outSize, 0);
		io->setDataToSend(out);
		std::atomic<long> received{0};
		std::mutex mu;
		std::vector<uint8_t> first, last;
		bool closed = false;
		io->setReceiveDataListener([&](eipScanner::cip::CipUdint, eipScanner::cip::CipUint, const std::vector<uint8_t> &d) {
			std::lock_guard<std::mutex> lk(mu);
			if (received++ == 0) first = d;
			last = d;
		});
		io->setCloseListener([&]() { closed = true; });
		long sent = 0;
		io->setSendDataListener([&](std::vector<uint8_t> &) { sent++; });
		const auto end = std::chrono::steady_clock::now() + std::chrono::milliseconds(a.num("duration-ms", 1000));
		while (std::chrono::steady_clock::now() < end && cm.hasOpenConnections()) cm.handleConnections(std::chrono::milliseconds(2));
		bool timedOut = closed;
		if (cm.hasOpenConnections()) cm.forwardClose(si, wio);
		std::lock_guard<std::mutex> lk(mu);
		std::cout << head(a) << ",\"ok\":true,\"error\":null,\"forwardOpen\":{\"status\":0},\"sent\":" << sent
		          << ",\"received\":" << received.load() << ",\"firstInput\":" << str(hexOf(first))
		          << ",\"lastInput\":" << str(hexOf(last)) << ",\"timedOut\":" << (timedOut ? "true" : "false")
		          << ",\"forwardClose\":{\"status\":0}}" << std::endl;
		return 0;
	}
	return usage("unknown operation " + a.op);
}

const char *envOr(const char *k, const char *def) {
	const char *v = std::getenv(k);
	return v ? v : def;
}

int capabilities() {
	std::cout << "{\"schemaVersion\":\"1.0\",\"adapter\":\"eipscanner\",\"adapterVersion\":"
	          << str(envOr("ETHERNETIP_INTEROP_ADAPTER_VERSION", "dev"))
	          << ",\"protocol\":\"ethernetip\",\"upstream\":{\"name\":\"EIPScanner\",\"version\":"
	          << str(envOr("ETHERNETIP_INTEROP_UPSTREAM_VERSION", "")) << ",\"revision\":"
	          << str(envOr("ETHERNETIP_INTEROP_UPSTREAM_REVISION", ""))
	          << ",\"license\":\"MIT\",\"language\":\"C++\",\"independentEngine\":true},"
	             "\"roles\":{\"server\":false,\"client\":true},\"fixtures\":[],\"clientOperations\":[";
	for (size_t i = 0; i < sizeof kOps / sizeof kOps[0]; i++) std::cout << (i ? "," : "") << str(kOps[i]);
	std::cout << "],\"clientFeatures\":{\"listIdentityTcp\":false,\"listIdentityUdp\":true,\"listServices\":false,"
	             "\"getAttributeSingle\":true,\"setAttributeSingle\":true,\"getAttributesAll\":true,"
	             "\"multipleService\":true,\"unconnectedSend\":false,\"class3\":false,\"class1\":true,"
	             "\"largeForwardOpen\":true,\"multicast\":false,\"inputOnly\":true,\"listenOnly\":false,"
	             "\"logixTags\":false,\"readTagFragmented\":false,\"tagList\":false,\"configData\":true},"
	             "\"serverFeatures\":{}}"
	          << std::endl;
	return 0;
}

// printFixture writes /fixtures/<name>/fixture.json; a name with a path
// separator or ".." is a usage error, as is a name without a fixture.
int printFixture(const std::string &name) {
	if (name.empty() || name.find('/') != std::string::npos || name.find('\\') != std::string::npos ||
	    name.find("..") != std::string::npos)
		return usage("invalid fixture name " + name);
	std::ifstream in("/fixtures/" + name + "/fixture.json", std::ios::binary);
	if (!in) return usage("no fixture " + name);
	std::cout << in.rdbuf();
	return 0;
}

// Flags each operation takes; anything else is a usage error. Switches take
// no value.
const std::set<std::string> kValueFlags = {"host", "port", "timeout-ms", "transport", "class", "instance", "attribute",
                                           "data", "path", "config", "output", "input", "output-size", "input-size",
                                           "rpi-ms", "duration-ms", "output-data", "config-data"};
const std::set<std::string> kNumericFlags = {"port", "timeout-ms", "class", "instance", "attribute", "config", "output",
                                             "input", "output-size", "input-size", "rpi-ms", "duration-ms"};

} // namespace

int main(int argc, char **argv) {
	Logger::setAppender(std::make_unique<StderrAppender>());
	Logger::setLogLevel(LogLevel::INFO);
	std::vector<std::string> args(argv + 1, argv + argc);
	if (args.empty()) return usage("usage: ethernetip-interop client <op> --host HOST ... | print-capabilities | print-fixture [NAME] | version");
	if (args[0] == "print-capabilities") return capabilities();
	if (args[0] == "version") {
		std::cout << "{\"adapter\":\"eipscanner\",\"adapterVersion\":" << str(envOr("ETHERNETIP_INTEROP_ADAPTER_VERSION", "dev"))
		          << ",\"upstream\":\"EIPScanner\",\"upstreamVersion\":" << str(envOr("ETHERNETIP_INTEROP_UPSTREAM_VERSION", ""))
		          << ",\"upstreamRevision\":" << str(envOr("ETHERNETIP_INTEROP_UPSTREAM_REVISION", ""))
		          << ",\"contract\":\"1.0\"}" << std::endl;
		return 0;
	}
	if (args[0] == "print-fixture") return printFixture(args.size() > 1 ? args[1] : "adapter");
	if (args[0] == "server") return usage("the eipscanner adapter has no server role");
	if (args[0] != "client" || args.size() < 2) return usage("unknown command");
	Args a;
	a.op = args[1];
	bool known = false;
	for (auto o : kOps) known = known || a.op == o;
	if (!known) return usage("unknown operation " + a.op);
	for (size_t i = 2; i < args.size(); i++) {
		if (args[i].rfind("--", 0) != 0) return usage("unexpected argument " + args[i]);
		std::string k = args[i].substr(2);
		if (k == "large") {
			a.kv[k] = "1";
			continue;
		}
		if (k == "multicast" || k == "connected")
			return usage("--" + k + " needs a feature EIPScanner does not have");
		if (!kValueFlags.count(k)) return usage("unknown flag --" + k);
		if (i + 1 >= args.size()) return usage("flag --" + k + " needs a value");
		const std::string v = args[++i];
		if (kNumericFlags.count(k)) {
			try {
				size_t end = 0;
				std::stol(v, &end, 0);
				if (end != v.size()) throw std::invalid_argument(v);
			} catch (const std::exception &) {
				return usage("--" + k + ": not a number: " + v);
			}
		}
		if (k == "data" || k == "output-data" || k == "config-data") {
			try {
				fromHex(v);
			} catch (const std::exception &) {
				return usage("--" + k + ": not hex: " + v);
			}
		}
		if (k == "path") {
			a.paths.push_back(v);
			continue;
		}
		a.kv[k] = v;
	}
	// EIPScanner sends ListIdentity over UDP only: that is the default here.
	if (a.has("transport") && a.get("transport") != "udp" && a.get("transport") != "tcp")
		return usage("--transport must be tcp or udp");
	if (a.op == "list-identity" && a.get("transport", "udp") != "udp")
		return usage("EIPScanner sends ListIdentity over UDP only");
	if (a.has("output-data") && a.get("output-data").size() / 2 != static_cast<size_t>(a.num("output-size", 0)))
		return usage("--output-data must be --output-size octets of hex");
	if (!a.has("host")) return usage("--host is required");
	try {
		return run(a);
	} catch (const std::exception &e) {
		return fail(a, "failed", e.what());
	}
}
