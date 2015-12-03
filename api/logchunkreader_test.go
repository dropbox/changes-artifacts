package api

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/dropbox/changes-artifacts/database"
	"github.com/dropbox/changes-artifacts/model"
)

func makeChunks(offset int, chunks ...string) []model.LogChunk {
	logChunks := make([]model.LogChunk, len(chunks))

	for i, chunk := range chunks {
		logChunks[i] = model.LogChunk{ByteOffset: int64(offset), Size: int64(len(chunk)), ContentBytes: []byte(chunk)}
		offset += len(chunk)
	}

	return logChunks
}

func TestLogChunkReaderSeek(t *testing.T) {
	type testcase struct {
		offset         int
		whence         int
		expectError    bool
		expectedOffset int
	}

	cases := []testcase{
		{offset: 1, whence: os.SEEK_SET, expectError: false, expectedOffset: 1},
		{offset: 1025, whence: os.SEEK_CUR, expectError: false, expectedOffset: 1025},
		{offset: 5, whence: os.SEEK_CUR, expectError: false, expectedOffset: 5},
		{offset: -10, whence: os.SEEK_END, expectError: false, expectedOffset: 1015},
		{offset: -1, whence: os.SEEK_SET, expectError: true, expectedOffset: 0},
		{offset: 1026, whence: os.SEEK_CUR, expectError: true, expectedOffset: 0},
		{offset: 1, whence: os.SEEK_END, expectError: true, expectedOffset: 0},
	}

	for _, c := range cases {
		lcr := newLogChunkReader(&model.Artifact{Size: 1025}, nil)

		n, err := lcr.Seek(int64(c.offset), c.whence)
		if n != int64(c.expectedOffset) {
			t.Errorf("After Seek(%d, %d), expected offset: %d, actual offset: %d", c.offset, c.whence, c.expectedOffset, n)
		}
		if c.expectError {
			if err == nil {
				t.Errorf("Expected error during Seek(%d, %d), none received", c.offset, c.whence)
			}
		} else if err != nil {
			t.Errorf("Unexpected error during Seek(%d, %d): %s", c.offset, c.whence, err)
		}
	}
}

func TestLogChunkReaderRead(t *testing.T) {
	type mockDBCall struct {
		offset    int
		limit     int
		logChunks []model.LogChunk
	}

	type unit struct {
		readBufLen  int
		expectEOF   bool
		expectError bool
		expectBytes string

		expectDBCall *mockDBCall
	}

	type testcase struct {
		artifactSize int64
		units        []unit
	}

	cases := []testcase{
		{
			artifactSize: 5,
			units: []unit{
				{readBufLen: 0, expectError: false}, // Empty p
				{readBufLen: 1, expectBytes: "0", expectError: false, expectDBCall: &mockDBCall{offset: 0, limit: 5, logChunks: makeChunks(0, "01234")}},
				{readBufLen: 3, expectBytes: "123", expectError: false}, // Read from cache
				{readBufLen: 2, expectBytes: "4", expectEOF: true},      // Read from cache and truncate p
				{readBufLen: 1, expectEOF: true},                        // Read at EOF
			},
		},
		{
			artifactSize: 5,
			units: []unit{
				{readBufLen: 5, expectBytes: "01234", expectEOF: true, expectDBCall: &mockDBCall{offset: 0, limit: 5, logChunks: makeChunks(0, "01234")}},
			},
		},
		{
			artifactSize: 5,
			units: []unit{
				{readBufLen: 3, expectBytes: "012", expectError: false, expectDBCall: &mockDBCall{offset: 0, limit: 5, logChunks: makeChunks(0, "01234")}},
				{readBufLen: 2, expectBytes: "34", expectEOF: true}, // Read from cache till EOF
			},
		},
		{
			artifactSize: 5,
			units: []unit{
				{readBufLen: 2, expectBytes: "01", expectError: false, expectDBCall: &mockDBCall{offset: 0, limit: 5, logChunks: makeChunks(0, "012")}},
				{readBufLen: 4, expectBytes: "234", expectEOF: true, expectDBCall: &mockDBCall{offset: 3, limit: 5, logChunks: makeChunks(3, "34")}},
			},
		},
		{
			artifactSize: 5,
			units: []unit{
				{readBufLen: 2, expectBytes: "01", expectError: false, expectDBCall: &mockDBCall{offset: 0, limit: 5, logChunks: makeChunks(0, "01", "23")}},
				{readBufLen: 4, expectBytes: "234", expectEOF: true, expectDBCall: &mockDBCall{offset: 4, limit: 5, logChunks: makeChunks(4, "4")}},
			},
		},
	}

	for _, c := range cases {
		mockdb := &database.MockDatabase{}
		lcr := newLogChunkReaderWithReadahead(&model.Artifact{Size: c.artifactSize, Id: 12345}, mockdb)
		for _, unit := range c.units {
			if unit.expectDBCall != nil {
				mockdb.On("ListLogChunksInArtifact", int64(12345), int64(unit.expectDBCall.offset), int64(unit.expectDBCall.limit)).Return(unit.expectDBCall.logChunks, nil).Once()
			}

			p := make([]byte, unit.readBufLen)

			n, err := lcr.Read(p)
			if n != len(unit.expectBytes) {
				t.Errorf("Mismatch in number of bytes read during Read(%d): Expected: %d, Actual: %d", unit.readBufLen, len(unit.expectBytes), n)
			}
			if !bytes.Equal([]byte(unit.expectBytes), p[:n]) {
				t.Errorf("Mismatch in content read during Read(%d): Expected: '%s', Actual: '%s'", unit.readBufLen, unit.expectBytes, p[:n])
			}

			if unit.expectEOF {
				if err != io.EOF {
					t.Errorf("Expected EOF during Read(%d), none received", unit.readBufLen)
				}
			} else if unit.expectError {
				if err != nil {
					t.Errorf("Expected error during Read(%d), none received", unit.readBufLen)
				}
			} else if err != nil {
				t.Errorf("Unexpected error during Read(%d): %s", unit.readBufLen, err)
			}

			mockdb.AssertExpectations(t)
		}
	}
}

