package config

import (
	"strings"
	"testing"
	"time"
)

type sample struct {
	Port  int           `env:"T_PORT" default:"8081" doc:"Listen port."`
	Name  string        `env:"T_NAME" default:"x" doc:"A name."`
	Rate  float64       `env:"T_RATE" default:"0.5" doc:"A rate."`
	On    bool          `env:"T_ON" default:"false" doc:"A switch."`
	Wait  time.Duration `env:"T_WAIT" default:"2s" doc:"A wait."`
	Plain string
}

func TestDefaultsAndOverrides(t *testing.T) {
	var c sample
	if err := Load(&c); err != nil {
		t.Fatal(err)
	}
	if c.Port != 8081 || c.Name != "x" || c.Rate != 0.5 || c.On || c.Wait != 2*time.Second {
		t.Fatalf("defaults wrong: %+v", c)
	}
	t.Setenv("T_PORT", "9000")
	t.Setenv("T_ON", "true")
	t.Setenv("T_WAIT", "150ms")
	if err := Load(&c); err != nil {
		t.Fatal(err)
	}
	if c.Port != 9000 || !c.On || c.Wait != 150*time.Millisecond {
		t.Fatalf("overrides wrong: %+v", c)
	}
}

func TestInvalidIntNamesVariable(t *testing.T) {
	t.Setenv("T_PORT", "abc")
	var c sample
	err := Load(&c)
	if err == nil || !strings.Contains(err.Error(), "T_PORT") {
		t.Fatalf("err = %v, want it to name T_PORT", err)
	}
}

func TestDocsListsAllTags(t *testing.T) {
	vars := Docs(sample{})
	if len(vars) != 5 {
		t.Fatalf("got %d vars, want 5 (untagged fields skipped)", len(vars))
	}
	if vars[0].Name != "T_PORT" || vars[0].Default != "8081" || vars[0].Doc != "Listen port." || vars[0].Service != "sample" {
		t.Fatalf("first var = %+v", vars[0])
	}
}
