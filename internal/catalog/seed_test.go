package catalog

import (
	"reflect"
	"testing"
)

func TestGenerateProductsDeterministic(t *testing.T) {
	a, b := GenerateProducts(500), GenerateProducts(500)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("generator is not deterministic")
	}
}

func TestGenerateProducts500Categories(t *testing.T) {
	ps := GenerateProducts(500)
	if len(ps) != 500 {
		t.Fatalf("len = %d", len(ps))
	}
	cats := map[string]bool{}
	for i, p := range ps {
		cats[p.Category] = true
		if p.ID != i+1 || p.Name == "" || p.PriceCents <= 0 || p.Stock <= 0 || len(p.Features) != FeatureDims {
			t.Fatalf("bad product %+v", p)
		}
	}
	if len(cats) < 10 {
		t.Fatalf("only %d categories", len(cats))
	}
}
