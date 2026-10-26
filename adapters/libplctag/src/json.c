/* SPDX-License-Identifier: MIT */
#include "json.h"

#include <ctype.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
	const char *p;
	const char *err;
	int depth;
} parser;

static jvalue *parse_value(parser *ps);

static void skip_ws(parser *ps) {
	while (*ps->p == ' ' || *ps->p == '\t' || *ps->p == '\n' || *ps->p == '\r') ps->p++;
}

static jvalue *new_value(jtype t) {
	jvalue *v = calloc(1, sizeof(*v));
	if (v) v->type = t;
	return v;
}

static int push(jvalue *v, char *key, jvalue *item) {
	jvalue **items = realloc(v->items, (v->n + 1) * sizeof(*items));
	if (!items) return 0;
	v->items = items;
	if (v->type == J_OBJECT) {
		char **keys = realloc(v->keys, (v->n + 1) * sizeof(*keys));
		if (!keys) return 0;
		v->keys = keys;
		v->keys[v->n] = key;
	}
	v->items[v->n++] = item;
	return 1;
}

static void put_utf8(char **o, unsigned cp) {
	char *q = *o;
	if (cp < 0x80) {
		*q++ = (char)cp;
	} else if (cp < 0x800) {
		*q++ = (char)(0xC0 | (cp >> 6));
		*q++ = (char)(0x80 | (cp & 0x3F));
	} else if (cp < 0x10000) {
		*q++ = (char)(0xE0 | (cp >> 12));
		*q++ = (char)(0x80 | ((cp >> 6) & 0x3F));
		*q++ = (char)(0x80 | (cp & 0x3F));
	} else {
		*q++ = (char)(0xF0 | (cp >> 18));
		*q++ = (char)(0x80 | ((cp >> 12) & 0x3F));
		*q++ = (char)(0x80 | ((cp >> 6) & 0x3F));
		*q++ = (char)(0x80 | (cp & 0x3F));
	}
	*o = q;
}

static int hex4(const char *p, unsigned *out) {
	unsigned v = 0;
	for (int i = 0; i < 4; i++) {
		char c = p[i];
		v <<= 4;
		if (c >= '0' && c <= '9') v |= (unsigned)(c - '0');
		else if (c >= 'a' && c <= 'f') v |= (unsigned)(c - 'a' + 10);
		else if (c >= 'A' && c <= 'F') v |= (unsigned)(c - 'A' + 10);
		else return 0;
	}
	*out = v;
	return 1;
}

static char *parse_string(parser *ps) {
	/* ps->p is at the opening quote. The decoded text is never longer than the source. */
	const char *s = ++ps->p;
	const char *e = s;
	while (*e && *e != '"') {
		if (*e == '\\' && e[1]) e++;
		e++;
	}
	if (*e != '"') {
		ps->err = "unterminated string";
		return NULL;
	}
	char *out = malloc((size_t)(e - s) + 1);
	if (!out) {
		ps->err = "out of memory";
		return NULL;
	}
	char *o = out;
	const char *p = s;
	while (p < e) {
		unsigned char c = (unsigned char)*p;
		if (c < 0x20) {
			ps->err = "control character in string";
			free(out);
			return NULL;
		}
		if (c != '\\') {
			*o++ = (char)c;
			p++;
			continue;
		}
		p++;
		switch (*p) {
		case '"': *o++ = '"'; break;
		case '\\': *o++ = '\\'; break;
		case '/': *o++ = '/'; break;
		case 'b': *o++ = '\b'; break;
		case 'f': *o++ = '\f'; break;
		case 'n': *o++ = '\n'; break;
		case 'r': *o++ = '\r'; break;
		case 't': *o++ = '\t'; break;
		case 'u': {
			unsigned cp;
			if (p + 5 > e || !hex4(p + 1, &cp)) {
				ps->err = "bad \\u escape";
				free(out);
				return NULL;
			}
			p += 4;
			if (cp >= 0xD800 && cp < 0xDC00 && p + 7 <= e && p[1] == '\\' && p[2] == 'u') {
				unsigned lo;
				if (hex4(p + 3, &lo) && lo >= 0xDC00 && lo < 0xE000) {
					cp = 0x10000 + ((cp - 0xD800) << 10) + (lo - 0xDC00);
					p += 6;
				}
			}
			put_utf8(&o, cp);
			break;
		}
		default:
			ps->err = "bad escape";
			free(out);
			return NULL;
		}
		p++;
	}
	*o = 0;
	ps->p = e + 1;
	return out;
}

