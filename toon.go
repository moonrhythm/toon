// Package toon is an encode-only implementation of TOON (Token-Oriented Object
// Notation), spec v3.3 (https://github.com/toon-format/spec).
//
// TOON is a line-oriented, indentation-based text format for the JSON data
// model. Compared with JSON it drops most punctuation (braces, brackets around
// every array element, quotes around most strings) and encodes arrays of
// uniform objects as a compact table with a single field header instead of
// repeating keys on every row. The result is a representation that costs far
// fewer tokens to feed to an LLM while remaining unambiguous and deterministic.
//
// Only encoding is implemented; there is no decoder — the format targets LLM
// consumers, which read it natively.
//
// Values are first interpreted using encoding/json semantics — json struct
// tags and custom json.Marshaler implementations are honored — then rendered
// as TOON. Anything that marshals correctly to JSON marshals correctly to
// TOON, with object key order and full number precision preserved.
package toon

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MediaType is the provisional media type for TOON.
const MediaType = "text/toon"

// indentUnit is the number of spaces per indentation level (spec default: 2).
const indentUnit = 2

// Marshal encodes v as TOON. v is first interpreted using encoding/json
// semantics — json struct tags and custom MarshalJSON implementations are
// honored — then rendered as TOON.
func Marshal(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	root, err := parseValue(dec)
	if err != nil {
		return nil, err
	}

	var e encoder
	e.encodeRoot(root)
	return []byte(strings.Join(e.lines, "\n")), nil
}

// object is an order-preserving JSON object. map[string]any cannot be used
// because it loses field order, which TOON must preserve.
type object struct {
	keys []string
	vals []any
}

// parseValue reads one JSON value from dec into the internal representation:
// nil, bool, json.Number, string, *object, or []any.
func parseValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}

	delim, ok := tok.(json.Delim)
	if !ok {
		// primitive: nil, bool, json.Number, or string
		return tok, nil
	}

	switch delim {
	case '{':
		obj := &object{}
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key := kt.(string)
			val, err := parseValue(dec)
			if err != nil {
				return nil, err
			}
			obj.keys = append(obj.keys, key)
			obj.vals = append(obj.vals, val)
		}
		if _, err := dec.Token(); err != nil { // consume '}'
			return nil, err
		}
		return obj, nil
	case '[':
		arr := []any{}
		for dec.More() {
			val, err := parseValue(dec)
			if err != nil {
				return nil, err
			}
			arr = append(arr, val)
		}
		if _, err := dec.Token(); err != nil { // consume ']'
			return nil, err
		}
		return arr, nil
	default:
		return tok, nil
	}
}

type encoder struct {
	lines []string
}

func (e *encoder) line(s string) {
	e.lines = append(e.lines, s)
}

func spaces(level int) string {
	return strings.Repeat(" ", level*indentUnit)
}

// encodeRoot renders the document root (§5 root-form discovery).
func (e *encoder) encodeRoot(v any) {
	switch t := v.(type) {
	case *object:
		// Empty root object -> empty document (no lines).
		for i, k := range t.keys {
			e.writeField(spaces(0), 0, k, t.vals[i])
		}
	case []any:
		e.writeRootArray(t)
	default:
		e.line(encodePrimitive(v))
	}
}

// writeField renders an object member. lead is the exact leading text before the
// key (normally spaces(indent); for the first field of a list item it is
// spaces(indent-1)+"- "). indent is the level used for any nested children.
func (e *encoder) writeField(lead string, indent int, key string, v any) {
	kp := lead + encodeKey(key)

	switch t := v.(type) {
	case *object:
		if len(t.keys) == 0 {
			e.line(kp + ":")
			return
		}
		e.line(kp + ":")
		for i, k := range t.keys {
			e.writeField(spaces(indent+1), indent+1, k, t.vals[i])
		}
	case []any:
		if len(t) == 0 {
			e.line(kp + ": []")
			return
		}
		e.writeNonEmptyArray(kp, indent, t)
	default:
		e.line(kp + ": " + encodePrimitive(v))
	}
}

// writeRootArray renders an array in root position (§9, §6).
func (e *encoder) writeRootArray(arr []any) {
	if len(arr) == 0 {
		e.line("[]")
		return
	}
	e.writeNonEmptyArray("", 0, arr)
}