func TestLogChunkReaderCacheInvalidation(t *testing.T) {
	mockdb := &database.MockDatabase{}
	lcr := newLogChunkReader(&model.Artifact{Id: 123, Size: 11}, mockdb)

	mockdb.On("ListLogChunksInArtifact", int64(123), int64(0), int64(7)).Return(makeChunks(0, "012345", "67891"), nil).Once()
	bts := make([]byte, 7)
	lcr.Read(bts)
	mockdb.AssertExpectations(t)

	// No-op seek
	lcr.Seek(0, os.SEEK_CUR)
	if lcr.unread.Len() != 4 {
		t.Errorf("Cache expected to be 4 bytes, but is of size %d bytes", lcr.unread.Len())
	}

	// Seek 1 byte ahead, advance cache
	lcr.Seek(1, os.SEEK_CUR)
	if lcr.unread.Len() != 3 {
		t.Errorf("Cache expected to be 3 bytes, but is of size %d bytes", lcr.unread.Len())
	}

	// Seek 1 byte behind, invalidate cache
	lcr.Seek(-1, os.SEEK_CUR)
	if lcr.unread.Len() != 0 {
		t.Errorf("Cache expected to be empty, but is of size %d bytes", lcr.unread.Len())
	}

	lcr.Seek(0, os.SEEK_SET)
	mockdb.On("ListLogChunksInArtifact", int64(123), int64(0), int64(7)).Return(makeChunks(0, "012345", "678"), nil).Once()
	lcr.Read(bts)
	mockdb.AssertExpectations(t)

	// Seek beyond end of cache, invalidate cache
	lcr.Seek(3, os.SEEK_CUR)
	if lcr.unread.Len() != 0 {
		t.Errorf("Cache expected to be empty, but is of size %d bytes", lcr.unread.Len())
	}
}

func TestGetByteRangeFromRequest(t *testing.T) {
	type testcase struct {
		query         string
		artifactSize  int
		expectedBegin int
		expectedEnd   int
		expectError   bool
	}
	cases := []testcase{
		{query: "/chunked", artifactSize: 0, expectedBegin: 0, expectedEnd: 0, expectError: true},
		{query: "/chunked", artifactSize: 100, expectedBegin: 0, expectedEnd: 99, expectError: false},
		{query: "/chunked?limit=0", artifactSize: 100, expectedBegin: 0, expectedEnd: 99, expectError: false},
		{query: "/chunked?limit=5", artifactSize: 100, expectedBegin: 0, expectedEnd: 4, expectError: false},
		{query: "/chunked?offset=0", artifactSize: 100, expectedBegin: 0, expectedEnd: 99, expectError: false},
		{query: "/chunked?offset=0&limit=0", artifactSize: 100, expectedBegin: 0, expectedEnd: 99, expectError: false},
		{query: "/chunked?offset=0&limit=11", artifactSize: 100, expectedBegin: 0, expectedEnd: 10, expectError: false},
		{query: "/chunked?offset=4&limit=0", artifactSize: 100, expectedBegin: 4, expectedEnd: 99, expectError: false},
		{query: "/chunked?offset=4&limit=20", artifactSize: 100, expectedBegin: 4, expectedEnd: 23, expectError: false},
		{query: "/chunked?offset=85&limit=20", artifactSize: 100, expectedBegin: 85, expectedEnd: 99, expectError: false},
		{query: "/chunked?offset=85&limit=20", artifactSize: 30, expectedBegin: 0, expectedEnd: 30, expectError: true},
		{query: "/chunked?offset=-1&limit=20", artifactSize: 100, expectedBegin: 0, expectedEnd: 19, expectError: false},
		{query: "/chunked?offset=0&limit=-1", artifactSize: 100, expectedBegin: 0, expectedEnd: 99, expectError: false},
		{query: "/chunked?offset=99", artifactSize: 100, expectedBegin: 99, expectedEnd: 99, expectError: false},
		{query: "/chunked", artifactSize: 2000000, expectedBegin: 0, expectedEnd: 99999, expectError: false},
		{query: "/chunked?limit=2000000", artifactSize: 2000000, expectedBegin: 0, expectedEnd: 99999, expectError: false},
	}

	for _, c := range cases {
		artifact := &model.Artifact{Size: int64(c.artifactSize)}
		req, _ := http.NewRequest("GET", c.query, nil)

		begin, end, err := getByteRangeFromRequest(req, artifact)
		if int(begin) != c.expectedBegin || int(end) != c.expectedEnd {
			t.Errorf("Byte range mismatch for request %s => Expected: Begin=%d End=%d, Actual: Begin=%d End=%d", c.query, c.expectedBegin, c.expectedEnd, begin, end)
		}

		if c.expectError && err == nil {
			t.Errorf("Expected error for request %s, but got none", c.query)
		}

		if !c.expectError && err != nil {
			t.Errorf("Expected no error for request %s, but got %s", c.query, err)
		}
	}
}