static jvalue *parse_number(parser *ps) {
	const char *s = ps->p;
	const char *p = s;
	if (*p == '-') p++;
	if (*p == '0') {
		p++;
	} else if (isdigit((unsigned char)*p)) {
		while (isdigit((unsigned char)*p)) p++;
	} else {
		ps->err = "bad number";
		return NULL;
	}
	if (*p == '.') {
		p++;
		if (!isdigit((unsigned char)*p)) {
			ps->err = "bad number";
			return NULL;
		}
		while (isdigit((unsigned char)*p)) p++;
	}
	if (*p == 'e' || *p == 'E') {
		p++;
		if (*p == '+' || *p == '-') p++;
		if (!isdigit((unsigned char)*p)) {
			ps->err = "bad number";
			return NULL;
		}
		while (isdigit((unsigned char)*p)) p++;
	}
	jvalue *v = new_value(J_NUMBER);
	if (!v || !(v->text = strndup(s, (size_t)(p - s)))) {
		free(v);
		ps->err = "out of memory";
		return NULL;
	}
	ps->p = p;
	return v;
}

static jvalue *parse_container(parser *ps, int object) {
	if (++ps->depth > 64) {
		ps->err = "nested too deeply";
		return NULL;
	}
	jvalue *v = new_value(object ? J_OBJECT : J_ARRAY);
	if (!v) {
		ps->err = "out of memory";
		return NULL;
	}
	char close = object ? '}' : ']';
	ps->p++;
	skip_ws(ps);
	if (*ps->p == close) {
		ps->p++;
		ps->depth--;
		return v;
	}
	for (;;) {
		char *key = NULL;
		skip_ws(ps);
		if (object) {
			if (*ps->p != '"') {
				ps->err = "expected a member name";
				goto fail;
			}
			if (!(key = parse_string(ps))) goto fail;
			skip_ws(ps);
			if (*ps->p != ':') {
				free(key);
				ps->err = "expected ':'";
				goto fail;
			}
			ps->p++;
		}
		jvalue *item = parse_value(ps);
		if (!item) {
			free(key);
			goto fail;
		}
		if (!push(v, key, item)) {
			free(key);
			json_free(item);
			ps->err = "out of memory";
			goto fail;
		}
		skip_ws(ps);
		if (*ps->p == ',') {
			ps->p++;
			continue;
		}
		if (*ps->p == close) {
			ps->p++;
			ps->depth--;
			return v;
		}
		ps->err = object ? "expected ',' or '}'" : "expected ',' or ']'";
		goto fail;
	}
fail:
	json_free(v);
	return NULL;
}

static jvalue *parse_value(parser *ps) {
	skip_ws(ps);
	switch (*ps->p) {
	case '{': return parse_container(ps, 1);
	case '[': return parse_container(ps, 0);
	case '"': {
		char *s = parse_string(ps);
		if (!s) return NULL;
		jvalue *v = new_value(J_STRING);
		if (!v) {
			free(s);
			ps->err = "out of memory";
			return NULL;
		}
		v->text = s;
		return v;
	}
	case 't':
		if (strncmp(ps->p, "true", 4) == 0) {
			ps->p += 4;
			return new_value(J_TRUE);
		}
		break;
	case 'f':
		if (strncmp(ps->p, "false", 5) == 0) {
			ps->p += 5;
			return new_value(J_FALSE);
		}
		break;
	case 'n':
		if (strncmp(ps->p, "null", 4) == 0) {
			ps->p += 4;
			return new_value(J_NULL);
		}
		break;
	default:
		if (*ps->p == '-' || isdigit((unsigned char)*ps->p)) return parse_number(ps);
	}
	ps->err = "unexpected character";
	return NULL;
}

jvalue *json_parse(const char *text, const char **err) {
	parser ps = {text, NULL, 0};
	jvalue *v = parse_value(&ps);
	if (v) {
		skip_ws(&ps);
		if (*ps.p) {
			json_free(v);
			v = NULL;
			ps.err = "trailing characters";
		}
	}
	if (!v && err) *err = ps.err ? ps.err : "invalid JSON";
	return v;
}

void json_free(jvalue *v) {
	if (!v) return;
	for (size_t i = 0; i < v->n; i++) {
		json_free(v->items[i]);
		if (v->keys) free(v->keys[i]);
	}
	free(v->items);
	free(v->keys);
	free(v->text);
	free(v);
}

const jvalue *json_get(const jvalue *obj, const char *key) {
	if (!obj || obj->type != J_OBJECT) return NULL;
	for (size_t i = 0; i < obj->n; i++)
		if (strcmp(obj->keys[i], key) == 0) return obj->items[i];
	return NULL;
}