// writeNonEmptyArray renders a non-empty array whose header starts with prefix
// (spaces+key for an object field, "" for a root array). indent is the level of
// the header; rows/list items appear at indent+1.
func (e *encoder) writeNonEmptyArray(prefix string, indent int, arr []any) {
	n := strconv.Itoa(len(arr))

	if allPrimitive(arr) {
		e.line(prefix + "[" + n + "]: " + joinPrimitives(arr))
		return
	}

	if fields, ok := tabularFields(arr); ok {
		e.line(prefix + "[" + n + "]{" + joinFields(fields) + "}:")
		for _, el := range arr {
			e.line(spaces(indent+1) + joinRow(el.(*object), fields))
		}
		return
	}

	// Mixed / non-uniform array -> expanded list (§9.4).
	e.line(prefix + "[" + n + "]:")
	for _, el := range arr {
		e.writeListItem(el, indent+1)
	}
}

// writeListItem renders one element of an expanded-list array. h is the level of
// the hyphen marker; the item's field content sits at level h+1.
func (e *encoder) writeListItem(v any, h int) {
	ci := h + 1

	switch t := v.(type) {
	case *object:
		if len(t.keys) == 0 {
			// Empty object list item -> bare hyphen (§10).
			e.line(spaces(h) + "-")
			return
		}
		// First field shares the hyphen line; the rest are at level ci.
		e.writeField(spaces(h)+"- ", ci, t.keys[0], t.vals[0])
		for i := 1; i < len(t.keys); i++ {
			e.writeField(spaces(ci), ci, t.keys[i], t.vals[i])
		}
	case []any:
		e.writeArrayListElement(t, h)
	default:
		e.line(spaces(h) + "- " + encodePrimitive(v))
	}
}

// writeArrayListElement renders an array that is itself a direct list element
// (the "- [M]:" position). Per §9.4 the tabular form is not available here (no
// key on which to hang the field list), so arrays of objects use the expanded
// list form. Nested content sits at level h+1 relative to the hyphen.
func (e *encoder) writeArrayListElement(arr []any, h int) {
	if len(arr) == 0 {
		e.line(spaces(h) + "- [0]:")
		return
	}

	n := strconv.Itoa(len(arr))
	if allPrimitive(arr) {
		e.line(spaces(h) + "- [" + n + "]: " + joinPrimitives(arr))
		return
	}

	e.line(spaces(h) + "- [" + n + "]:")
	for _, el := range arr {
		e.writeListItem(el, h+1)
	}
}

// isPrimitive reports whether v is a TOON primitive (string, number, bool, null).
func isPrimitive(v any) bool {
	switch v.(type) {
	case nil, bool, json.Number, string:
		return true
	default:
		return false
	}
}

func allPrimitive(arr []any) bool {
	for _, v := range arr {
		if !isPrimitive(v) {
			return false
		}
	}
	return true
}

// tabularFields returns the ordered field list (from the first element) if arr
// qualifies for tabular encoding (§9.3): every element is a non-empty object,
// all elements share the same key set, and every value is a primitive.
func tabularFields(arr []any) ([]string, bool) {
	first, ok := arr[0].(*object)
	if !ok || len(first.keys) == 0 {
		return nil, false
	}

	fieldSet := make(map[string]struct{}, len(first.keys))
	for _, k := range first.keys {
		fieldSet[k] = struct{}{}
	}
	if len(fieldSet) != len(first.keys) {
		return nil, false // duplicate keys within an object -> bail to list form
	}

	for _, el := range arr {
		obj, ok := el.(*object)
		if !ok || len(obj.keys) != len(first.keys) {
			return nil, false
		}
		seen := make(map[string]struct{}, len(obj.keys))
		for i, k := range obj.keys {
			if _, inFirst := fieldSet[k]; !inFirst {
				return nil, false
			}
			if _, dup := seen[k]; dup {
				return nil, false
			}
			seen[k] = struct{}{}
			if !isPrimitive(obj.vals[i]) {
				return nil, false
			}
		}
	}
	return first.keys, true
}

func joinPrimitives(arr []any) string {
	parts := make([]string, len(arr))
	for i, v := range arr {
		parts[i] = encodePrimitive(v)
	}
	return strings.Join(parts, ",")
}

func joinFields(fields []string) string {
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = encodeKey(f)
	}
	return strings.Join(parts, ",")
}

// joinRow renders a tabular row, ordering cells by the header field order and
// looking each field up by key (element key order may differ from the header).
func joinRow(obj *object, fields []string) string {
	byKey := make(map[string]any, len(obj.keys))
	for i, k := range obj.keys {
		byKey[k] = obj.vals[i]
	}
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = encodePrimitive(byKey[f])
	}
	return strings.Join(parts, ",")
}

