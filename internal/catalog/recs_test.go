package catalog

import (
	"reflect"
	"testing"
	"time"
)

func TestRecommendDeterministic(t *testing.T) {
	all := GenerateProducts(100)
	a, b := Recommend(all, 5, 6), Recommend(all, 5, 6)
	if !reflect.DeepEqual(a, b) || len(a) != 6 {
		t.Fatalf("not deterministic or wrong length: %d", len(a))
	}
}

func TestRecommendExcludesSelfAndReturnsN(t *testing.T) {
	all := GenerateProducts(100)
	got := Recommend(all, 5, 4)
	if len(got) != 4 {
		t.Fatalf("len = %d", len(got))
	}
	for _, p := range got {
		if p.ID == 5 {
			t.Fatal("recommendation contains the product itself")
		}
	}
}

func TestRecommendUnknownIDReturnsNil(t *testing.T) {
	if got := Recommend(GenerateProducts(10), 999, 4); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestRecommendNLargerThanCatalog(t *testing.T) {
	if got := Recommend(GenerateProducts(3), 1, 10); len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
}

// Calibration guard: one call over the 500-product seed must cost 40-80 ms
// on a single core. Measured on the build machine; bounds are loose (30-150)
// to tolerate CI noise.
func TestRecommendCostIsInBand(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("timing test (skipped under -short or -race)")
	}
	all := GenerateProducts(500)
	Recommend(all, 1, 6) // warm
	const runs = 5
	start := time.Now()
	for i := 0; i < runs; i++ {
		Recommend(all, i+1, 6)
	}
	per := time.Since(start) / runs
	if per < 30*time.Millisecond || per > 150*time.Millisecond {
		t.Fatalf("per call = %v, want ~40-80ms (recsWorkPasses needs recalibration)", per)
	}
}

func BenchmarkRecommend500(b *testing.B) {
	all := GenerateProducts(500)
	for i := 0; i < b.N; i++ {
		Recommend(all, 1+i%500, 6)
	}
}
