package ffmpeg

import (
	"strings"
	"testing"
)

func TestCapture(t *testing.T) {
	t.Run("under the limit keeps everything", func(t *testing.T) {
		c := capture{Limit: 16}
		c.Write([]byte("abc"))
		c.Write([]byte("def"))
		if string(c.Bytes()) != "abcdef" || c.Dropped() != 0 {
			t.Errorf("bytes=%q dropped=%d", c.Bytes(), c.Dropped())
		}
	})

	t.Run("over the limit keeps the tail", func(t *testing.T) {
		c := capture{Limit: 4}
		c.Write([]byte("abc"))
		c.Write([]byte("def"))
		if string(c.Bytes()) != "cdef" || c.Dropped() != 2 {
			t.Errorf("bytes=%q dropped=%d", c.Bytes(), c.Dropped())
		}
	})

	t.Run("one write larger than the limit", func(t *testing.T) {
		c := capture{Limit: 4}
		c.Write([]byte("ab"))
		c.Write([]byte("0123456789"))
		if string(c.Bytes()) != "6789" || c.Dropped() != 8 {
			t.Errorf("bytes=%q dropped=%d", c.Bytes(), c.Dropped())
		}
	})

	t.Run("no limit", func(t *testing.T) {
		c := capture{}
		for range 100 {
			c.Write([]byte("xxxxxxxxxx"))
		}
		if len(c.Bytes()) != 1000 || c.Dropped() != 0 {
			t.Errorf("len=%d dropped=%d", len(c.Bytes()), c.Dropped())
		}
	})

	t.Run("total written is never short", func(t *testing.T) {
		c := capture{Limit: 4}
		n, err := c.Write([]byte("0123456789"))
		if n != 10 || err != nil {
			t.Errorf("Write = %d, %v", n, err)
		}
	})

	t.Run("stays bounded over many writes", func(t *testing.T) {
		c := capture{Limit: 64}
		line := strings.Repeat("y", 40) + "\n"
		for range 1000 {
			c.Write([]byte(line))
		}
		if len(c.Bytes()) != 64 || cap(c.Bytes()) > 4*64 {
			t.Errorf("len=%d cap=%d", len(c.Bytes()), cap(c.Bytes()))
		}
		if want := int64(1000*len(line)) - 64; c.Dropped() != want {
			t.Errorf("dropped = %d, want %d", c.Dropped(), want)
		}
	})
}
