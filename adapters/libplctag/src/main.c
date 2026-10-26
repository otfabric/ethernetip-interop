/* SPDX-License-Identifier: MIT */
/*
 * ethernetip-interop adapter for libplctag (docs/CONTAINER_CONTRACT.md).
 *
 * client: one operation as an originator through libplctag's public API
 *         (plc_tag_create/read/write): Logix tags (protocol=ab-eip,
 *         plc=ControlLogix, path=1,<slot>), the tag list (@tags, @udt/<id>),
 *         generic CIP services through the raw tag (@raw) and the Identity
 *         object through the identity tag (@identity, plc=generic).
 * server: libplctag's PLC simulator ab_server (src/tools/ab_server), started
 *         with the fixture's tags; the adapter writes the fixture's initial
 *         values through libplctag before it signals readiness.
 */
#define _GNU_SOURCE
#include <arpa/inet.h>
#include <ctype.h>
#include <errno.h>
#include <fcntl.h>
#include <ifaddrs.h>
#include <math.h>
#include <netinet/in.h>
#include <signal.h>
#include <stdarg.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

#include "json.h"
#include "libplctag.h"

#define ADAPTER "libplctag"
#define DEFAULT_FIXTURE "logix"
#define READY_FILE "/run/ethernetip-interop/ready"
#define AB_SERVER "/usr/local/bin/ab_server"
#define CAPABILITIES_FILE "/opt/adapter/capabilities.json"

/* ------------------------------------------------------------------------ */
/* Output                                                                   */

typedef struct {
	char *s;
	size_t len, cap;
} buf;

static void bput(buf *b, const char *s, size_t n) {
	if (b->len + n + 1 > b->cap) {
		size_t cap = b->cap ? b->cap : 256;
		while (b->len + n + 1 > cap) cap *= 2;
		char *p = realloc(b->s, cap);
		if (!p) {
			fputs("out of memory\n", stderr);
			exit(1);
		}
		b->s = p;
		b->cap = cap;
	}
	memcpy(b->s + b->len, s, n);
	b->len += n;
	b->s[b->len] = 0;
}

#define BLIT(b, lit) bput((b), (lit), sizeof(lit) - 1)

static void bprintf(buf *b, const char *fmt, ...) {
	char tmp[512];
	va_list ap;
	va_start(ap, fmt);
	int n = vsnprintf(tmp, sizeof tmp, fmt, ap);
	va_end(ap);
	if (n < 0) return;
	if ((size_t)n < sizeof tmp) {
		bput(b, tmp, (size_t)n);
		return;
	}
	char *big = malloc((size_t)n + 1);
	if (!big) return;
	va_start(ap, fmt);
	vsnprintf(big, (size_t)n + 1, fmt, ap);
	va_end(ap);
	bput(b, big, (size_t)n);
	free(big);
}

/* bstr appends a JSON string literal; NULL gives null. */
static void bstr(buf *b, const char *s) {
	if (!s) {
		BLIT(b, "null");
		return;
	}
	BLIT(b, "\"");
	for (const unsigned char *p = (const unsigned char *)s; *p; p++) {
		switch (*p) {
		case '"': BLIT(b, "\\\""); break;
		case '\\': BLIT(b, "\\\\"); break;
		case '\n': BLIT(b, "\\n"); break;
		case '\r': BLIT(b, "\\r"); break;
		case '\t': BLIT(b, "\\t"); break;
		default:
			if (*p < 0x20 || *p >= 0x7f) bprintf(b, "\\u%04x", *p);
			else bput(b, (const char *)p, 1);
		}
	}
	BLIT(b, "\"");
}

static void bhex(buf *b, const uint8_t *d, size_t n) {
	static const char h[] = "0123456789abcdef";
	BLIT(b, "\"");
	for (size_t i = 0; i < n; i++) {
		char c[2] = {h[d[i] >> 4], h[d[i] & 15]};
		bput(b, c, 2);
	}
	BLIT(b, "\"");
}

/* bdouble appends a number the way JSON readers give it back (shortest round trip). */
static void bdouble(buf *b, double v, bool single) {
	if (!isfinite(v)) {
		BLIT(b, "null");
		return;
	}
	char tmp[64];
	for (int prec = single ? 6 : 15; prec <= 17; prec++) {
		snprintf(tmp, sizeof tmp, "%.*g", prec, v);
		if (single ? (float)strtod(tmp, NULL) == (float)v : strtod(tmp, NULL) == v) break;
	}
	bput(b, tmp, strlen(tmp));
}

/* ------------------------------------------------------------------------ */
/* Arguments                                                                */

static int usage(const char *fmt, ...) {
	va_list ap;
	fputs("ethernetip-interop: ", stderr);
	va_start(ap, fmt);
	vfprintf(stderr, fmt, ap);
	va_end(ap);
	fputs("\n", stderr);
	return 2;
}

typedef struct {
	const char *name;
	const char *value; /* NULL when absent; "" for a present switch */
} flag;

typedef struct {
	flag f[24];
	int n;
} flags;

static const char *fget(const flags *fl, const char *name) {
	for (int i = 0; i < fl->n; i++)
		if (strcmp(fl->f[i].name, name) == 0) return fl->f[i].value;
	return NULL;
}

static bool is_switch(const char *name) { return strcmp(name, "connected") == 0; }

/* parse_flags reads "--name value" pairs and bare switches; allowed is a
 * space-separated list of the names the command takes. */
static int parse_flags(int argc, char **argv, const char *allowed, flags *out) {
	out->n = 0;
	for (int i = 0; i < argc; i++) {
		const char *a = argv[i];
		if (strncmp(a, "--", 2) != 0 || !a[2]) return usage("unexpected argument %s", a);
		const char *name = a + 2;
		char pat[64];
		snprintf(pat, sizeof pat, " %s ", name);
		char all[512];
		snprintf(all, sizeof all, " %s ", allowed);
		if (!strstr(all, pat)) return usage("unknown flag --%s", name);
		if (fget(out, name)) return usage("flag --%s given twice", name);
		if (out->n >= (int)(sizeof out->f / sizeof out->f[0])) return usage("too many flags");
		if (is_switch(name)) {
			out->f[out->n++] = (flag){name, ""};
			continue;
		}
		if (i + 1 >= argc) return usage("flag --%s needs a value", name);
		out->f[out->n++] = (flag){name, argv[++i]};
	}
	return 0;
}

/* parse_num parses a decimal or 0x hexadecimal number in [lo, hi]. */
static bool parse_num(const char *s, long long lo, long long hi, long long *out) {
	if (!s || !*s) return false;
	char *end = NULL;
	errno = 0;
	long long v;
	if (s[0] == '0' && (s[1] == 'x' || s[1] == 'X')) {
		if (!isxdigit((unsigned char)s[2])) return false;
		v = (long long)strtoull(s + 2, &end, 16);
	} else {
		v = strtoll(s, &end, 10);
	}
	if (errno || *end || v < lo || v > hi) return false;
	*out = v;
	return true;
}

static int hexval(char c) {
	if (c >= '0' && c <= '9') return c - '0';
	if (c >= 'a' && c <= 'f') return c - 'a' + 10;
	if (c >= 'A' && c <= 'F') return c - 'A' + 10;
	return -1;
}

static bool parse_hex(const char *s, uint8_t **out, size_t *n) {
	size_t len = strlen(s);
	if (len % 2) return false;
	uint8_t *d = malloc(len / 2 + 1);
	if (!d) return false;
	for (size_t i = 0; i < len; i += 2) {
		int hi = hexval(s[i]), lo = hexval(s[i + 1]);
		if (hi < 0 || lo < 0) {
			free(d);
			return false;
		}
		d[i / 2] = (uint8_t)(hi << 4 | lo);
	}
	*out = d;
	*n = len / 2;
	return true;
}

