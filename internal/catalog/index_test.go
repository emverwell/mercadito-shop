package catalog

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestSearchBeforeAndAfterWarmSameResults(t *testing.T) {
	ix := NewIndex(GenerateProducts(200))
	before := ix.Search("fruit", 20)
	ix.Warm(context.Background(), 20*time.Millisecond)
	after := ix.Search("fruit", 20)
	if len(before) == 0 || !reflect.DeepEqual(before, after) {
		t.Fatalf("results differ: %d vs %d", len(before), len(after))
	}
}

func TestReadyFalseUntilWarmDone(t *testing.T) {
	ix := NewIndex(GenerateProducts(50))
	if ix.Ready() {
		t.Fatal("ready before warm-up")
	}
	done := make(chan struct{})
	go func() { ix.Warm(context.Background(), 100*time.Millisecond); close(done) }()
	time.Sleep(20 * time.Millisecond)
	if ix.Ready() {
		t.Fatal("ready while warming")
	}
	<-done
	if !ix.Ready() {
		t.Fatal("not ready after warm-up")
	}
}

func TestWarmZeroIsImmediate(t *testing.T) {
	ix := NewIndex(GenerateProducts(10))
	ix.Warm(context.Background(), 0)
	if !ix.Ready() {
		t.Fatal("zero warm-up must be ready at once")
	}
}

func TestWarmStopsOnContextCancel(t *testing.T) {
	ix := NewIndex(GenerateProducts(10))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	ix.Warm(ctx, 5*time.Second)
	if time.Since(start) > time.Second {
		t.Fatal("Warm ignored cancelled context")
	}
}

func TestSearchEmptyQueryReturnsFirstN(t *testing.T) {
	ix := NewIndex(GenerateProducts(50))
	got := ix.Search("", 5)
	if len(got) != 5 || got[0].ID != 1 {
		t.Fatalf("got %d results, first id %d", len(got), got[0].ID)
	}
}
