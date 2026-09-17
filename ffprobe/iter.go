package ffprobe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"strings"
)

// Frames streams decoded frames without holding the whole listing in memory.
// It runs ffprobe -show_frames and yields each frame as it is printed;
// iteration stops early when the consumer breaks out of the loop, which kills
// ffprobe. The final yield carries any error ffprobe reported.
//
//	for f, err := range prober.Frames(ctx, path, ffprobe.SelectStreams("v:0")) {
//		if err != nil {
//			return err
//		}
//		if f.KeyFrame.Bool() { ... }
//	}
func (p *Prober) Frames(ctx context.Context, input string, opts ...Option) iter.Seq2[*Frame, error] {
	return func(yield func(*Frame, error) bool) {
		streamSection(ctx, p, input, "frames", append(opts, ShowFrames()), func(dec *json.Decoder) (bool, error) {
			var f Frame
			if err := dec.Decode(&f); err != nil {
				return false, err
			}
			return yield(&f, nil), nil
		}, func(err error) { yield(nil, err) })
	}
}

// Packets streams packets the way Frames streams frames.
func (p *Prober) Packets(ctx context.Context, input string, opts ...Option) iter.Seq2[*Packet, error] {
	return func(yield func(*Packet, error) bool) {
		streamSection(ctx, p, input, "packets", append(opts, ShowPackets()), func(dec *json.Decoder) (bool, error) {
			var pk Packet
			if err := dec.Decode(&pk); err != nil {
				return false, err
			}
			return yield(&pk, nil), nil
		}, func(err error) { yield(nil, err) })
	}
}

// PacketsAndFrames streams the interleaved packet and frame listing that
// ffprobe produces when both sections are requested.
func (p *Prober) PacketsAndFrames(ctx context.Context, input string, opts ...Option) iter.Seq2[*PacketOrFrame, error] {
	return func(yield func(*PacketOrFrame, error) bool) {
		streamSection(ctx, p, input, "packets_and_frames", append(opts, ShowPackets(), ShowFrames()), func(dec *json.Decoder) (bool, error) {
			var pf PacketOrFrame
			if err := dec.Decode(&pf); err != nil {
				return false, err
			}
			return yield(&pf, nil), nil
		}, func(err error) { yield(nil, err) })
	}
}

// Frames streams decoded frames using the default Prober.
func Frames(ctx context.Context, input string, opts ...Option) iter.Seq2[*Frame, error] {
	return Default.Frames(ctx, input, opts...)
}

// Packets streams packets using the default Prober.
func Packets(ctx context.Context, input string, opts ...Option) iter.Seq2[*Packet, error] {
	return Default.Packets(ctx, input, opts...)
}

// PacketsAndFrames streams the interleaved listing using the default Prober.
func PacketsAndFrames(ctx context.Context, input string, opts ...Option) iter.Seq2[*PacketOrFrame, error] {
	return Default.PacketsAndFrames(ctx, input, opts...)
}

// streamSection runs ffprobe and walks its top-level JSON object. Items of
// the array named key are handed to item; every other top-level member is
// decoded so an "error" section can be surfaced.
func streamSection(ctx context.Context, p *Prober, input, key string, opts []Option,
	item func(*json.Decoder) (bool, error), fail func(error)) {
	var c config
	c.apply(opts)
	args := p.buildArgs(&c, input)

	run := p.newRun(ctx, &c, input)
	defer run.cancel()

	cmd := p.command(run.ctx, args, &c)
	var stderr bytes.Buffer
	cmd.Stderr = p.stderrWriter(&stderr)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fail(err)
		return
	}
	if err := cmd.Start(); err != nil {
		if ctxErr := run.err(); ctxErr != nil {
			fail(ctxErr)
			return
		}
		fail(p.startError(err))
		return
	}

	// Once the consumer stops we cancel ffprobe and drain so Wait returns.
	stopped := false
	finish := func() error {
		if stopped {
			run.cancel()
			_, _ = io.Copy(io.Discard, stdout)
		}
		waitErr := cmd.Wait()
		if stopped {
			return nil
		}
		if waitErr != nil && run.ctx.Err() == nil {
			return &ExitError{ExitCode: cmd.ProcessState.ExitCode(), Stderr: strings.TrimSpace(stderr.String()), Args: args, Cause: waitErr}
		}
		return nil
	}

	dec := json.NewDecoder(stdout)
	var probeErr *ProbeError
	err = walkTopLevel(dec, func(name string) error {
		switch name {
		case key:
			if _, err := dec.Token(); err != nil { // opening '['
				return err
			}
			for dec.More() {
				more, err := item(dec)
				if err != nil {
					return err
				}
				if !more {
					stopped = true
					return errStopped
				}
			}
			_, err := dec.Token() // closing ']'
			return err
		case "error":
			var pe ProbeError
			if err := dec.Decode(&pe); err != nil {
				return err
			}
			probeErr = &pe
			return nil
		default:
			var skip json.RawMessage
			return dec.Decode(&skip)
		}
	})
	if err == errStopped {
		_ = finish()
		return
	}
	waitErr := finish()
	switch {
	case probeErr != nil:
		fail(probeErr)
	case err != nil && run.ctx.Err() != nil:
		fail(run.err())
	case err != nil:
		fail(fmt.Errorf("ffprobe: decode output: %w", err))
	case waitErr != nil:
		fail(waitErr)
	}
}

var errStopped = fmt.Errorf("consumer stopped")

// walkTopLevel reads a JSON object and calls member for each key, leaving
// the decoder positioned at the member's value.
func walkTopLevel(dec *json.Decoder, member func(name string) error) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("expected object, got %v", tok)
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		name, ok := tok.(string)
		if !ok {
			return fmt.Errorf("expected key, got %v", tok)
		}
		if err := member(name); err != nil {
			return err
		}
	}
	_, err = dec.Token() // closing '}'
	return err
}