/* ------------------------------------------------------------------------ */
/* CIP types                                                                */

typedef struct {
	const char *name;
	uint8_t code;
	int size;
} cip_type;

static const cip_type TYPES[] = {
    {"BOOL", 0xC1, 1},  {"SINT", 0xC2, 1},  {"INT", 0xC3, 2},   {"DINT", 0xC4, 4},
    {"LINT", 0xC5, 8},  {"USINT", 0xC6, 1}, {"UINT", 0xC7, 2},  {"UDINT", 0xC8, 4},
    {"ULINT", 0xC9, 8}, {"REAL", 0xCA, 4},  {"LREAL", 0xCB, 8}, {"DWORD", 0xD3, 4},
};

static const cip_type *type_by_name(const char *name) {
	for (size_t i = 0; i < sizeof TYPES / sizeof TYPES[0]; i++)
		if (strcmp(TYPES[i].name, name) == 0 && TYPES[i].code != 0xD3) return &TYPES[i];
	return NULL;
}

static const cip_type *type_by_code(uint8_t code) {
	for (size_t i = 0; i < sizeof TYPES / sizeof TYPES[0]; i++)
		if (TYPES[i].code == code) return &TYPES[i];
	return NULL;
}

/* The Logix STRING structure: its handle on the wire (A0 02 CE 0F). */
#define LOGIX_STRING_HANDLE 0x0FCE
#define LOGIX_STRING_SIZE 88

/* ------------------------------------------------------------------------ */
/* libplctag                                                                */

typedef struct {
	const char *host;
	int port;
	int timeout;
	int slot;
} target;

static char *attrs(const target *t, const char *plc, const char *extra) {
	char *s = NULL;
	if (asprintf(&s, "protocol=ab-eip&gateway=%s:%d&%s%s%s", t->host, t->port, plc, *extra ? "&" : "", extra) < 0) {
		fputs("out of memory\n", stderr);
		exit(1);
	}
	return s;
}

static char *logix_attrs(const target *t, const char *extra) {
	char plc[64];
	snprintf(plc, sizeof plc, "plc=ControlLogix&path=1,%d", t->slot);
	return attrs(t, plc, extra);
}

/* A libplctag status is a transport failure (no usable answer) or an
 * answer of the target that libplctag turned into an error code. */
static const char *transport_error(int rc) {
	switch (rc) {
	case PLCTAG_ERR_TIMEOUT: return "timeout";
	case PLCTAG_ERR_BAD_GATEWAY:
	case PLCTAG_ERR_OPEN:
	case PLCTAG_ERR_WINSOCK: return "connect-failed";
	case PLCTAG_ERR_BAD_CONNECTION:
	case PLCTAG_ERR_CLOSE:
	case PLCTAG_ERR_READ:
	case PLCTAG_ERR_WRITE:
	case PLCTAG_ERR_ABORT:
	case PLCTAG_ERR_BAD_REPLY:
	case PLCTAG_ERR_NO_MEM:
	case PLCTAG_ERR_NO_RESOURCES:
	case PLCTAG_ERR_CREATE:
	case PLCTAG_ERR_BAD_CONFIG:
	case PLCTAG_ERR_THREAD_CREATE: return "failed";
	default: return NULL;
	}
}

/* ------------------------------------------------------------------------ */
/* Client                                                                   */

typedef struct {
	const char *op;
	buf doc;
} result;

static void head(result *r) {
	r->doc.len = 0;
	bprintf(&r->doc, "{\"schemaVersion\":\"1.0\",\"adapter\":\"%s\",\"operation\":", ADAPTER);
	bstr(&r->doc, r->op);
}

static int emit(result *r, int code) {
	BLIT(&r->doc, "}");
	printf("%s\n", r->doc.s);
	fflush(stdout);
	return code;
}

static int fail(result *r, const char *code, const char *fmt, ...) {
	char msg[512];
	va_list ap;
	va_start(ap, fmt);
	vsnprintf(msg, sizeof msg, fmt, ap);
	va_end(ap);
	head(r);
	BLIT(&r->doc, ",\"ok\":false,\"error\":{\"code\":");
	bstr(&r->doc, code);
	BLIT(&r->doc, ",\"message\":");
	bstr(&r->doc, msg);
	BLIT(&r->doc, "}");
	return emit(r, 1);
}

static void ok_head(result *r) {
	head(r);
	BLIT(&r->doc, ",\"ok\":true,\"error\":null");
}

/* fail_rc reports a libplctag status that left no usable answer. */
static int fail_rc(result *r, int rc, const char *what) {
	const char *code = transport_error(rc);
	return fail(r, code ? code : "failed", "%s: %s", what, plc_tag_decode_error(rc));
}

/* create makes a tag and waits for it (for a Logix tag: libplctag's first
 * read, which learns the type). */
static int32_t create(const char *a, int timeout) { return plc_tag_create(a, timeout); }

/* --- generic CIP through the raw tag --- */

static void epath(buf *p, long long cls, long long inst, long long attr, bool with_attr) {
	uint8_t seg[12];
	size_t n = 0;
	if (cls <= 0xFF) {
		seg[n++] = 0x20;
		seg[n++] = (uint8_t)cls;
	} else {
		seg[n++] = 0x21;
		seg[n++] = 0;
		seg[n++] = (uint8_t)cls;
		seg[n++] = (uint8_t)(cls >> 8);
	}
	if (inst <= 0xFF) {
		seg[n++] = 0x24;
		seg[n++] = (uint8_t)inst;
	} else {
		seg[n++] = 0x25;
		seg[n++] = 0;
		seg[n++] = (uint8_t)inst;
		seg[n++] = (uint8_t)(inst >> 8);
	}
	if (with_attr) {
		if (attr <= 0xFF) {
			seg[n++] = 0x30;
			seg[n++] = (uint8_t)attr;
		} else {
			seg[n++] = 0x31;
			seg[n++] = 0;
			seg[n++] = (uint8_t)attr;
			seg[n++] = (uint8_t)(attr >> 8);
		}
	}
	uint8_t words = (uint8_t)(n / 2);
	bput(p, (const char *)&words, 1);
	bput(p, (const char *)seg, n);
}

