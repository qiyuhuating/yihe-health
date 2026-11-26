package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync"

	"yihe-care/internal/health"
)

const healthBodyBufferSize = 128 << 10
const healthBodyBufferStep = 32 << 10

type healthBodyBuffer struct{ data []byte }

// Retain at most sixteen cleared buffers per size tier across collections.
// Overflow drops the buffer; acquisition never waits for another request.
type healthBodyPool struct {
	once sync.Once
	idle chan *healthBodyBuffer
}

func (p *healthBodyPool) init() { p.once.Do(func() { p.idle = make(chan *healthBodyBuffer, 16) }) }
func (p *healthBodyPool) Get() *healthBodyBuffer {
	p.init()
	select {
	case b := <-p.idle:
		return b
	default:
		return nil
	}
}
func (p *healthBodyPool) Put(b *healthBodyBuffer) {
	p.init()
	select {
	case p.idle <- b:
	default:
	}
}

var healthBodyBuffers [healthBodyBufferSize / healthBodyBufferStep]healthBodyPool

// Bounded, known-size uploads reuse scratch bytes without retaining credentials.
// The handler supplies a new request value. Error paths replay the original
// decoder to preserve first-document versus trailing-data errors and mutations.
func decodeHealthBatch(w http.ResponseWriter, r *http.Request, req *healthBatchRequest) bool {
	if r.ContentLength < 16<<10 || r.ContentLength > healthBodyBufferSize {
		return decodeHealth(w, r, req)
	}
	index := int((r.ContentLength - 1) / healthBodyBufferStep)
	pool := &healthBodyBuffers[index]
	limit := (index + 1) * healthBodyBufferStep
	scratch := pool.Get()
	if scratch == nil {
		scratch = &healthBodyBuffer{data: make([]byte, 0, limit+512)}
	}
	defer func() {
		clear(scratch.data[:cap(scratch.data)])
		pool.Put(scratch)
	}()
	body := http.MaxBytesReader(w, r.Body, 2<<20)
	defer body.Close()
	buffer := bytes.NewBuffer(scratch.data[:0])
	// Do not trust ContentLength to bound allocations. Larger bodies stream
	// through the original decoder without growing or abandoning scratch bytes.
	_, readErr := buffer.ReadFrom(io.LimitReader(body, int64(limit)))
	raw := buffer.Bytes()
	var parsed healthBatchRequest
	if len(raw) < limit && readErr == nil {
		// Size is only an allocation hint, never the accepted sample count.
		parsed.Samples = make([]health.Sample, 0, min(200, max(1, len(raw)/320)))
		if json.Unmarshal(raw, &parsed) == nil {
			// An omitted field keeps our capacity; [] resets it to zero and
			// null sets nil. Preserve all three original decoding outcomes.
			if len(parsed.Samples) == 0 && cap(parsed.Samples) > 0 {
				parsed.Samples = nil
			}
			*req = parsed
			return true
		}
	}
	replay := *r
	var remainder io.Reader = healthBodyReadError{readErr}
	if len(raw) == limit && readErr == nil {
		remainder = body
	}
	replay.Body = io.NopCloser(io.MultiReader(bytes.NewReader(raw), remainder))
	return decodeHealth(w, &replay, req)
}

type healthBodyReadError struct{ err error }

func (r healthBodyReadError) Read([]byte) (int, error) {
	if r.err == nil {
		return 0, io.EOF
	}
	return 0, r.err
}
