package checkout

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakePlacer struct {
	order string
	total int
	err   error
	got   []Item
}

func (f *fakePlacer) Place(_ context.Context, _ string, items []Item) (string, int, error) {
	f.got = items
	return f.order, f.total, f.err
}

type fakeCart struct {
	items   []Item
	err     error
	cleared bool
}

func (f *fakeCart) Get(context.Context, string) ([]Item, error) { return f.items, f.err }
func (f *fakeCart) Clear(context.Context, string) error        { f.cleared = true; return nil }

func post(h http.Handler, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/checkout", strings.NewReader(body)))
	return rec
}

func newH(p *fakePlacer, c *fakeCart) http.Handler {
	r := newRouterForTest()
	(&Handler{Placer: p, Cart: c}).Routes(r)
	return r
}

func TestSuccess201AndCartCleared(t *testing.T) {
	p, c := &fakePlacer{order: "ord-1", total: 1500}, &fakeCart{items: []Item{{ProductID: 1, Qty: 2}}}
	rec := post(newH(p, c), `{"session_id":"abc"}`)
	if rec.Code != 201 || !strings.Contains(rec.Body.String(), `"order_id":"ord-1"`) || !strings.Contains(rec.Body.String(), `"total_cents":1500`) {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	if !c.cleared {
		t.Fatal("cart not cleared after successful order")
	}
}

func TestEmptyCart400(t *testing.T) {
	rec := post(newH(&fakePlacer{}, &fakeCart{}), `{"session_id":"abc"}`)
	if rec.Code != 400 {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestBadBodyAndSession400(t *testing.T) {
	for _, b := range []string{`{bad`, `{"session_id":""}`, `{"session_id":"has space"}`} {
		if rec := post(newH(&fakePlacer{}, &fakeCart{}), b); rec.Code != 400 {
			t.Fatalf("%s = %d, want 400", b, rec.Code)
		}
	}
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{ErrInsufficientStock, 409},
		{ErrUnknownProduct, 409},
		{ErrPayment, 502},
		{errors.New("db exploded"), 500},
	}
	for _, tc := range cases {
		c := &fakeCart{items: []Item{{ProductID: 1, Qty: 1}}}
		rec := post(newH(&fakePlacer{err: tc.err}, c), `{"session_id":"abc"}`)
		if rec.Code != tc.want {
			t.Fatalf("%v = %d, want %d", tc.err, rec.Code, tc.want)
		}
		if c.cleared {
			t.Fatalf("%v: cart cleared although order failed", tc.err)
		}
	}
}

func TestCartServiceDown502(t *testing.T) {
	rec := post(newH(&fakePlacer{}, &fakeCart{err: errors.New("cart down")}), `{"session_id":"abc"}`)
	if rec.Code != 502 {
		t.Fatalf("code = %d, want 502", rec.Code)
	}
}