static int op_generic(result *r, const target *t, const flags *fl) {
	long long cls = 0, inst = 0, attr = 0;
	const char *v;
	if ((v = fget(fl, "class")) && !parse_num(v, 0, 0xFFFF, &cls)) return usage("--class: not a number in 0..65535");
	if ((v = fget(fl, "instance")) && !parse_num(v, 0, 0xFFFF, &inst)) return usage("--instance: not a number in 0..65535");
	if ((v = fget(fl, "attribute")) && !parse_num(v, 0, 0xFFFF, &attr)) return usage("--attribute: not a number in 0..65535");
	uint8_t *data = NULL;
	size_t data_len = 0;
	if (strcmp(r->op, "set-attribute") == 0) {
		if (!(v = fget(fl, "data"))) return usage("set-attribute needs --data");
		if (!parse_hex(v, &data, &data_len)) return usage("--data: not hexadecimal octets");
	}
	bool connected = fget(fl, "connected") != NULL;

	buf req = {0};
	uint8_t service = strcmp(r->op, "get-attribute") == 0 ? 0x0E : strcmp(r->op, "set-attribute") == 0 ? 0x10 : 0x01;
	bput(&req, (const char *)&service, 1);
	epath(&req, cls, inst, attr, service != 0x01);
	if (data_len) bput(&req, (const char *)data, data_len);
	free(data);

	/*
	 * Unconnected: libplctag's raw tag of a ControlLogix (path 1,<slot>)
	 * wraps the request in Unconnected_Send (0x52 to @6/1) and sends it with
	 * SendRRData. Connected: libplctag's Micro800 mode, which has no route:
	 * Forward_Open (0x54) with the connection path 20 02 24 01 to the
	 * target itself, then SendUnitData.
	 */
	char *a = connected ? attrs(t, "plc=micro800", "name=@raw") : logix_attrs(t, "name=@raw&use_connected_msg=0");
	int32_t tag = create(a, t->timeout);
	free(a);
	if (tag < 0) return fail_rc(r, tag, "plc_tag_create @raw");
	int rc = plc_tag_set_size(tag, (int)req.len);
	if (rc >= 0) rc = plc_tag_set_raw_bytes(tag, 0, (uint8_t *)req.s, (int)req.len);
	if (rc != PLCTAG_STATUS_OK) {
		plc_tag_destroy(tag);
		return fail_rc(r, rc, "setting up the raw request");
	}
	/* The raw tag sends its buffer with plc_tag_write and holds the reply afterwards. */
	rc = plc_tag_write(tag, t->timeout);
	if (rc != PLCTAG_STATUS_OK) {
		plc_tag_destroy(tag);
		return fail_rc(r, rc, "plc_tag_write @raw");
	}
	int size = plc_tag_get_size(tag);
	uint8_t *resp = malloc(size > 0 ? (size_t)size : 1);
	if (size < 4 || !resp || plc_tag_get_raw_bytes(tag, 0, resp, size) != PLCTAG_STATUS_OK) {
		plc_tag_destroy(tag);
		free(resp);
		return fail(r, "protocol", "raw reply of %d octets, too short for a CIP reply", size);
	}
	plc_tag_destroy(tag);
	/* reply service, reserved, general status, additional status size (words), additional status, data */
	int ext_words = resp[3];
	if (4 + 2 * ext_words > size) {
		free(resp);
		return fail(r, "protocol", "raw reply: additional status longer than the reply");
	}
	ok_head(r);
	bprintf(&r->doc, ",\"status\":%d,\"extendedStatus\":[", resp[2]);
	for (int i = 0; i < ext_words; i++) bprintf(&r->doc, "%s%u", i ? "," : "", resp[4 + 2 * i] | resp[5 + 2 * i] << 8);
	BLIT(&r->doc, "],\"data\":");
	bhex(&r->doc, resp + 4 + 2 * ext_words, (size_t)(size - 4 - 2 * ext_words));
	free(resp);
	return emit(r, 0);
}

/* --- the Identity object through the identity tag --- */

static uint16_t u16(const uint8_t *p) { return (uint16_t)(p[0] | p[1] << 8); }
static uint32_t u32(const uint8_t *p) { return (uint32_t)p[0] | (uint32_t)p[1] << 8 | (uint32_t)p[2] << 16 | (uint32_t)p[3] << 24; }

static int op_identity(result *r, const target *t) {
	char *a = attrs(t, "plc=generic", "name=@identity");
	int32_t tag = create(a, t->timeout);
	free(a);
	if (tag < 0) return fail_rc(r, tag, "plc_tag_create @identity");
	int rc = plc_tag_read(tag, t->timeout);
	if (rc != PLCTAG_STATUS_OK) {
		plc_tag_destroy(tag);
		return fail_rc(r, rc, "plc_tag_read @identity");
	}
	int size = plc_tag_get_size(tag);
	uint8_t d[512] = {0};
	if (size > (int)sizeof d) size = sizeof d;
	if (size < 15 || plc_tag_get_raw_bytes(tag, 0, d, size) != PLCTAG_STATUS_OK) {
		plc_tag_destroy(tag);
		return fail(r, "protocol", "identity reply of %d octets is too short", size);
	}
	plc_tag_destroy(tag);
	/* Get_Attributes_All of the Identity object: vendor, device type, product
	 * code, revision, status, serial number, product name (SHORT_STRING). */
	int name_len = d[14];
	if (15 + name_len > size) return fail(r, "protocol", "identity reply: product name longer than the reply");
	char name[256];
	memcpy(name, d + 15, (size_t)name_len);
	name[name_len] = 0;
	char sock[300];
	snprintf(sock, sizeof sock, "%s:%d", t->host, t->port);
	ok_head(r);
	bprintf(&r->doc, ",\"identity\":{\"vendorId\":%u,\"deviceType\":%u,\"productCode\":%u,\"revision\":\"%u.%u\",\"status\":%u,"
	                 "\"serialNumber\":%u,\"productName\":",
	        u16(d), u16(d + 2), u16(d + 4), d[6], d[7], u16(d + 8), u32(d + 10));
	bstr(&r->doc, name);
	BLIT(&r->doc, ",\"socketAddress\":null}");
	return emit(r, 0);
}

/* --- Logix tags --- */

/* tag_type reads the encoded type of a tag libplctag has read: the CIP type
 * code (0 for a structure) and the structure handle. */
static void tag_type(int32_t tag, uint8_t *code, uint16_t *handle) {
	uint8_t tb[8] = {0};
	int n = plc_tag_get_byte_array_attribute(tag, "raw_tag_type_bytes", tb, sizeof tb);
	*code = 0;
	*handle = 0;
	if (n >= 4 && tb[0] == 0xA0) {
		*handle = u16(tb + 2);
	} else if (n >= 1) {
		*code = tb[0];
	}
}

/* is_bit_name: a tag name ending in .<digits> addresses a bit of an integer. */
static bool is_bit_name(const char *name) {
	const char *dot = strrchr(name, '.');
	if (!dot || !dot[1]) return false;
	for (const char *p = dot + 1; *p; p++)
		if (!isdigit((unsigned char)*p)) return false;
	return true;
}

/* values appends the values of a read tag; returns its type name. */
static const char *values(buf *b, int32_t tag, const char *name, int count) {
	BLIT(b, "[");
	if (is_bit_name(name)) {
		int v = plc_tag_get_bit(tag, 0);
		bprintf(b, "%d]", v > 0 ? 1 : 0);
		return "BOOL";
	}
	uint8_t code;
	uint16_t handle;
	tag_type(tag, &code, &handle);
	int size = plc_tag_get_size(tag);
	const cip_type *ct = code ? type_by_code(code) : NULL;
	const char *tname = ct ? ct->name : handle == LOGIX_STRING_HANDLE ? "STRING" : NULL;
	int esize = ct ? ct->size : (count > 0 ? size / count : size);
	if (handle == LOGIX_STRING_HANDLE) esize = LOGIX_STRING_SIZE;
	for (int i = 0; i < count && (i + 1) * esize <= size; i++) {
		int off = i * esize;
		if (i) BLIT(b, ",");
		if (handle == LOGIX_STRING_HANDLE) {
			char s[LOGIX_STRING_SIZE + 1] = {0};
			plc_tag_get_string(tag, off, s, sizeof s);
			bstr(b, s);
			continue;
		}
		if (!ct) {
			uint8_t raw[4096];
			int n = esize < (int)sizeof raw ? esize : (int)sizeof raw;
			plc_tag_get_raw_bytes(tag, off, raw, n);
			bhex(b, raw, (size_t)n);
			continue;
		}
		switch (ct->code) {
		case 0xC1: bprintf(b, "%d", plc_tag_get_uint8(tag, off) ? 1 : 0); break;
		case 0xC2: bprintf(b, "%d", plc_tag_get_int8(tag, off)); break;
		case 0xC3: bprintf(b, "%d", plc_tag_get_int16(tag, off)); break;
		case 0xC4: bprintf(b, "%d", plc_tag_get_int32(tag, off)); break;
		case 0xC5: bprintf(b, "%lld", (long long)plc_tag_get_int64(tag, off)); break;
		case 0xC6: bprintf(b, "%u", plc_tag_get_uint8(tag, off)); break;
		case 0xC7: bprintf(b, "%u", plc_tag_get_uint16(tag, off)); break;
		case 0xC8:
		case 0xD3: bprintf(b, "%u", plc_tag_get_uint32(tag, off)); break;
		case 0xC9: bprintf(b, "%llu", (unsigned long long)plc_tag_get_uint64(tag, off)); break;
		case 0xCA: bdouble(b, plc_tag_get_float32(tag, off), true); break;
		case 0xCB: bdouble(b, plc_tag_get_float64(tag, off), false); break;
		}
	}
	BLIT(b, "]");
	return tname;
}

