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
type Deployment struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Replicas int    `json:"replicas"`
}

b, _ := toon.Marshal(map[string]any{
	"items": []Deployment{
		{"api", "WebService", 2},
		{"worker", "Worker", 1},
	},
})
```

```
items[2]{name,type,replicas}:
  api,WebService,2
  worker,Worker,1
```

`toon.MediaType` (`text/toon`) is the format's provisional media type, for
HTTP content negotiation.

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
