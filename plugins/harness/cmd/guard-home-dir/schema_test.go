package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

// schemaPath は plugin に同梱する guard-home-dir.yaml の JSON Schema。
const schemaPath = "../../schemas/guard-home-dir.schema.json"

// compileSchema は同梱の JSON Schema を読んで compile する。
func compileSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	abs, err := filepath.Abs(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	sch, err := c.Compile(abs)
	if err != nil {
		t.Fatalf("compile %s: %v", schemaPath, err)
	}
	return sch
}

// schemaAccepts は YAML を JSON の値へ直して schema に当て、受け付けるかを返す。
// YAML として読めないものは受け付けない扱いにする。
func schemaAccepts(t *testing.T, sch *jsonschema.Schema, data []byte) bool {
	t.Helper()
	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("YAML を JSON にできない: %v", err)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return sch.Validate(inst) == nil
}

// testdata/config の valid と invalid の YAML を、Go の読み方と schema の両方に当て、結果が一致することを見る。
// エディタの検査 (schema) と guard の判定 (Go) が食い違うと、エディタで通った YAML を guard が読めない。
func TestSchemaMatchesLoadConfig(t *testing.T) {
	sch := compileSchema(t)
	for _, dir := range []string{"valid", "invalid"} {
		want := dir == "valid"
		files, err := filepath.Glob(filepath.Join("testdata", "config", dir, "*.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if len(files) == 0 {
			t.Fatalf("testdata/config/%s に fixture が無い", dir)
		}
		for _, f := range files {
			t.Run(dir+"/"+filepath.Base(f), func(t *testing.T) {
				data, err := os.ReadFile(f)
				if err != nil {
					t.Fatal(err)
				}
				xdg := writeConfig(t, string(data))
				cfg := loadConfig("/home/tester", envOf(map[string]string{"XDG_CONFIG_HOME": xdg}))
				if got := cfg.state == configOK; got != want {
					t.Errorf("loadConfig ok = %v, want %v (err: %v)", got, want, cfg.err)
				}
				if got := schemaAccepts(t, sch, data); got != want {
					t.Errorf("schema accepts = %v, want %v", got, want)
				}
			})
		}
	}
}