static char *tag_attrs(const target *t, const char *name, int count) {
	char *extra = NULL;
	if (asprintf(&extra, "name=%s&elem_count=%d", name, count) < 0) exit(1);
	char *a = logix_attrs(t, extra);
	free(extra);
	return a;
}

static bool valid_tag_name(const char *s) {
	/* libplctag's attribute string separates attributes with '&'. */
	return s && *s && !strchr(s, '&') && !strchr(s, '=');
}

static int op_read_tag(result *r, const target *t, const flags *fl) {
	const char *name = fget(fl, "tag");
	long long count = 1;
	if (!valid_tag_name(name)) return usage("read-tag needs --tag NAME");
	if (fget(fl, "count") && !parse_num(fget(fl, "count"), 1, 65535, &count)) return usage("--count: not a number in 1..65535");
	char *a = tag_attrs(t, name, (int)count);
	int32_t tag = create(a, t->timeout);
	free(a);
	if (tag < 0) {
		if (transport_error(tag)) return fail_rc(r, tag, "plc_tag_create");
		ok_head(r);
		BLIT(&r->doc, ",\"status\":-1,\"errorText\":");
		bstr(&r->doc, plc_tag_decode_error(tag));
		BLIT(&r->doc, ",\"values\":[]");
		return emit(r, 0);
	}
	/* libplctag read the tag when it created it (to learn its type): that
	 * read is the operation's. */
	ok_head(r);
	buf v = {0};
	const char *tname = values(&v, tag, name, (int)count);
	BLIT(&r->doc, ",\"status\":0,\"errorText\":null,\"type\":");
	bstr(&r->doc, tname);
	BLIT(&r->doc, ",\"values\":");
	bput(&r->doc, v.s, v.len);
	free(v.s);
	plc_tag_destroy(tag);
	return emit(r, 0);
}

static int op_read_tags(result *r, const target *t, const flags *fl) {
	const char *list = fget(fl, "tags");
	if (!list || !*list) return usage("read-tags needs --tags A,B,C");
	char *copy = strdup(list);
	char *names[64];
	int n = 0;
	for (char *save = NULL, *p = strtok_r(copy, ",", &save); p; p = strtok_r(NULL, ",", &save)) {
		if (n == 64 || !valid_tag_name(p)) {
			free(copy);
			return usage("--tags: at most 64 tag names");
		}
		names[n++] = p;
	}
	if (!n) {
		free(copy);
		return usage("--tags: no tag names");
	}
	/* Created without waiting, so that libplctag sends their first reads
	 * together (it packs them into Multiple Service Packets). */
	int32_t tags[64];
	for (int i = 0; i < n; i++) {
		char *a = tag_attrs(t, names[i], 1);
		tags[i] = plc_tag_create(a, 0);
		free(a);
	}
	struct timespec start, now;
	clock_gettime(CLOCK_MONOTONIC, &start);
	for (;;) {
		bool pending = false;
		for (int i = 0; i < n; i++)
			if (tags[i] >= 0 && plc_tag_status(tags[i]) == PLCTAG_STATUS_PENDING) pending = true;
		if (!pending) break;
		clock_gettime(CLOCK_MONOTONIC, &now);
		if ((now.tv_sec - start.tv_sec) * 1000 + (now.tv_nsec - start.tv_nsec) / 1000000 > t->timeout) break;
		usleep(2000);
	}
	/* No usable answer at all: a failed operation. */
	int rc0 = tags[0] < 0 ? tags[0] : plc_tag_status(tags[0]);
	bool all_transport = true;
	for (int i = 0; i < n; i++) {
		int rc = tags[i] < 0 ? tags[i] : plc_tag_status(tags[i]);
		if (rc == PLCTAG_STATUS_PENDING) rc = PLCTAG_ERR_TIMEOUT;
		if (!transport_error(rc)) all_transport = false;
		if (i == 0) rc0 = rc;
	}
	if (all_transport) {
		for (int i = 0; i < n; i++)
			if (tags[i] >= 0) plc_tag_destroy(tags[i]);
		free(copy);
		return fail_rc(r, rc0, "read-tags");
	}
	ok_head(r);
	BLIT(&r->doc, ",\"results\":[");
	for (int i = 0; i < n; i++) {
		int rc = tags[i] < 0 ? tags[i] : plc_tag_status(tags[i]);
		if (rc == PLCTAG_STATUS_PENDING) rc = PLCTAG_ERR_TIMEOUT;
		if (i) BLIT(&r->doc, ",");
		BLIT(&r->doc, "{\"tag\":");
		bstr(&r->doc, names[i]);
		if (rc == PLCTAG_STATUS_OK) {
			buf v = {0};
			const char *tname = values(&v, tags[i], names[i], 1);
			BLIT(&r->doc, ",\"status\":0,\"errorText\":null,\"type\":");
			bstr(&r->doc, tname);
			BLIT(&r->doc, ",\"values\":");
			bput(&r->doc, v.s, v.len);
			free(v.s);
		} else {
			BLIT(&r->doc, ",\"status\":-1,\"errorText\":");
			bstr(&r->doc, plc_tag_decode_error(rc));
			BLIT(&r->doc, ",\"values\":[]");
		}
		BLIT(&r->doc, "}");
		if (tags[i] >= 0) plc_tag_destroy(tags[i]);
	}
	BLIT(&r->doc, "]");
	free(copy);
	return emit(r, 0);
}

/* set_values stores the values of --values in a tag libplctag has read. */
static const char *set_values(int32_t tag, const char *type, char **vals, int n) {
	static char err[160];
	int rc = PLCTAG_STATUS_OK;
	if (strcmp(type, "STRING") == 0) {
		rc = plc_tag_set_string(tag, 0, vals[0]);
		if (rc != PLCTAG_STATUS_OK) snprintf(err, sizeof err, "plc_tag_set_string: %s", plc_tag_decode_error(rc));
		return rc == PLCTAG_STATUS_OK ? NULL : err;
	}
	const cip_type *ct = type_by_name(type);
	for (int i = 0; i < n && rc == PLCTAG_STATUS_OK; i++) {
		const char *s = vals[i];
		int off = i * ct->size;
		char *end = NULL;
		errno = 0;
		switch (ct->code) {
		case 0xCA: rc = plc_tag_set_float32(tag, off, strtof(s, &end)); break;
		case 0xCB: rc = plc_tag_set_float64(tag, off, strtod(s, &end)); break;
		case 0xC6:
		case 0xC7:
		case 0xC8:
		case 0xC9: {
			unsigned long long u = strtoull(s, &end, 0);
			if (ct->code == 0xC6) rc = plc_tag_set_uint8(tag, off, (uint8_t)u);
			if (ct->code == 0xC7) rc = plc_tag_set_uint16(tag, off, (uint16_t)u);
			if (ct->code == 0xC8) rc = plc_tag_set_uint32(tag, off, (uint32_t)u);
			if (ct->code == 0xC9) rc = plc_tag_set_uint64(tag, off, (uint64_t)u);
			break;
		}
		default: {
			long long v = strtoll(s, &end, 0);
			if (ct->code == 0xC1) rc = plc_tag_set_uint8(tag, off, v ? 1 : 0);
			if (ct->code == 0xC2) rc = plc_tag_set_int8(tag, off, (int8_t)v);
			if (ct->code == 0xC3) rc = plc_tag_set_int16(tag, off, (int16_t)v);
			if (ct->code == 0xC4) rc = plc_tag_set_int32(tag, off, (int32_t)v);
			if (ct->code == 0xC5) rc = plc_tag_set_int64(tag, off, (int64_t)v);
		}
		}
		if (end && *end) {
			snprintf(err, sizeof err, "value %s is not a %s", s, type);
			return err;
		}
	}
	if (rc != PLCTAG_STATUS_OK) {
		snprintf(err, sizeof err, "setting the values: %s", plc_tag_decode_error(rc));
		return err;
	}
	return NULL;
}

