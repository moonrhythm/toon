package toon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// --- Conformance fixtures ---------------------------------------------------
//
// testdata/encode-*.json are copied verbatim from the canonical TOON spec repo
// (github.com/toon-format/spec, tests/fixtures/encode). Each input is fed
// through Marshal as a json.RawMessage so the original key order and number
// literals are preserved.

type fixtureFile struct {
	Category string        `json:"category"`
	Tests    []fixtureTest `json:"tests"`
}

type fixtureTest struct {
	Name        string          `json:"name"`
	Input       json.RawMessage `json:"input"`
	Expected    string          `json:"expected"`
	ShouldError bool            `json:"shouldError"`
	Options     *struct {
		Delimiter  string `json:"delimiter"`
		Indent     *int   `json:"indent"`
		KeyFolding string `json:"keyFolding"`
	} `json:"options"`
}

func TestFixtures(t *testing.T) {
	files, err := filepath.Glob("testdata/encode-*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no fixture files found")
	}

	for _, file := range files {
		file := file
		t.Run(filepath.Base(file), func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var ff fixtureFile
			if err := json.Unmarshal(raw, &ff); err != nil {
				t.Fatal(err)
			}
			for _, tc := range ff.Tests {
				tc := tc
				// Skip cases this encoder does not target: non-default
				// delimiter, non-default indent, key folding, and error cases.
				if tc.ShouldError {
					continue
				}
				if tc.Options != nil {
					if tc.Options.Delimiter != "" && tc.Options.Delimiter != "," {
						continue
					}
					if tc.Options.Indent != nil && *tc.Options.Indent != indentUnit {
						continue
					}
					if tc.Options.KeyFolding != "" && tc.Options.KeyFolding != "off" {
						continue
					}
				}
				t.Run(tc.Name, func(t *testing.T) {
					got, err := Marshal(tc.Input)
					if err != nil {
						t.Fatalf("Marshal error: %v", err)
					}
					if string(got) != tc.Expected {
						t.Errorf("mismatch\ninput:    %s\ngot:      %q\nexpected: %q", tc.Input, got, tc.Expected)
					}
				})
			}
		})
	}
}

// --- Go-value table tests ---------------------------------------------------

