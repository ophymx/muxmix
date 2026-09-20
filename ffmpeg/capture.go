package ffmpeg

// DefaultCaptureLimit is how many bytes of ffmpeg's log Result.Stderr keeps
// by default. It is far more than a failing run produces and bounds one
// that never ends: a live push logs a stats line every half second, which
// would otherwise grow the capture for as long as the stream runs.
const DefaultCaptureLimit = 1 << 20 // 1 MiB

// capture accumulates output, keeping only the last Limit bytes. ffmpeg
// puts what matters at the end — the error message, the final stats line —
// so the tail is the half worth keeping. A Limit of zero or less keeps
// everything.
//
// One goroutine writes to a capture (the stderr copier, or the stats
// reader), and the run reads Bytes only after that goroutine has finished,
// so it needs no lock.
type capture struct {
	Limit   int
	buf     []byte
	dropped int64
}

// Write never fails and never reports a short write, so it does not break
// the io.MultiWriter it sits in when the limit is reached.
func (c *capture) Write(p []byte) (int, error) {
	n := len(p)
	switch {
	case c.Limit <= 0:
		c.buf = append(c.buf, p...)
	case n >= c.Limit:
		// This write alone overflows: keep its tail and drop the rest.
		c.dropped += int64(len(c.buf)) + int64(n-c.Limit)
		c.buf = append(c.buf[:0], p[n-c.Limit:]...)
	default:
		if over := len(c.buf) + n - c.Limit; over > 0 {
			c.dropped += int64(over)
			c.buf = append(c.buf[:0], c.buf[over:]...)
		}
		c.buf = append(c.buf, p...)
	}
	return n, nil
}

// Bytes returns what was kept.
func (c *capture) Bytes() []byte { return c.buf }

// Dropped returns how many bytes fell off the front.
func (c *capture) Dropped() int64 { return c.dropped }