static int parse_bool(const char *s) {
	if (strcasecmp(s, "true") == 0 || strcmp(s, "1") == 0) return 1;
	if (strcasecmp(s, "false") == 0 || strcmp(s, "0") == 0) return 0;
	return -1;
}

static int op_write_tag(result *r, const target *t, const flags *fl) {
	const char *name = fget(fl, "tag"), *type = fget(fl, "type"), *vs = fget(fl, "values");
	if (!valid_tag_name(name)) return usage("write-tag needs --tag NAME");
	if (!type || (!type_by_name(type) && strcmp(type, "STRING") != 0)) return usage("--type: not a supported type");
	if (!vs) return usage("write-tag needs --values");
	char *copy = strdup(vs);
	char **vals = NULL;
	int n = 0;
	if (strcmp(type, "STRING") == 0) {
		/* One text, commas included. */
		if (strlen(vs) > 82) {
			free(copy);
			return usage("--values: a STRING holds at most 82 characters");
		}
		vals = malloc(sizeof *vals);
		vals[n++] = copy;
	} else {
		vals = malloc((strlen(vs) + 1) * sizeof *vals);
		char *p = copy;
		for (;;) {
			char *c = strchr(p, ',');
			if (c) *c = 0;
			if (!*p) {
				free(vals);
				free(copy);
				return usage("--values: empty value");
			}
			vals[n++] = p;
			if (!c) break;
			p = c + 1;
		}
	}
	bool bit = strcmp(type, "BOOL") == 0 && is_bit_name(name);
	if (strcmp(type, "BOOL") == 0) {
		for (int i = 0; i < n; i++) {
			int b = parse_bool(vals[i]);
			if (b < 0) {
				free(vals);
				free(copy);
				return usage("--values: BOOL values are true/false or 1/0");
			}
			vals[i] = b ? "1" : "0";
		}
	}
	if (bit && n != 1) {
		free(vals);
		free(copy);
		return usage("--values: a bit takes one value");
	}
	char *a = tag_attrs(t, name, strcmp(type, "STRING") == 0 ? 1 : n);
	int32_t tag = create(a, t->timeout);
	free(a);
	int rc = tag < 0 ? tag : PLCTAG_STATUS_OK;
	const char *err = NULL;
	char mismatch[128] = "";
	if (rc == PLCTAG_STATUS_OK && !bit) {
		/* libplctag writes with the type the tag has (from its first read),
		 * whatever --type says: refuse a mismatch rather than write the
		 * values at the wrong size. */
		uint8_t code;
		uint16_t handle;
		tag_type(tag, &code, &handle);
		const cip_type *have = code ? type_by_code(code) : NULL;
		const char *hname = have ? have->name : handle == LOGIX_STRING_HANDLE ? "STRING" : "a structure";
		bool match = strcmp(type, "STRING") == 0 ? handle == LOGIX_STRING_HANDLE
		                                         : have && (have->code == type_by_name(type)->code ||
		                                                    (have->code == 0xD3 && type_by_name(type)->code == 0xC8));
		if (!match) {
			snprintf(mismatch, sizeof mismatch, "the tag is %s, not %s; nothing written", hname, type);
			rc = PLCTAG_ERR_BAD_PARAM;
		}
	}
	if (rc == PLCTAG_STATUS_OK) {
		if (bit) {
			rc = plc_tag_set_bit(tag, 0, vals[0][0] == '1');
		} else {
			err = set_values(tag, type, vals, n);
		}
		if (rc == PLCTAG_STATUS_OK && !err) rc = plc_tag_write(tag, t->timeout);
	}
	free(vals);
	free(copy);
	if (tag >= 0) plc_tag_destroy(tag);
	if (err) return fail(r, "failed", "%s", err);
	if (rc != PLCTAG_STATUS_OK && transport_error(rc)) return fail_rc(r, rc, "write-tag");
	ok_head(r);
	if (rc == PLCTAG_STATUS_OK) {
		BLIT(&r->doc, ",\"status\":0,\"errorText\":null");
	} else {
		BLIT(&r->doc, ",\"status\":-1,\"errorText\":");
		bstr(&r->doc, *mismatch ? mismatch : plc_tag_decode_error(rc));
	}
	return emit(r, 0);
}

/* template_name reads a structure's name through libplctag's @udt/<id> tag. */
static char *template_name(const target *t, unsigned id) {
	char extra[64];
	snprintf(extra, sizeof extra, "name=@udt/%u", id);
	char *a = logix_attrs(t, extra);
	int32_t tag = plc_tag_create(a, t->timeout);
	free(a);
	if (tag < 0) return NULL;
	if (plc_tag_read(tag, t->timeout) != PLCTAG_STATUS_OK) {
		plc_tag_destroy(tag);
		return NULL;
	}
	int size = plc_tag_get_size(tag);
	uint8_t *d = malloc(size > 0 ? (size_t)size : 1);
	char *name = NULL;
	if (d && size >= 14 && plc_tag_get_raw_bytes(tag, 0, d, size) == PLCTAG_STATUS_OK) {
		/* 14-octet header, 8 octets per member, then the template name
		 * ("NAME;n..." up to a NUL) and the member names. */
		int members = u16(d + 10);
		int off = 14 + 8 * members;
		if (off < size) {
			int end = off;
			while (end < size && d[end] && d[end] != ';') end++;
			name = strndup((const char *)d + off, (size_t)(end - off));
		}
	}
	free(d);
	plc_tag_destroy(tag);
	return name;
}