func TestMarshal(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		// primitives
		{"root string", "hello", "hello"},
		{"root string quoted number-like", "42", `"42"`},
		{"root empty string", "", `""`},
		{"root bool true", true, "true"},
		{"root bool false", false, "false"},
		{"root nil", nil, "null"},
		{"root int", 42, "42"},
		{"root negative int", -7, "-7"},
		{"root float", 3.14, "3.14"},
		{"root zero", 0, "0"},
		{"unicode unquoted", "café 🚀 你好", "café 🚀 你好"},

		// number canonicalization (exponent forms from Go's json output)
		{"float 1e6 -> decimal", 1e6, "1000000"},
		{"float 1e-6 -> decimal", 1e-6, "0.000001"},
		{"float 1e20 -> decimal", 1e20, "100000000000000000000"},
		{"float 1e21 -> exponent", 1e21, "1e+21"},
		{"float 1e-7 -> exponent", 1e-7, "1e-7"},
		{"trailing zeros trimmed", 1.500, "1.5"},
		{"whole float to int", 10.0, "10"},
		{"max safe int", int64(9007199254740991), "9007199254740991"},

		// string quoting triggers
		{"colon", map[string]any{"note": "a:b"}, `note: "a:b"`},
		{"comma", map[string]any{"note": "a,b"}, `note: "a,b"`},
		{"backslash", map[string]any{"p": `C:\x`}, `p: "C:\\x"`},
		{"quote", map[string]any{"t": `say "hi"`}, `t: "say \"hi\""`},
		{"newline", map[string]any{"t": "a\nb"}, `t: "a\nb"`},
		{"tab", map[string]any{"t": "a\tb"}, `t: "a\tb"`},
		{"control char lowercase hex", map[string]any{"v": "a\u0004b"}, `v: "a\u0004b"`},
		{"leading space", map[string]any{"t": " x"}, `t: " x"`},
		{"trailing space", map[string]any{"t": "x "}, `t: "x "`},
		{"looks like bool", map[string]any{"v": "true"}, `v: "true"`},
		{"looks like null", map[string]any{"v": "null"}, `v: "null"`},
		{"leading hyphen", map[string]any{"v": "-x"}, `v: "-x"`},
		{"single hyphen", map[string]any{"v": "-"}, `v: "-"`},
		{"brackets", map[string]any{"v": "[x]"}, `v: "[x]"`},
		{"braces", map[string]any{"v": "{x}"}, `v: "{x}"`},

		// unicode boundary whitespace: an unquoted leading/trailing space-like
		// rune would be stripped by a decoder, corrupting the value
		{"leading nbsp", map[string]any{"v": "\u00a0x"}, "v: \"\u00a0x\""},
		{"trailing nbsp", map[string]any{"v": "x\u00a0"}, "v: \"x\u00a0\""},
		{"lone nbsp", map[string]any{"v": "\u00a0"}, "v: \"\u00a0\""},
		{"leading ideographic space", map[string]any{"v": "\u3000x"}, "v: \"\u3000x\""},
		{"leading zwnbsp bom", map[string]any{"v": "\ufeffx"}, "v: \"\ufeffx\""},
		{"leading line separator", map[string]any{"v": "\u2028x"}, "v: \"\u2028x\""},
		{"interior nbsp stays bare", map[string]any{"v": "a\u00a0b"}, "v: a\u00a0b"},

		// null value in object
		{"null value", map[string]any{"v": nil}, "v: null"},

		// arrays of primitives
		{"primitive array", map[string]any{"tags": []string{"a", "b"}}, "tags[2]: a,b"},
		{"empty array field", map[string]any{"items": []any{}}, "items: []"},
		{"empty root array", []any{}, "[]"},
		{"root primitive array", []any{"x", "y", 10}, "[3]: x,y,10"},

		// arrays of arrays
		{"array of arrays", map[string]any{"pairs": [][]int{{1, 2}, {3}}},
			"pairs[2]:\n  - [2]: 1,2\n  - [1]: 3"},
		{"empty inner arrays", map[string]any{"pairs": [][]int{{}, {}}},
			"pairs[2]:\n  - [0]:\n  - [0]:"},

		// tabular arrays
		{"tabular", map[string]any{"items": []map[string]any{
			{"id": 1, "name": "Ada"}, {"id": 2, "name": "Bob"},
		}}, "items[2]{id,name}:\n  1,Ada\n  2,Bob"},

		// key quoting
		{"numeric key quoted", map[string]any{"123": "x"}, `"123": x`},
		{"empty key quoted", map[string]any{"": 1}, `"": 1`},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := Marshal(tc.in)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// --- Struct / json semantics ------------------------------------------------

type inner struct {
	X int `json:"x"`
}

type sample struct {
	ID       int      `json:"id"`
	Name     string   `json:"name"`
	Optional string   `json:"optional,omitempty"`
	Hidden   string   `json:"-"`
	Tags     []string `json:"tags"`
	Nested   inner    `json:"nested"`
}

func TestMarshalStructTags(t *testing.T) {
	s := sample{
		ID:     1,
		Name:   "Ada",
		Hidden: "secret",
		Tags:   []string{"a", "b"},
		Nested: inner{X: 9},
	}
	// Field order follows the struct declaration; omitempty drops Optional;
	// the "-" tag drops Hidden.
	want := "id: 1\nname: Ada\ntags[2]: a,b\nnested:\n  x: 9"
	got, err := Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMarshalOmitemptyPresent(t *testing.T) {
	s := sample{ID: 1, Name: "Ada", Optional: "here", Tags: nil, Nested: inner{X: 0}}
	// Tags is nil -> JSON null -> "tags: null". omitempty field present.
	want := "id: 1\nname: Ada\noptional: here\ntags: null\nnested:\n  x: 0"
	got, err := Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// color is an enum-like type with a custom MarshalJSON, mirroring the ~14 enum
// types in the api package.
type color int

const (
	red color = iota
	green
)

func (c color) MarshalJSON() ([]byte, error) {
	switch c {
	case red:
		return json.Marshal("red")
	case green:
		return json.Marshal("green")
	default:
		return json.Marshal("unknown")
	}
}

func TestMarshalCustomMarshaler(t *testing.T) {
	v := map[string]any{"c": green}
	want := "c: green"
	got, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestDeterministic checks that struct field order is preserved and stable
// across runs (unlike map[string]any, which json sorts alphabetically).
func TestDeterministic(t *testing.T) {
	type ordered struct {
		Zebra int `json:"zebra"`
		Apple int `json:"apple"`
		Mango int `json:"mango"`
	}
	o := ordered{Zebra: 1, Apple: 2, Mango: 3}
	want := "zebra: 1\napple: 2\nmango: 3"
	for i := 0; i < 100; i++ {
		got, err := Marshal(o)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("iteration %d: got %q, want %q", i, got, want)
		}
	}
}

func TestEmptyObject(t *testing.T) {
	got, err := Marshal(struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestMediaType(t *testing.T) {
	if MediaType != "text/toon" {
		t.Errorf("MediaType = %q", MediaType)
	}
}
