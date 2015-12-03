package api

import (
	"bytes"
	"errors"
	"io"
	"os"
	"unicode/utf8"

	"github.com/dropbox/changes-artifacts/database"
	"github.com/dropbox/changes-artifacts/model"
)

// Number of bytes to read ahead while making DB queries.
const ReadaheadBytes = 1 * 1024 * 1024

// logChunkReader presents an io.ReadSeeker interface to sequentially reading logchunks for an
// artifact from the database. It stores any un-Read() bytes in an internal buffer to avoid reading
// logchunks multiple times from the database. Also, an optional readahead size can be specified
// while reading large byte ranges (or the entire artifact) to prepopulate the cache using lesser
// number of DB queries (instead of many short queries, each of which has a non-trivial overhead on
// the DB).
type logChunkReader struct {
	artifact  *model.Artifact
	db        database.Database
	offset    int64
	readAhead int64
	unread    bytes.Buffer // Readahead buffer of bytes which were read from DB but not yet read by client.
	// This can be replaced with a global cache because logchunks are immutable to reduce even more
	// hits to the DB.
}

func newLogChunkReader(artifact *model.Artifact, db database.Database) *logChunkReader {
	return &logChunkReader{artifact: artifact, db: db}
}

func newLogChunkReaderWithReadahead(artifact *model.Artifact, db database.Database) *logChunkReader {
	return &logChunkReader{artifact: artifact, db: db, readAhead: min(artifact.Size, ReadaheadBytes)}
}

func (lcr *logChunkReader) Read(p []byte) (int, error) {
	offsetInSlice := int64(0)
	if len(p) == 0 {
		return 0, nil
	}

	if lcr.unread.Len() > 0 {
		n, err := lcr.unread.Read(p)
		offsetInSlice += int64(n)
		lcr.offset += int64(n)

		if err != nil {
			return n, err
		} else {
			// If we've already filled buffer, return
			if n == len(p) {
				if lcr.offset == lcr.artifact.Size {
					return n, io.EOF
				}
				return n, nil
			}
		}
	}

	if lcr.offset < 0 || lcr.offset >= lcr.artifact.Size {
		return int(offsetInSlice), io.EOF
	}

	bytesToRead := min(lcr.artifact.Size-lcr.offset, int64(len(p))-offsetInSlice)
	if bytesToRead+offsetInSlice != int64(len(p)) {
		p = p[0 : bytesToRead+offsetInSlice]
	}

	chunks, err := lcr.db.ListLogChunksInArtifact(lcr.artifact.Id, lcr.offset, min(lcr.artifact.Size, lcr.offset+max(bytesToRead, lcr.readAhead)))
	// TODO: Should we retry? This appears to be the best place to do so.
	if err != nil {
		return int(offsetInSlice), err
	}

	for _, chunk := range chunks {
		if bytesToRead == 0 {
			lcr.unread.Write(chunk.ContentBytes)
		}
		if chunk.ByteOffset < lcr.offset+bytesToRead && chunk.ByteOffset+chunk.Size > lcr.offset {
			startByte := lcr.offset - chunk.ByteOffset
			bytesToReadFromChunk := min(bytesToRead, chunk.Size-startByte)

			copy(p[offsetInSlice:], chunk.ContentBytes[startByte:startByte+bytesToReadFromChunk])
			lcr.offset += bytesToReadFromChunk
			offsetInSlice += bytesToReadFromChunk
			bytesToRead -= bytesToReadFromChunk

			if bytesToReadFromChunk < chunk.Size-startByte {
				lcr.unread.Write(chunk.ContentBytes[startByte+bytesToReadFromChunk:])
			}
		}
	}

	if lcr.offset == lcr.artifact.Size {
		return len(p), io.EOF
	}
	return len(p), nil
}

var errInvalidSeek = errors.New("Invalid seek target")

func (lcr *logChunkReader) setOffset(offset int64) (int64, error) {
	if offset >= 0 && offset <= lcr.artifact.Size {
		delta := int(offset - lcr.offset)
		if delta >= 0 && delta < lcr.unread.Len() {
			lcr.unread.Next(delta)
		} else {
			lcr.unread.Reset()
		}
		lcr.offset = offset
		return lcr.offset, nil
	}

	return lcr.offset, errInvalidSeek
}

func (lcr *logChunkReader) Seek(offset int64, whence int) (int64, error) {
	if whence == os.SEEK_SET {
		// Seek WRT start of file
		return lcr.setOffset(offset)
	}

	if whence == os.SEEK_CUR {
		// Seek WRT current offset
		return lcr.setOffset(lcr.offset + offset)
	}

	return lcr.setOffset(lcr.artifact.Size + offset)
}

var _ io.ReadSeeker = (*logChunkReader)(nil)

var errInvalidStartingRune = errors.New("Read() started in the middle of a rune")

// Read valid UTF-8 content from provided io.Reader.
// If underlying reader starts in the middle of a rune, an error is returned.
// If reader ends in the middle of a rune, the last (invalid) rune is discarded. Note that the
// underlying reader will now start reading from the middle of a rune.
func runeLimitedRead(r io.Reader, p []byte) (int, error) {
	n, err := r.Read(p)
	if n == 0 {
		return n, err
	}

	// If first byte is not a valid rune starting byte, returned error
	if n > 0 && !utf8.RuneStart(p[0]) {
		return 0, errInvalidStartingRune
	}

	// The following code is a lightly modified version of utf8#Valid()
	for i := 0; i < n; {
		if p[i] < utf8.RuneSelf {
			// Skip single byte rune
			i++
			continue
		}

		r, size := utf8.DecodeRune(p[i:])
		if size == 1 && r == utf8.RuneError {
			return i, err
		}
		i += size
	}

	return n, err
}