static int op_tag_list(result *r, const target *t) {
	char *a = logix_attrs(t, "name=@tags");
	int32_t tag = create(a, t->timeout);
	free(a);
	int rc = tag < 0 ? tag : plc_tag_read(tag, t->timeout);
	if (rc != PLCTAG_STATUS_OK) {
		if (tag >= 0) plc_tag_destroy(tag);
		if (transport_error(rc)) return fail_rc(r, rc, "tag list (@tags)");
		/* The target answered, with an error: no tag list. */
		ok_head(r);
		BLIT(&r->doc, ",\"status\":-1,\"errorText\":");
		bstr(&r->doc, plc_tag_decode_error(rc));
		BLIT(&r->doc, ",\"tags\":[],\"programName\":null");
		return emit(r, 0);
	}
	int size = plc_tag_get_size(tag);
	uint8_t *d = malloc(size > 0 ? (size_t)size : 1);
	if (!d || plc_tag_get_raw_bytes(tag, 0, d, size) != PLCTAG_STATUS_OK) {
		plc_tag_destroy(tag);
		free(d);
		return fail(r, "failed", "reading the tag list buffer");
	}
	plc_tag_destroy(tag);
	ok_head(r);
	BLIT(&r->doc, ",\"tags\":[");
	/* Entries: instance id (4), symbol type (2), element size (2),
	 * three dimensions (3 x 4), name length (2), name. */
	struct {
		unsigned id;
		char *name;
	} cache[64];
	int ncache = 0;
	int off = 0, first = 1;
	while (off + 22 <= size) {
		uint16_t stype = u16(d + off + 4);
		uint32_t dims[3] = {u32(d + off + 8), u32(d + off + 12), u32(d + off + 16)};
		int nlen = u16(d + off + 20);
		if (off + 22 + nlen > size) break;
		char name[256];
		int cl = nlen < 255 ? nlen : 255;
		memcpy(name, d + off + 22, (size_t)cl);
		name[cl] = 0;
		off += 22 + nlen;
		if (stype & 0x1000) continue; /* system tags */
		if (strncmp(name, "__", 2) == 0) continue;
		char *tname = NULL;
		const char *tn = NULL;
		if (stype & 0x8000) {
			unsigned id = stype & 0x0FFF;
			for (int i = 0; i < ncache; i++)
				if (cache[i].id == id) tn = cache[i].name;
			if (!tn) {
				char *found = template_name(t, id);
				if (ncache < 64) {
					cache[ncache].id = id;
					cache[ncache].name = found;
					ncache++;
				} else {
					tname = found; /* freed below */
				}
				tn = found;
			}
		} else {
			const cip_type *ct = type_by_code((uint8_t)(stype & 0xFF));
			tn = ct ? ct->name : NULL;
		}
		if (!first) BLIT(&r->doc, ",");
		first = 0;
		BLIT(&r->doc, "{\"name\":");
		bstr(&r->doc, name);
		BLIT(&r->doc, ",\"type\":");
		bstr(&r->doc, tn);
		BLIT(&r->doc, ",\"dims\":[");
		int nd = (stype >> 13) & 3;
		for (int i = 0; i < nd && i < 3; i++) bprintf(&r->doc, "%s%u", i ? "," : "", dims[i]);
		BLIT(&r->doc, "]}");
		free(tname);
	}
	for (int i = 0; i < ncache; i++) free(cache[i].name);
	free(d);
	BLIT(&r->doc, "],\"status\":0,\"errorText\":null,\"programName\":null");
	return emit(r, 0);
}

static const struct {
	const char *op;
	const char *flags;
} OPS[] = {
    {"get-attribute", "class instance attribute connected"},
    {"set-attribute", "class instance attribute data connected"},
    {"get-attributes-all", "class instance connected"},
    {"identity", ""},
    {"read-tag", "tag count"},
    {"read-tags", "tags"},
    {"write-tag", "tag type values"},
    {"tag-list", ""},
};

static int client(int argc, char **argv) {
	if (argc < 1) return usage("client needs an operation");
	const char *op = argv[0];
	const char *opflags = NULL;
	for (size_t i = 0; i < sizeof OPS / sizeof OPS[0]; i++)
		if (strcmp(OPS[i].op, op) == 0) opflags = OPS[i].flags;
	if (!opflags) return usage("operation %s is not supported by the libplctag adapter", op);
	char allowed[256];
	snprintf(allowed, sizeof allowed, "host port timeout-ms %s", opflags);
	flags fl;
	int rc = parse_flags(argc - 1, argv + 1, allowed, &fl);
	if (rc) return rc;
	target t = {fget(&fl, "host"), 44818, 5000, 0};
	if (!t.host || !*t.host) return usage("--host is required");
	if (strchr(t.host, '&') || strchr(t.host, '=') || strchr(t.host, ':')) return usage("--host: not an address");
	long long v;
	if (fget(&fl, "port")) {
		if (!parse_num(fget(&fl, "port"), 1, 65535, &v)) return usage("--port: not a port number");
		t.port = (int)v;
	}
	if (fget(&fl, "timeout-ms")) {
		if (!parse_num(fget(&fl, "timeout-ms"), 1, 3600000, &v)) return usage("--timeout-ms: not a positive number");
		t.timeout = (int)v;
	}

	/* Diagnostics on stderr: libplctag's own log (packet dumps from level 4). */
	const char *dbg = getenv("LIBPLCTAG_DEBUG");
	if (dbg && *dbg) plc_tag_set_debug_level(atoi(dbg));

	result r = {op, {0}};
	if (strcmp(op, "get-attribute") == 0 || strcmp(op, "set-attribute") == 0 || strcmp(op, "get-attributes-all") == 0)
		rc = op_generic(&r, &t, &fl);
	else if (strcmp(op, "identity") == 0)
		rc = op_identity(&r, &t);
	else if (strcmp(op, "read-tag") == 0)
		rc = op_read_tag(&r, &t, &fl);
	else if (strcmp(op, "read-tags") == 0)
		rc = op_read_tags(&r, &t, &fl);
	else if (strcmp(op, "write-tag") == 0)
		rc = op_write_tag(&r, &t, &fl);
	else
		rc = op_tag_list(&r, &t);
	free(r.doc.s);
	plc_tag_shutdown();
	return rc;
}

/* ------------------------------------------------------------------------ */
/* Server                                                                   */

static volatile sig_atomic_t stopping = 0;
static void on_signal(int sig) {
	(void)sig;
	stopping = 1;
}

static char *read_file(const char *path) {
	FILE *f = fopen(path, "rb");
	if (!f) return NULL;
	buf b = {0};
	char tmp[4096];
	size_t n;
	while ((n = fread(tmp, 1, sizeof tmp, f)) > 0) bput(&b, tmp, n);
	fclose(f);
	if (!b.s) BLIT(&b, "");
	return b.s;
}

/* The types ab_server's ControlLogix simulator has (main.c parse_cip_tag). */
static const char *SERVER_TYPES[] = {"BOOL", "SINT", "INT", "DINT", "LINT", "REAL", "LREAL", "STRING"};

static bool server_type(const char *t) {
	for (size_t i = 0; i < sizeof SERVER_TYPES / sizeof SERVER_TYPES[0]; i++)
		if (strcmp(SERVER_TYPES[i], t) == 0) return true;
	return false;
}

static const char *str_of(const jvalue *v) { return v && v->type == J_STRING ? v->text : NULL; }

static bool int_of(const jvalue *v, long long *out) {
	if (!v || v->type != J_NUMBER) return false;
	char *end;
	errno = 0;
	long long x = strtoll(v->text, &end, 10);
	if (errno || *end) return false;
	*out = x;
	return true;
}

