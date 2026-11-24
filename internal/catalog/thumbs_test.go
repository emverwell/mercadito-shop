package catalog

import (
	"errors"
	"testing"
)

func never() bool  { return false }
func always() bool { return true }

func TestThumbCachedSecondCall(t *testing.T) {
	th := NewThumbs(0, never)
	a, err := th.Get(1, 64)
	if err != nil || len(a) == 0 {
		t.Fatalf("get: %v len=%d", err, len(a))
	}
	size := th.Bytes()
	b, _ := th.Get(1, 64)
	if th.Bytes() != size || &a[0] != &b[0] {
		t.Fatal("second call should be served from cache")
	}
}

func TestBoundedCacheNeverExceedsMax(t *testing.T) {
	th := NewThumbs(1, never)
	for i := 0; i < 60; i++ {
		if _, err := th.Get(1, 200+i*8); err != nil {
			t.Fatal(err)
		}
		if th.Bytes() > 1<<20 {
			t.Fatalf("cache at %d bytes after %d widths, max is 1 MiB", th.Bytes(), i+1)
		}
	}
	if th.Bytes() == 0 {
		t.Fatal("bounded cache should still hold something")
	}
}

func TestUnboundedGrowsWithWidths(t *testing.T) {
	th := NewThumbs(0, never)
	var last int64
	for i := 0; i < 20; i++ {
		_, _ = th.Get(1, 100+i*20)
		if th.Bytes() <= last {
			t.Fatalf("cache did not grow at step %d", i)
		}
		last = th.Bytes()
	}
}

func TestBadWidthRejected(t *testing.T) {
	th := NewThumbs(0, never)
	for _, w := range []int{0, -5, 15, 2049, 4096} {
		if _, err := th.Get(1, w); !errors.Is(err, ErrBadWidth) {
			t.Fatalf("w=%d err=%v, want ErrBadWidth", w, err)
		}
	}
}

func TestBurstDefeatsCache(t *testing.T) {
	th := NewThumbs(0, always)
	_, _ = th.Get(1, 64)
	one := th.Bytes()
	_, _ = th.Get(1, 64)
	if th.Bytes() < one*2-1 {
		t.Fatalf("burst should add a new entry per call: %d -> %d", one, th.Bytes())
	}
}
