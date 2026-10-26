/* SPDX-License-Identifier: MIT */
/*
 * A small JSON reader for the fixture files: enough of RFC 8259 for
 * fixtures/<name>/fixture.json (objects, arrays, strings, numbers, true,
 * false, null). Numbers keep their text, so 64-bit integers survive.
 */
#ifndef ETHERNETIP_INTEROP_JSON_H
#define ETHERNETIP_INTEROP_JSON_H

#include <stddef.h>

typedef enum { J_NULL, J_FALSE, J_TRUE, J_NUMBER, J_STRING, J_ARRAY, J_OBJECT } jtype;

typedef struct jvalue {
	jtype type;
	char *text;            /* J_STRING: the decoded string; J_NUMBER: the literal */
	size_t n;              /* J_ARRAY / J_OBJECT: number of items */
	char **keys;           /* J_OBJECT: member names */
	struct jvalue **items; /* J_ARRAY / J_OBJECT: items / member values */
} jvalue;

/* json_parse parses a whole document; NULL (and *err set) when it is not JSON. */
jvalue *json_parse(const char *text, const char **err);
void json_free(jvalue *v);

/* json_get returns the member of an object, or NULL. */
const jvalue *json_get(const jvalue *obj, const char *key);

#endif