/* fixture_problem says why ab_server cannot serve a fixture, or NULL. */
static const char *fixture_problem(const jvalue *fx) {
	static char why[256];
	if (!fx || fx->type != J_OBJECT) return "not a JSON object";
	const jvalue *sv = json_get(fx, "schemaVersion");
	if (!str_of(sv) || strcmp(sv->text, "1.0") != 0) return "schemaVersion is not \"1.0\"";
	const char *name = str_of(json_get(fx, "name"));
	if (!name || !*name) return "no name";
	const jvalue *echo = json_get(fx, "echoOutputToInput");
	if (echo && echo->type == J_TRUE) return "echoOutputToInput: ab_server has no assemblies";
	const jvalue *tpl = json_get(fx, "templates");
	if (tpl && tpl->type == J_ARRAY && tpl->n > 0) return "templates: ab_server has no structure templates";
	const jvalue *slot = json_get(fx, "slot");
	long long s = 0;
	if (slot && (!int_of(slot, &s) || s < 0 || s > 255)) return "slot is not 0..255";
	const jvalue *tags = json_get(fx, "tags");
	if (!tags || tags->type != J_ARRAY || tags->n == 0) return "no tags: ab_server serves tags only";
	for (size_t i = 0; i < tags->n; i++) {
		const jvalue *t = tags->items[i];
		const char *tn = str_of(json_get(t, "name"));
		const char *ty = str_of(json_get(t, "type"));
		if (!tn || !ty) return "a tag without name or type";
		for (const char *p = tn; *p; p++)
			if (!isalnum((unsigned char)*p) && *p != '_') {
				snprintf(why, sizeof why, "tag %s: ab_server takes letters, digits and _ only", tn);
				return why;
			}
		if (!server_type(ty)) {
			snprintf(why, sizeof why, "tag %s: ab_server has no type %s", tn, ty);
			return why;
		}
		long long count = 1;
		const jvalue *c = json_get(t, "count");
		if (c && (!int_of(c, &count) || count < 2 || count > 100000)) {
			snprintf(why, sizeof why, "tag %s: bad count", tn);
			return why;
		}
		const jvalue *vals = json_get(t, "values"), *pat = json_get(t, "pattern"), *strs = json_get(t, "strings");
		if (strcmp(ty, "STRING") == 0) {
			if (!strs || strs->type != J_ARRAY || (long long)strs->n != count) {
				snprintf(why, sizeof why, "tag %s: a STRING tag needs %lld strings", tn, count);
				return why;
			}
			for (size_t k = 0; k < strs->n; k++)
				if (!str_of(strs->items[k]) || strlen(strs->items[k]->text) > 82) {
					snprintf(why, sizeof why, "tag %s: strings of at most 82 characters", tn);
					return why;
				}
		} else if (vals) {
			if (vals->type != J_ARRAY || (long long)vals->n != count) {
				snprintf(why, sizeof why, "tag %s: values must have %lld entries", tn, count);
				return why;
			}
			for (size_t k = 0; k < vals->n; k++)
				if (vals->items[k]->type != J_NUMBER) {
					snprintf(why, sizeof why, "tag %s: values must be numbers", tn);
					return why;
				}
		} else if (pat) {
			if (!c || pat->type != J_OBJECT || !json_get(pat, "multiplier") || !json_get(pat, "offset") ||
			    json_get(pat, "multiplier")->type != J_NUMBER || json_get(pat, "offset")->type != J_NUMBER) {
				snprintf(why, sizeof why, "tag %s: bad pattern", tn);
				return why;
			}
		} else {
			snprintf(why, sizeof why, "tag %s: no values", tn);
			return why;
		}
	}
	return NULL;
}

/* element_text gives the initial value of element i of a fixture tag as text. */
static void element_text(const jvalue *t, size_t i, const char *type, char *out, size_t outlen) {
	const jvalue *vals = json_get(t, "values");
	if (vals) {
		snprintf(out, outlen, "%s", vals->items[i]->text);
		return;
	}
	const jvalue *pat = json_get(t, "pattern");
	double m = strtod(json_get(pat, "multiplier")->text, NULL), o = strtod(json_get(pat, "offset")->text, NULL);
	double v = (double)i * m + o;
	if (strcmp(type, "REAL") == 0 || strcmp(type, "LREAL") == 0) snprintf(out, outlen, "%.17g", v);
	else snprintf(out, outlen, "%.0f", nearbyint(v));
}

/* write_initial writes the fixture's values into ab_server through libplctag. */
static bool write_initial(const jvalue *fx, int port, int slot) {
	const jvalue *tags = json_get(fx, "tags");
	target t = {"127.0.0.1", port, 5000, slot};
	bool okall = true;
	for (size_t i = 0; i < tags->n && okall; i++) {
		const jvalue *tg = tags->items[i];
		const char *name = json_get(tg, "name")->text, *type = json_get(tg, "type")->text;
		long long count = 1;
		int_of(json_get(tg, "count"), &count);
		char *a = tag_attrs(&t, name, (int)count);
		int32_t tag = plc_tag_create(a, t.timeout);
		free(a);
		if (tag < 0) {
			fprintf(stderr, "initial values: tag %s: %s\n", name, plc_tag_decode_error(tag));
			return false;
		}
		int rc = PLCTAG_STATUS_OK;
		if (strcmp(type, "STRING") == 0) {
			const jvalue *strs = json_get(tg, "strings");
			for (long long k = 0; k < count && rc == PLCTAG_STATUS_OK; k++)
				rc = plc_tag_set_string(tag, (int)k * LOGIX_STRING_SIZE, strs->items[k]->text);
		} else {
			const cip_type *ct = type_by_name(type);
			for (long long k = 0; k < count && rc == PLCTAG_STATUS_OK; k++) {
				char txt[64];
				element_text(tg, (size_t)k, type, txt, sizeof txt);
				int off = (int)k * ct->size;
				switch (ct->code) {
				case 0xC1: rc = plc_tag_set_uint8(tag, off, strtoll(txt, NULL, 10) ? 1 : 0); break;
				case 0xC2: rc = plc_tag_set_int8(tag, off, (int8_t)strtoll(txt, NULL, 10)); break;
				case 0xC3: rc = plc_tag_set_int16(tag, off, (int16_t)strtoll(txt, NULL, 10)); break;
				case 0xC4: rc = plc_tag_set_int32(tag, off, (int32_t)strtoll(txt, NULL, 10)); break;
				case 0xC5: rc = plc_tag_set_int64(tag, off, (int64_t)strtoll(txt, NULL, 10)); break;
				case 0xCA: rc = plc_tag_set_float32(tag, off, strtof(txt, NULL)); break;
				case 0xCB: rc = plc_tag_set_float64(tag, off, strtod(txt, NULL)); break;
				}
			}
		}
		if (rc == PLCTAG_STATUS_OK) rc = plc_tag_write(tag, t.timeout);
		if (rc != PLCTAG_STATUS_OK) {
			fprintf(stderr, "initial values: tag %s: %s\n", name, plc_tag_decode_error(rc));
			okall = false;
		}
		plc_tag_destroy(tag);
	}
	plc_tag_shutdown();
	return okall;
}

static bool accepts(int port) {
	int fd = socket(AF_INET, SOCK_STREAM, 0);
	if (fd < 0) return false;
	struct sockaddr_in sa = {0};
	sa.sin_family = AF_INET;
	sa.sin_port = htons((uint16_t)port);
	sa.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
	bool ok = connect(fd, (struct sockaddr *)&sa, sizeof sa) == 0;
	close(fd);
	return ok;
}

static void own_ip(char *out, size_t n) {
	struct ifaddrs *ifs = NULL, *i;
	snprintf(out, n, "0.0.0.0");
	if (getifaddrs(&ifs) != 0) return;
	for (i = ifs; i; i = i->ifa_next) {
		if (!i->ifa_addr || i->ifa_addr->sa_family != AF_INET) continue;
		struct in_addr a = ((struct sockaddr_in *)i->ifa_addr)->sin_addr;
		if ((ntohl(a.s_addr) >> 24) == 127) continue;
		inet_ntop(AF_INET, &a, out, (socklen_t)n);
		break;
	}
	freeifaddrs(ifs);
}

static void stop_child(pid_t pid) {
	/* ab_server stops on SIGINT (it has no SIGTERM handler). */
	kill(pid, SIGINT);
	for (int i = 0; i < 100; i++) {
		if (waitpid(pid, NULL, WNOHANG) == pid) return;
		usleep(50000);
	}
	kill(pid, SIGKILL);
	waitpid(pid, NULL, 0);
}

