package catalog

import (
	"bytes"
	"container/list"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strconv"
	"sync"
)

// ErrBadWidth is returned for thumbnail widths outside 16..2048.
var ErrBadWidth = errors.New("width must be between 16 and 2048")

const (
	minWidth = 16
	maxWidth = 2048
)

// Thumbs generates and caches PNG thumbnails. With maxMB == 0 the cache has
// no byte limit; otherwise least-recently-used entries are evicted.
type Thumbs struct {
	maxBytes int64
	burst    func() bool

	mu    sync.Mutex
	items map[string]*list.Element
	order *list.List // front = most recent
	bytes int64
	seq   int64
}

type entry struct {
	key  string
	data []byte
}

// NewThumbs builds a cache. burst, when it returns true, makes every request
// use a fresh cache key.
func NewThumbs(maxMB int, burst func() bool) *Thumbs {
	return &Thumbs{
		maxBytes: int64(maxMB) << 20,
		burst:    burst,
		items:    map[string]*list.Element{},
		order:    list.New(),
	}
}

// Bytes reports the current size of cached image data.
func (t *Thumbs) Bytes() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.bytes
}

// Get returns the PNG thumbnail for product id at width w.
func (t *Thumbs) Get(id, w int) ([]byte, error) {
	if w < minWidth || w > maxWidth {
		return nil, ErrBadWidth
	}
	key := strconv.Itoa(id) + ":" + strconv.Itoa(w)
	t.mu.Lock()
	if t.burst != nil && t.burst() {
		t.seq++
		key += ":" + strconv.FormatInt(t.seq, 10)
	}
	if el, ok := t.items[key]; ok {
		t.order.MoveToFront(el)
		data := el.Value.(*entry).data
		t.mu.Unlock()
		return data, nil
	}
	t.mu.Unlock()

	data, err := render(id, w)
	if err != nil {
		return nil, err
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.items[key]; ok {
		return data, nil
	}
	if t.maxBytes > 0 && int64(len(data)) > t.maxBytes {
		return data, nil // too large to cache at all
	}
	t.items[key] = t.order.PushFront(&entry{key: key, data: data})
	t.bytes += int64(len(data))
	for t.maxBytes > 0 && t.bytes > t.maxBytes {
		last := t.order.Back()
		e := last.Value.(*entry)
		t.order.Remove(last)
		delete(t.items, e.key)
		t.bytes -= int64(len(e.data))
	}
	return data, nil
}

// render draws a deterministic 4:3 pattern seeded by the product id.
func render(id, w int) ([]byte, error) {
	h := w * 3 / 4
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	state := uint32(id&0x7fffffff)*2654435761 + 1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			state = state*1664525 + 1013904223
			n := uint8(state >> 29) // low-amplitude noise keeps PNGs from collapsing
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(x*255/w&0xff) + n, G: uint8(y*255/h&0xff) + n, B: uint8(id*37&0xff) + n, A: 255,
			})
		}
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