// encodePrimitive renders a primitive value (§7 strings, §2 numbers/bools/null).
func encodePrimitive(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		if t {
			return "true"
		}
		return "false"
	case json.Number:
		return canonicalNumber(string(t))
	case string:
		return encodeString(t)
	default:
		// Not reachable: parseValue only produces the types above.
		return encodeString("")
	}
}

var (
	// numericLike matches strings that must be quoted because they would parse
	// as a number (§7.2).
	numericLikeRe = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)
	// bareKeyRe matches keys that may be emitted unquoted (§7.3).
	bareKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)
)

// encodeString renders a string value, quoting and escaping only when required
// by §7.2.
func encodeString(s string) string {
	if needsQuote(s) {
		return quote(s)
	}
	return s
}

// encodeKey renders an object key or tabular field name (§7.3).
func encodeKey(k string) string {
	if bareKeyRe.MatchString(k) {
		return k
	}
	return quote(k)
}

// needsQuote implements §7.2 for the default (comma) delimiter.
func needsQuote(s string) bool {
	if s == "" {
		return true
	}
	first, _ := utf8.DecodeRuneInString(s)
	last, _ := utf8.DecodeLastRuneInString(s)
	if isBoundarySpace(first) || isBoundarySpace(last) {
		return true
	}
	if s == "true" || s == "false" || s == "null" {
		return true
	}
	if s[0] == '-' { // equals "-" or starts with "-"
		return true
	}
	if numericLikeRe.MatchString(s) {
		return true
	}
	// colon, quote, backslash, brackets, braces, or the comma delimiter.
	if strings.ContainsAny(s, ":\"\\[]{},") {
		return true
	}
	for _, r := range s {
		if r < 0x20 {
			return true
		}
	}
	return false
}

// isBoundarySpace reports whether a leading/trailing rune forces quoting: any
// Unicode whitespace, plus U+FEFF (ZWNBSP/BOM), which decoders treat as
// space-like. An unquoted boundary space would be stripped by a decoder,
// silently corrupting the value.
func isBoundarySpace(r rune) bool {
	return unicode.IsSpace(r) || r == '\uFEFF'
}

const hexDigits = "0123456789abcdef"

// quote wraps s in double quotes and applies the §7.1 escape table.
func quote(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				b.WriteString(`\u00`)
				b.WriteByte(hexDigits[(r>>4)&0xf])
				b.WriteByte(hexDigits[r&0xf])
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// canonicalNumber renders a JSON number in TOON canonical form (§2). Input is a
// valid JSON number literal; canonicalization is done on the decimal string to
// preserve full precision.
func canonicalNumber(s string) string {
	orig := s

	neg := false
	switch {
	case strings.HasPrefix(s, "-"):
		neg = true
		s = s[1:]
	case strings.HasPrefix(s, "+"):
		s = s[1:]
	}

	exp := 0
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		e, err := strconv.Atoi(s[i+1:])
		if err != nil {
			return orig
		}
		exp = e
		s = s[:i]
	}

	intPart, fracPart := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, fracPart = s[:i], s[i+1:]
	}

	digits := intPart + fracPart
	decExp := exp - len(fracPart) // value == digits * 10^decExp

	digits = strings.TrimLeft(digits, "0")
	for len(digits) > 0 && digits[len(digits)-1] == '0' {
		digits = digits[:len(digits)-1]
		decExp++
	}
	if digits == "" {
		return "0" // zero (also normalizes -0)
	}

	// E is floor(log10(|value|)).
	E := decExp + len(digits) - 1

	var out string
	if E >= -6 && E <= 20 {
		out = plainDecimal(digits, decExp)
	} else {
		out = expForm(digits, E)
	}
	if neg {
		return "-" + out
	}
	return out
}

// plainDecimal formats digits * 10^decExp without an exponent. digits has no
// leading or trailing zeros.
func plainDecimal(digits string, decExp int) string {
	if decExp >= 0 {
		return digits + strings.Repeat("0", decExp)
	}
	f := -decExp
	if f < len(digits) {
		return digits[:len(digits)-f] + "." + digits[len(digits)-f:]
	}
	return "0." + strings.Repeat("0", f-len(digits)) + digits
}

// expForm formats digits (mantissa) with exponent E in JSON exponent notation.
func expForm(digits string, E int) string {
	mant := digits
	if len(digits) > 1 {
		mant = digits[:1] + "." + digits[1:]
	}
	sign := "+"
	if E < 0 {
		sign = "-"
		E = -E
	}
	return mant + "e" + sign + strconv.Itoa(E)
}
