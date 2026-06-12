package managed

import (
	"bytes"
	"context"
	"io"
	"os"
	"time"
)

// TranscriptPollInterval controls tail polling frequency. Overridden in tests.
var TranscriptPollInterval = 200 * time.Millisecond

// TailTranscript polls path for appended JSONL lines starting at offset and
// calls emit for each complete line (without trailing newline). Tolerates the
// file not existing yet, and resets to the start if the file shrinks
// (truncate/recreate). Returns when ctx is done.
func TailTranscript(ctx context.Context, path string, offset int64, emit func(string)) {
	var partial bytes.Buffer
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(TranscriptPollInterval):
		}

		fi, err := os.Stat(path)
		if err != nil {
			continue
		}
		if fi.Size() < offset {
			offset = 0
			partial.Reset()
		}
		if fi.Size() == offset {
			continue
		}

		f, err := os.Open(path)
		if err != nil {
			continue
		}
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			f.Close()
			continue
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			continue
		}
		offset += int64(len(data))
		partial.Write(data)

		for {
			idx := bytes.IndexByte(partial.Bytes(), '\n')
			if idx < 0 {
				break
			}
			line := string(partial.Next(idx + 1)[:idx])
			if line != "" {
				emit(line)
			}
		}
	}
}
