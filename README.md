# toon

[![Go Reference](https://pkg.go.dev/badge/github.com/moonrhythm/toon.svg)](https://pkg.go.dev/github.com/moonrhythm/toon)

Encode-only Go implementation of [TOON](https://github.com/toon-format/spec)
(Token-Oriented Object Notation), spec v3.3.

TOON is a line-oriented, indentation-based text format for the JSON data
model. Compared with JSON it drops most punctuation, and arrays of uniform
objects collapse to a single field header plus one delimited row per element —
typically ~40% fewer LLM tokens than JSON for list-shaped data.

## Install

```sh
go get github.com/moonrhythm/toon
```

## Usage

```go
b, err := toon.Marshal(v)
```

`Marshal` interprets `v` using `encoding/json` semantics — `json` struct tags
(`omitempty`, `-`, renames) and custom `MarshalJSON` implementations are
honored — then renders TOON. Anything that marshals correctly to JSON marshals
correctly to TOON, with object key order and full number precision preserved.

```go
type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

b, _ := toon.Marshal(map[string]any{
	"users": []User{
		{1, "Alice", "alice@example.com"},
		{2, "Bob", "bob@example.com"},
	},
})
```

```
users[2]{id,name,email}:
  1,Alice,alice@example.com
  2,Bob,bob@example.com
```

`toon.MediaType` (`text/toon`) is the format's provisional media type, for
HTTP content negotiation.

## Empty fields are omitted by default

Empty object fields — `null`, `""`, `{}`, `[]` (after their own contents are
pruned) — are omitted from the output: absent and empty carry the same
information for an LLM reader, and empty fields are pure token cost. `false`
and `0` are meaningful values and are always kept, and array elements are
never removed.

In an array of objects, a field is omitted only when it is empty in **every**
element — per-element omission would break the key-set uniformity that
enables the tabular layout (and cost more tokens than it saves).

Pass `toon.IncludeEmpty()` to encode the full data model verbatim:

```go
b, err := toon.Marshal(v, toon.IncludeEmpty())
```

## Format choices

Output is deterministic with fixed spec-default options: 2-space indent,
comma delimiter, length markers on, no key folding. There is no decoder —
the format targets LLM consumers, which read it natively.

## Conformance

The test suite includes the official encoder fixtures from the
[spec repository](https://github.com/toon-format/spec), and the encoder's
output for every fixture plus an adversarial corpus round-trips cleanly
through the reference TypeScript implementation's strict decoder.

## License

MIT
