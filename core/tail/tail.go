// Package tail reads newly appended complete lines from growing files.
// It extends the scanner's fingerprint-cache idea with byte offsets so large
// JSONL transcripts are parsed incrementally instead of re-read every scan.
package tail

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"
)

const DefaultMaxPartial = 1 << 20

type Tailer struct {
	// MaxPartial caps how long a line without a newline may grow before the
	// tailer gives up on it and skips to the next newline.
	MaxPartial int
	states     map[string]*fileState
}

type fileState struct {
	offset   int64 // committed through the last complete newline
	size     int64
	modTime  time.Time
	inode    uint64
	skipping bool // discarding an oversized line until its newline arrives
}

func New() *Tailer {
	return &Tailer{MaxPartial: DefaultMaxPartial, states: map[string]*fileState{}}
}

// Lines returns the complete lines appended to path since the previous call.
// Bytes after the last newline are left in the file and re-read once the
// writer completes the line; they are never emitted early.
func (t *Tailer) Lines(path string) ([][]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		delete(t.states, path)
		return nil, err
	}
	inode := inodeOf(info)
	state, ok := t.states[path]
	if !ok {
		state = &fileState{}
		t.states[path] = state
	} else if state.inode != inode || info.Size() < state.offset {
		// Rotation or truncation: start over from the top of the new content.
		*state = fileState{}
	} else if info.Size() == state.size && info.ModTime().Equal(state.modTime) {
		return nil, nil
	}
	state.inode = inode
	state.size = info.Size()
	state.modTime = info.ModTime()
	return t.consume(path, state)
}

// TailFrom begins tailing path from at most lastN bytes before its end,
// discarding the first (likely partial) line, so cold starts on large
// historical files skip their history. Later appends flow through Lines.
func (t *Tailer) TailFrom(path string, lastN int64) ([][]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	state := &fileState{inode: inodeOf(info), size: info.Size(), modTime: info.ModTime()}
	if info.Size() > lastN {
		state.offset = info.Size() - lastN
		state.skipping = state.offset > 0
	}
	t.states[path] = state
	return t.consume(path, state)
}

// Prune drops tail state for files absent from seen.
func (t *Tailer) Prune(seen map[string]bool) {
	for path := range t.states {
		if !seen[path] {
			delete(t.states, path)
		}
	}
}

func (t *Tailer) consume(path string, state *fileState) ([][]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		delete(t.states, path)
		return nil, err
	}
	defer file.Close()
	if _, err := file.Seek(state.offset, io.SeekStart); err != nil {
		delete(t.states, path)
		return nil, err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		delete(t.states, path)
		return nil, err
	}
	if state.skipping {
		newline := bytes.IndexByte(data, '\n')
		if newline < 0 {
			state.offset += int64(len(data))
			return nil, nil
		}
		state.offset += int64(newline + 1)
		state.skipping = false
		data = data[newline+1:]
	}
	var lines [][]byte
	for {
		newline := bytes.IndexByte(data, '\n')
		if newline < 0 {
			break
		}
		line := make([]byte, newline)
		copy(line, data[:newline])
		lines = append(lines, line)
		state.offset += int64(newline + 1)
		data = data[newline+1:]
	}
	if len(data) > t.maxPartial() {
		// The unfinished line is already too long to ever emit; skip it.
		state.offset += int64(len(data))
		state.skipping = true
		return lines, fmt.Errorf("%s: dropped line longer than %d bytes", path, t.maxPartial())
	}
	return lines, nil
}

func (t *Tailer) maxPartial() int {
	if t.MaxPartial > 0 {
		return t.MaxPartial
	}
	return DefaultMaxPartial
}

func inodeOf(info os.FileInfo) uint64 {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return stat.Ino
	}
	return 0
}