static int server(int argc, char **argv) {
	flags fl;
	int rc = parse_flags(argc, argv, "fixture port ready-file", &fl);
	if (rc) return rc;
	const char *fixture = fget(&fl, "fixture") ? fget(&fl, "fixture") : "/fixtures/" DEFAULT_FIXTURE "/fixture.json";
	const char *ready = fget(&fl, "ready-file") ? fget(&fl, "ready-file") : READY_FILE;
	long long port = 44818;
	if (fget(&fl, "port") && !parse_num(fget(&fl, "port"), 1, 65535, &port)) return usage("--port: not a port number");

	char *text = read_file(fixture);
	if (!text) {
		fprintf(stderr, "fixture %s: %s\n", fixture, strerror(errno));
		return 4;
	}
	const char *err = NULL;
	jvalue *fx = json_parse(text, &err);
	free(text);
	const char *problem = fx ? fixture_problem(fx) : err;
	if (problem) {
		fprintf(stderr, "fixture %s: %s\n", fixture, problem);
		json_free(fx);
		return 4;
	}
	long long slot = 0;
	int_of(json_get(fx, "slot"), &slot);

	/* ab_server's command line: one --tag=Name:TYPE[n] per fixture tag. */
	const jvalue *tags = json_get(fx, "tags");
	char **cargv = calloc(tags->n + 8, sizeof *cargv);
	int ca = 0;
	cargv[ca++] = AB_SERVER;
	cargv[ca++] = "--plc=ControlLogix";
	if (asprintf(&cargv[ca++], "--path=1,%lld", slot) < 0 || asprintf(&cargv[ca++], "--port=%lld", port) < 0) return 1;
	for (size_t i = 0; i < tags->n; i++) {
		long long count = 1;
		int_of(json_get(tags->items[i], "count"), &count);
		if (asprintf(&cargv[ca++], "--tag=%s:%s[%lld]", json_get(tags->items[i], "name")->text,
		             json_get(tags->items[i], "type")->text, count) < 0)
			return 1;
	}
	cargv[ca] = NULL;

	struct sigaction sa = {0};
	sa.sa_handler = on_signal;
	sigaction(SIGTERM, &sa, NULL);
	sigaction(SIGINT, &sa, NULL);
	if (*ready) unlink(ready);

	pid_t pid = fork();
	if (pid < 0) {
		perror("fork");
		return 1;
	}
	if (pid == 0) {
		/* stdout carries JSON only: ab_server's output goes to stderr. */
		dup2(STDERR_FILENO, STDOUT_FILENO);
		signal(SIGINT, SIG_DFL);
		signal(SIGTERM, SIG_DFL);
		execv(AB_SERVER, cargv);
		perror("exec ab_server");
		_exit(127);
	}

	bool up = false;
	for (int i = 0; i < 300 && !stopping; i++) {
		if (waitpid(pid, NULL, WNOHANG) == pid) {
			fprintf(stderr, "ab_server exited\n");
			json_free(fx);
			return 1;
		}
		if (accepts((int)port)) {
			up = true;
			break;
		}
		usleep(100000);
	}
	if (!up && !stopping) {
		fprintf(stderr, "ab_server did not accept connections within 30 s\n");
		stop_child(pid);
		json_free(fx);
		return 1;
	}
	if (!stopping && !write_initial(fx, (int)port, (int)slot)) {
		stop_child(pid);
		json_free(fx);
		return 1;
	}
	if (!stopping) {
		if (*ready) {
			int fd = open(ready, O_WRONLY | O_CREAT | O_TRUNC, 0644);
			if (fd >= 0) close(fd);
			else fprintf(stderr, "ready file %s: %s\n", ready, strerror(errno));
		}
		char ip[64];
		own_ip(ip, sizeof ip);
		buf ev = {0};
		bprintf(&ev, "{\"event\":\"ready\",\"adapter\":\"%s\",\"address\":\"%s:%lld\",\"fixture\":", ADAPTER, ip, port);
		bstr(&ev, json_get(fx, "name")->text);
		printf("%s}\n", ev.s);
		fflush(stdout);
		free(ev.s);
	}
	json_free(fx);
	while (!stopping) {
		if (waitpid(pid, NULL, WNOHANG) == pid) {
			fprintf(stderr, "ab_server exited\n");
			if (*ready) unlink(ready);
			return 1;
		}
		usleep(100000);
	}
	if (*ready) unlink(ready);
	stop_child(pid);
	printf("{\"event\":\"stopped\",\"adapter\":\"%s\"}\n", ADAPTER);
	fflush(stdout);
	return 0;
}

/* ------------------------------------------------------------------------ */
/* Other commands                                                           */

static int print_capabilities(void) {
	char *text = read_file(CAPABILITIES_FILE);
	if (!text) {
		fprintf(stderr, "%s: %s\n", CAPABILITIES_FILE, strerror(errno));
		return 1;
	}
	const char *ver = getenv("ETHERNETIP_INTEROP_ADAPTER_VERSION");
	if (!ver || !*ver) ver = "dev";
	buf b = {0};
	for (const char *p = text; *p; p++) {
		if (*p == '\n') continue;
		if (strncmp(p, "@ADAPTER_VERSION@", 17) == 0) {
			bput(&b, ver, strlen(ver));
			p += 16;
			continue;
		}
		bput(&b, p, 1);
	}
	printf("%s\n", b.s);
	free(b.s);
	free(text);
	return 0;
}

static int print_fixture(int argc, char **argv) {
	if (argc > 1) return usage("print-fixture takes at most one name");
	const char *name = argc ? argv[0] : DEFAULT_FIXTURE;
	if (!*name || strchr(name, '/') || strchr(name, '\\') || strstr(name, "..")) return usage("invalid fixture name %s", name);
	char path[512];
	snprintf(path, sizeof path, "/fixtures/%s/fixture.json", name);
	FILE *f = fopen(path, "rb");
	if (!f) return usage("no fixture %s", name);
	char tmp[4096];
	size_t n;
	while ((n = fread(tmp, 1, sizeof tmp, f)) > 0) fwrite(tmp, 1, n, stdout);
	fclose(f);
	return 0;
}

static int version(void) {
	const char *v = getenv("ETHERNETIP_INTEROP_ADAPTER_VERSION");
	const char *uv = getenv("ETHERNETIP_INTEROP_UPSTREAM_VERSION");
	const char *ur = getenv("ETHERNETIP_INTEROP_UPSTREAM_REVISION");
	printf("{\"adapter\":\"%s\",\"adapterVersion\":\"%s\",\"upstream\":\"libplctag\",\"upstreamVersion\":\"%s\","
	       "\"upstreamRevision\":\"%s\",\"contract\":\"1.0\"}\n",
	       ADAPTER, v && *v ? v : "dev", uv ? uv : "", ur ? ur : "");
	return 0;
}

int main(int argc, char **argv) {
	if (argc < 2) return usage("usage: ethernetip-interop server|client|print-capabilities|print-fixture|version");
	const char *cmd = argv[1];
	if (strcmp(cmd, "client") == 0) return client(argc - 2, argv + 2);
	if (strcmp(cmd, "server") == 0) return server(argc - 2, argv + 2);
	if (strcmp(cmd, "print-capabilities") == 0) return argc == 2 ? print_capabilities() : usage("print-capabilities takes no arguments");
	if (strcmp(cmd, "print-fixture") == 0) return print_fixture(argc - 2, argv + 2);
	if (strcmp(cmd, "version") == 0) return argc == 2 ? version() : usage("version takes no arguments");
	return usage("unknown command %s", cmd);
}
