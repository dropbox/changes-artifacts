// A REST api for the artifact store, implemented using Martini.
//
// Each "Handle" function acts as a handler for a request and is
// routed with Martini (routing is hanlded elsewhere).
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/net/context"

	"github.com/dropbox/changes-artifacts/common/sentry"
	"github.com/dropbox/changes-artifacts/common/stats"
	"github.com/dropbox/changes-artifacts/database"
	"github.com/dropbox/changes-artifacts/model"
	"github.com/martini-contrib/render"
	"github.com/moshee/airlift/contentdisposition"
	"gopkg.in/amz.v1/s3"
)

const DEFAULT_DEADLINE = 30

// Format string to construct unique artifact names to recover from duplicate artifact name.
const DuplicateArtifactNameFormat = "%s.dup.%s"

// Maximum number of duplicate file name resolution attempts before failing with an internal error.
const MaxDuplicateFileNameResolutionAttempts = 5

// Maximum artifact size => 200 MB
const MaxArtifactSizeBytes = 200 * 1024 * 1024

// Maximum number of bytes to fetch while returning chunked response.
const MaxChunkedRequestBytes = 100000

var bytesUploadedCounter = stats.NewStat("bytes_uploaded")

type createArtifactReq struct {
	Name         string
	Chunked      bool
	Size         int64
	DeadlineMins uint
	RelativePath string
}

type createLogChunkReq struct {
	ByteOffset int64
	Size       int64
	Content    string // DEPRECATED in favor of Bytes
	Bytes      []byte
}

// CreateArtifact creates a new artifact in a open bucket.
//
// If an artifact with the same name already exists in the same bucket, we attempt to rename the
// artifact by adding a suffix.
// If the request specifies a chunked artifact, the size field is ignored and always set to zero.
// If the request is for a streamed artifact, size is mandatory.
// A relative path field may be specified to preserve the original file name and path. If no path is
// specified, the original artifact name is used by default.
func CreateArtifact(req createArtifactReq, bucket *model.Bucket, db database.Database) (*model.Artifact, *HttpError) {
	if len(req.Name) == 0 {
		return nil, NewHttpError(http.StatusBadRequest, "Artifact name not provided")
	}

	if bucket.State != model.OPEN {
		return nil, NewHttpError(http.StatusBadRequest, "Bucket is already closed")
	}

	artifact := new(model.Artifact)

	artifact.Name = req.Name
	artifact.BucketId = bucket.Id
	artifact.DateCreated = time.Now()

	if req.DeadlineMins == 0 {
		artifact.DeadlineMins = DEFAULT_DEADLINE
	} else {
		artifact.DeadlineMins = req.DeadlineMins
	}

	if req.Chunked {
		artifact.State = model.APPENDING
	} else {
		if req.Size == 0 {
			return nil, NewHttpError(http.StatusBadRequest, "Cannot create a new upload artifact without size.")
		} else if req.Size > MaxArtifactSizeBytes {
			return nil, NewHttpError(http.StatusRequestEntityTooLarge, fmt.Sprintf("Entity '%s' (size %d) is too large (limit %d)", req.Name, req.Size, MaxArtifactSizeBytes))
		}
		artifact.Size = req.Size
		artifact.State = model.WAITING_FOR_UPLOAD
	}

	if req.RelativePath == "" {
		// Use artifact name provided as default relativePath
		artifact.RelativePath = req.Name
	} else {
		artifact.RelativePath = req.RelativePath
	}

	// Attempt to insert artifact and retry with a different name if it fails.
	if err := db.InsertArtifact(artifact); err != nil {
		for attempt := 1; attempt <= MaxDuplicateFileNameResolutionAttempts; attempt++ {
			// Unable to create new artifact - if an artifact already exists, the above insert failed
			// because of a collision.
			if _, err := db.GetArtifactByName(bucket.Id, artifact.Name); err != nil {
				// This could be a transient DB error (down/unreachable), in which case we expect the client
				// to retry. There is no value in attempting alternate artifact names.
				//
				// We have no means of verifying there was a name collision - bail with an internal error.
				return nil, NewHttpError(http.StatusInternalServerError, err.Error())
			}

			// File name collision - attempt to resolve
			artifact.Name = fmt.Sprintf(DuplicateArtifactNameFormat, req.Name, randString(5))
			if err := db.InsertArtifact(artifact); err == nil {
				return artifact, nil
			}
		}

		return nil, NewHttpError(http.StatusInternalServerError, "Exceeded retry limit avoiding duplicates")
	}

	return artifact, nil
}

func HandleCreateArtifact(ctx context.Context, r render.Render, req *http.Request, db database.Database, bucket *model.Bucket) {
	if bucket == nil {
		LogAndRespondWithErrorf(ctx, r, http.StatusBadRequest, "No bucket specified")
		return
	}

	var createReq createArtifactReq

	if err := json.NewDecoder(req.Body).Decode(&createReq); err != nil {
		LogAndRespondWithError(ctx, r, http.StatusBadRequest, err)
		return
	}

	if artifact, err := CreateArtifact(createReq, bucket, db); err != nil {
		LogAndRespondWithError(ctx, r, err.errCode, err)
	} else {
		r.JSON(http.StatusOK, artifact)
	}
}

func ListArtifacts(ctx context.Context, r render.Render, req *http.Request, db database.Database, bucket *model.Bucket) {
	if bucket == nil {
		LogAndRespondWithErrorf(ctx, r, http.StatusBadRequest, "No bucket specified")
		return
	}

	artifacts, err := db.ListArtifactsInBucket(bucket.Id)
	if err != nil {
		LogAndRespondWithError(ctx, r, http.StatusInternalServerError, err)
		return
	}

	r.JSON(http.StatusOK, artifacts)
}

func HandleGetArtifact(ctx context.Context, r render.Render, artifact *model.Artifact) {
	if artifact == nil {
		LogAndRespondWithErrorf(ctx, r, http.StatusBadRequest, "No bucket specified")
		return
	}

	r.JSON(http.StatusOK, artifact)
}

// AppendLogChunk appends a logchunk to an artifact.
// If the logchunk position does not match the current end of artifact, an error is returned.
// An exception to this is made when the last seen logchunk is repeated, which is silently ignored
// without an error.
func AppendLogChunk(ctx context.Context, db database.Database, artifact *model.Artifact, logChunkReq *createLogChunkReq) *HttpError {
	if artifact.State != model.APPENDING {
		return NewHttpError(http.StatusBadRequest, fmt.Sprintf("Unexpected artifact state: %s", artifact.State))
	}

	if logChunkReq.Size <= 0 {
		return NewHttpError(http.StatusBadRequest, "Invalid chunk size %d", logChunkReq.Size)
	}

	var contentBytes []byte
	if len(logChunkReq.Bytes) != 0 {
		// If request sent Bytes, use Bytes.
		if int64(len(logChunkReq.Bytes)) != logChunkReq.Size {
			return NewHttpError(http.StatusBadRequest, "Content length %d does not match indicated size %d", len(logChunkReq.Bytes), logChunkReq.Size)
		}
		contentBytes = logChunkReq.Bytes
	} else {
		// Otherwise, allow Content, for now.
		if len(logChunkReq.Content) == 0 {
			return NewHttpError(http.StatusBadRequest, "Empty content string")
		}

		if int64(len(logChunkReq.Content)) != logChunkReq.Size {
			return NewHttpError(http.StatusBadRequest, "Content length %d does not match indicated size %d", len(logChunkReq.Content), logChunkReq.Size)
		}
		contentBytes = []byte(logChunkReq.Content)
	}

	// Find previous chunk in DB - append only
	nextByteOffset := artifact.Size
	if nextByteOffset != logChunkReq.ByteOffset {
		// There is a possibility the previous logchunk is being retried - we need to handle cases where
		// a server/proxy time out caused the client not to get an ACK when it successfully uploaded the
		// previous logchunk, due to which it is retrying.
		//
		// This is a best-effort check - if we encounter DB errors or any mismatch in the chunk
		// contents, we ignore this test and claim that a range mismatch occured.
		if nextByteOffset != 0 && nextByteOffset == logChunkReq.ByteOffset+logChunkReq.Size {
			if prevLogChunk, err := db.GetLastLogChunkSeenForArtifact(artifact.Id); err == nil {
				if prevLogChunk != nil && prevLogChunk.ByteOffset == logChunkReq.ByteOffset && prevLogChunk.Size == logChunkReq.Size && bytes.Equal(prevLogChunk.ContentBytes, contentBytes) {
					sentry.ReportMessage(ctx, fmt.Sprintf("Received duplicate chunk for artifact %v of size %d at byte %d", artifact.Id, logChunkReq.Size, logChunkReq.ByteOffset))
					return nil
				}
			}
		}

		return NewHttpError(http.StatusBadRequest, "Overlapping ranges detected, expected offset: %d, actual offset: %d", nextByteOffset, logChunkReq.ByteOffset)
	}

	// Expand artifact size - redundant after above change.
	if artifact.Size < logChunkReq.ByteOffset+logChunkReq.Size {
		artifact.Size = logChunkReq.ByteOffset + logChunkReq.Size
		if err := db.UpdateArtifact(artifact); err != nil {
			return NewHttpError(http.StatusInternalServerError, err.Error())
		}
	}

	logChunk := &model.LogChunk{
		ArtifactId:   artifact.Id,
		ByteOffset:   logChunkReq.ByteOffset,
		ContentBytes: contentBytes,
		Size:         logChunkReq.Size,
	}

	if err := db.InsertLogChunk(logChunk); err != nil {
		return NewHttpError(http.StatusBadRequest, "Error updating log chunk: %s", err)
	}
	return nil
}

// PostArtifact updates content associated with an artifact.
//
// If the artifact is streamed (uploaded in one shot), PutArtifact is invoked to stream content
// directly through to S3.
//
// If the artifact is chunked (appended chunk by chunk), verify that the position being written to
// matches the current end of artifact, insert a new log chunk at that position and move the end of
// file forward.
func PostArtifact(ctx context.Context, r render.Render, req *http.Request, db database.Database, s3bucket *s3.Bucket, artifact *model.Artifact) {
	if artifact == nil {
		LogAndRespondWithErrorf(ctx, r, http.StatusBadRequest, "No artifact specified")
		return
	}

	switch artifact.State {
	case model.WAITING_FOR_UPLOAD:
		contentLengthStr := req.Header.Get("Content-Length")
		contentLength, err := strconv.ParseInt(contentLengthStr, 10, 64) // string, base, bits
		if err != nil {
			LogAndRespondWithErrorf(ctx, r, http.StatusBadRequest, "Couldn't parse Content-Length as int64")
		} else if contentLength != artifact.Size {
			LogAndRespondWithErrorf(ctx, r, http.StatusBadRequest, "Content-Length does not match artifact size")
		} else if err = PutArtifact(ctx, artifact, db, s3bucket, PutArtifactReq{ContentLength: contentLengthStr, Body: req.Body}); err != nil {
			LogAndRespondWithError(ctx, r, http.StatusInternalServerError, err)
		} else {
			r.JSON(http.StatusOK, artifact)
		}
		return

	case model.UPLOADING:
		LogAndRespondWithErrorf(ctx, r, http.StatusBadRequest, "Artifact is currently being updated")
		return

	case model.UPLOADED:
		LogAndRespondWithErrorf(ctx, r, http.StatusBadRequest, "Artifact already uploaded")
		return

	case model.APPENDING:
		// TODO: Treat contents as a JSON request containing a chunk.
		logChunkReq := new(createLogChunkReq)
		if err := json.NewDecoder(req.Body).Decode(logChunkReq); err != nil {
			LogAndRespondWithError(ctx, r, http.StatusBadRequest, err)
			return
		}

		if err := AppendLogChunk(ctx, db, artifact, logChunkReq); err != nil {
			LogAndRespondWithError(ctx, r, err.errCode, err)
			return
		}

		r.JSON(http.StatusOK, artifact)
		return

	case model.APPEND_COMPLETE:
		LogAndRespondWithErrorf(ctx, r, http.StatusBadRequest, "Artifact is closed for further appends")
		return
	}
}

// CloseArtifact closes an artifact for further writes and begins process of merging and uploading
// the artifact. This operation is only valid for artifacts which are being uploaded in chunks.
// In all other cases, an error is returned.
func CloseArtifact(ctx context.Context, artifact *model.Artifact, db database.Database, s3bucket *s3.Bucket, failIfAlreadyClosed bool) error {
	switch artifact.State {
	case model.UPLOADED:
		// Already closed. Nothing to do here.
		fallthrough
	case model.APPEND_COMPLETE:
		// This artifact will be eventually shipped to S3. No change required.
		return nil

	case model.APPENDING:
		artifact.State = model.APPEND_COMPLETE
		if err := db.UpdateArtifact(artifact); err != nil {
			return err
		}

		return MergeLogChunks(ctx, artifact, db, s3bucket)

	case model.WAITING_FOR_UPLOAD:
		// Streaming artifact was not uploaded
		artifact.State = model.CLOSED_WITHOUT_DATA
		if err := db.UpdateArtifact(artifact); err != nil {
			return err
		}

		return nil

	default:
		return fmt.Errorf("Unexpected artifact state: %s", artifact.State)
	}
}

// Merges all of the individual chunks into a single object and stores it on s3.
// The log chunks are stored in the database, while the object is uploaded to s3.
func MergeLogChunks(ctx context.Context, artifact *model.Artifact, db database.Database, s3bucket *s3.Bucket) error {
	switch artifact.State {
	case model.APPEND_COMPLETE:
		// TODO: Reimplement using GorpDatabase
		// If the file is empty, don't bother creating an object on S3.
		if artifact.Size == 0 {
			artifact.State = model.CLOSED_WITHOUT_DATA
			artifact.S3URL = ""

			// Conversion between *DatabaseEror and error is tricky. If we don't do this, a nil
			// *DatabaseError can become a non-nil error.
			return db.UpdateArtifact(artifact).GetError()
		}

		// XXX Do we need to commit here or is this handled transparently?
		artifact.State = model.UPLOADING
		if err := db.UpdateArtifact(artifact); err != nil {
			return err
		}

		fileName := artifact.DefaultS3URL()

		r := newLogChunkReaderWithReadahead(artifact, db)

		if err := s3bucket.PutReader(fileName, r, artifact.Size, "binary/octet-stream", s3.PublicRead); err != nil {
			return err
		}

		bytesUploadedCounter.Add(artifact.Size)

		// XXX This is a long operation and should probably be asynchronous from the
		// actual HTTP request, and the client should poll to check when its uploaded.
		artifact.State = model.UPLOADED
		artifact.S3URL = fileName
		if err := db.UpdateArtifact(artifact); err != nil {
			return err
		}

		// From this point onwards, we will not send back any errors back to the user. If we are
		// unable to delete logchunks, we log it to Sentry instead.
		if _, err := db.DeleteLogChunksForArtifact(artifact.Id); err != nil {
			sentry.ReportError(ctx, err)
			return nil
		}

		return nil

	case model.WAITING_FOR_UPLOAD:
		fallthrough
	case model.ERROR:
		fallthrough
	case model.APPENDING:
		fallthrough
	case model.UPLOADED:
		fallthrough
	case model.UPLOADING:
		return fmt.Errorf("Artifact can only be merged when in APPEND_COMPLETE state, but state is %s", artifact.State)
	default:
		return fmt.Errorf("Illegal artifact state! State code is %d", artifact.State)
	}
}

// HandleCloseArtifact handles the HTTP request to close an artifact. See CloseArtifact for details.
func HandleCloseArtifact(ctx context.Context, r render.Render, db database.Database, s3bucket *s3.Bucket, artifact *model.Artifact) {
	if artifact == nil {
		LogAndRespondWithErrorf(ctx, r, http.StatusBadRequest, "No artifact specified")
		return
	}

	if err := CloseArtifact(ctx, artifact, db, s3bucket, true); err != nil {
		LogAndRespondWithError(ctx, r, http.StatusBadRequest, err)
		return
	}

	r.JSON(http.StatusOK, map[string]interface{}{})
}

func intParam(v url.Values, paramName string, fallback int64) int64 {
	if value := v.Get(paramName); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return int64(intVal)
		}

		// TODO: should we should raise an error here because the query is badly formatted?
	}

	return fallback
}

func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

var errReadBeyondEOF = errors.New("Reading beyond EOF")

func getByteRangeFromRequest(req *http.Request, artifact *model.Artifact) (int64, int64, error) {
	queryParams := req.URL.Query()

	// byteRangeBegin and byteRangeEnd are inclusive range markers within an artifact.
	byteRangeBegin := max(intParam(queryParams, "offset", 0), 0)
	if byteRangeBegin >= artifact.Size {
		return 0, artifact.Size, errReadBeyondEOF
	}

	limit := max(intParam(queryParams, "limit", 0), 0)
	if limit == 0 {
		limit = MaxChunkedRequestBytes
	}
	limit = min(limit, MaxChunkedRequestBytes)

	byteRangeEnd := min(artifact.Size-1, byteRangeBegin+limit-1)

	return byteRangeBegin, byteRangeEnd, nil
}

// GetArtifactContentChunks lists artifact contents in a chunked form. Useful to poll for updates to
// chunked artifacts. All artifact types are supported and chunks can be requested from arbitrary
// locations within artifacts.
//
// This is primarily meant for Changes UI for log following. If you need to fetch byte ranges from the
// store, it should be available directly at /content
//
// URL query parameters offset and limit can be used to control range of chunks to be fetched.
// offset -> byte offset of the start of the range to be fetched (defaults to beginning of artifact)
// limit  -> number of bytes to be fetched (defaults to 100KB)
//
// Negative values for any query parameter will cause it to be set to 0 (default)
func GetArtifactContentChunks(ctx context.Context, r render.Render, req *http.Request, res http.ResponseWriter, db database.Database, s3bucket *s3.Bucket, artifact *model.Artifact) {
	if artifact == nil {
		LogAndRespondWithErrorf(ctx, r, http.StatusBadRequest, "No artifact specified")
		return
	}

	type Chunk struct {
		ID     int64  `json:"id"`
		Offset int64  `json:"offset"`
		Size   int64  `json:"size"`
		Text   string `json:"text"`
	}

	type Result struct {
		Chunks     []Chunk `json:"chunks"`
		NextOffset int64   `json:"nextOffset"`
	}

	byteRangeBegin, byteRangeEnd, err := getByteRangeFromRequest(req, artifact)

	if err != nil {
		// If given range is not valid, steer client to a valid range.
		r.JSON(http.StatusOK, &Result{Chunks: []Chunk{}, NextOffset: byteRangeEnd})
		return
	}

	switch artifact.State {
	case model.UPLOADING:
		// No data to report right now. Wait till upload to S3 completes.
		fallthrough
	case model.WAITING_FOR_UPLOAD:
		// Upload hasn't started. No data to report. Try again later.
		r.JSON(http.StatusOK, &Result{Chunks: []Chunk{}, NextOffset: byteRangeBegin})
		return
	case model.UPLOADED:
		// Fetch from S3
		url := s3bucket.SignedURL(artifact.S3URL, time.Now().Add(30*time.Minute))
		rq, err := http.NewRequest("GET", url, nil)
		rq.Header.Add("Range", fmt.Sprintf("bytes=%d-%d", byteRangeBegin, byteRangeEnd))
		resp, err := http.DefaultClient.Do(rq)
		if err != nil {
			LogAndRespondWithError(ctx, r, http.StatusInternalServerError, err)
			return
		}
		if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
			LogAndRespondWithErrorf(ctx, r, http.StatusBadRequest, fmt.Sprintf("Bad status code %d", resp.StatusCode))
			return
		}
		var buf bytes.Buffer
		n, err := buf.ReadFrom(resp.Body)
		if err != nil {
			LogAndRespondWithError(ctx, r, http.StatusInternalServerError, err)
			return
		}
		r.JSON(http.StatusOK, &Result{
			Chunks:     []Chunk{Chunk{Offset: byteRangeBegin, Size: int64(n), Text: buf.String()}},
			NextOffset: byteRangeBegin + int64(n),
		})
		return
	case model.APPENDING:
		fallthrough
	case model.APPEND_COMPLETE:
		// Pick from log chunks
		rd := newLogChunkReader(artifact, db)
		rd.Seek(byteRangeBegin, os.SEEK_SET)

		bts := make([]byte, byteRangeEnd-byteRangeBegin+1)
		n, err := runeLimitedRead(rd, bts)
		if err != nil && err != io.EOF {
			LogAndRespondWithError(ctx, r, http.StatusInternalServerError, err)
			return
		}

		if n > 0 {
			r.JSON(http.StatusOK, &Result{
				Chunks:     []Chunk{Chunk{Offset: byteRangeBegin, Size: int64(n), Text: string(bts[:n])}},
				NextOffset: byteRangeBegin + int64(n),
			})
		} else {
			r.JSON(http.StatusOK, &Result{Chunks: []Chunk{}, NextOffset: byteRangeBegin})
		}
		return
	}
}

func GetArtifactContent(ctx context.Context, r render.Render, req *http.Request, res http.ResponseWriter, db database.Database, s3bucket *s3.Bucket, artifact *model.Artifact) {
	if artifact == nil {
		LogAndRespondWithErrorf(ctx, r, http.StatusBadRequest, "No artifact specified")
		return
	}

	switch artifact.State {
	case model.UPLOADED:
		// Fetch from S3
		reader, err := s3bucket.GetReader(artifact.S3URL)
		if err != nil {
			LogAndRespondWithError(ctx, r, http.StatusInternalServerError, err)
			return
		}
		contentdisposition.SetFilename(res, filepath.Base(artifact.RelativePath))
		if _, err = io.Copy(res, reader); err != nil {
			LogAndRespondWithErrorf(ctx, r, http.StatusInternalServerError, "Error transferring artifact: %s", err)
			return
		}
		return
	case model.UPLOADING:
		// Not done uploading to S3 yet. Error.
		LogAndRespondWithErrorf(ctx, r, http.StatusNotFound, "Waiting for content to complete uploading")
		return
	case model.APPENDING:
		fallthrough
	case model.APPEND_COMPLETE:
		// Pick from log chunks
		contentdisposition.SetFilename(res, filepath.Base(artifact.RelativePath))
		// All written bytes are immutable. So, unless size changes, all previously read contents can be cached.
		res.Header().Add("ETag", strconv.Itoa(int(artifact.Size)))
		http.ServeContent(res, req, filepath.Base(artifact.RelativePath), time.Time{}, newLogChunkReaderWithReadahead(artifact, db))
		return
	case model.WAITING_FOR_UPLOAD:
		// Not started yet. Error
		LogAndRespondWithErrorf(ctx, r, http.StatusNotFound, "Waiting for content to get uploaded")
		return
	}
}

type PutArtifactReq struct {
	ContentLength string
	Body          io.Reader
}

// PutArtifact writes a streamed artifact to S3. The entire file contents are streamed directly
// through to S3. If S3 is not accessible, we don't make any attempt to buffer on disk and fail
// immediately.
func PutArtifact(ctx context.Context, artifact *model.Artifact, db database.Database, bucket *s3.Bucket, req PutArtifactReq) error {
	if artifact.State != model.WAITING_FOR_UPLOAD {
		return fmt.Errorf("Expected artifact to be in state WAITING_FOR_UPLOAD: %s", artifact.State)
	}

	// New file being inserted into DB.
	// Mark status change to UPLOADING and start uploading to S3.
	//
	// First, verify that the size of the content being uploaded matches our expected size.
	var fileSize int64
	var err error

	if req.ContentLength != "" {
		fileSize, err = strconv.ParseInt(req.ContentLength, 10, 64) // string, base, bits
		// This should never happen if a sane HTTP client is used. Nonetheless ...
		if err != nil {
			return fmt.Errorf("Invalid Content-Length specified")
		}
	} else {
		// This too should never happen if a sane HTTP client is used. Nonetheless ...
		return fmt.Errorf("Content-Length not specified")
	}

	if fileSize != artifact.Size {
		return fmt.Errorf("Content length %d does not match expected file size %d", fileSize, artifact.Size)
	}

	// XXX Do we need to commit here or is this handled transparently?
	artifact.State = model.UPLOADING
	if err := db.UpdateArtifact(artifact); err != nil {
		return err
	}

	cleanupAndReturn := func(err error) error {
		// TODO: Is there a better way to detect and handle errors?
		// Use a channel to signify upload completion. In defer, check if the channel is empty. If
		// yes, mark error. Else ignore.
		if err != nil {
			// TODO: s/ERROR/WAITING_FOR_UPLOAD/ ?
			sentry.ReportError(ctx, err)
			artifact.State = model.ERROR
			err2 := db.UpdateArtifact(artifact)
			if err2 != nil {
				log.Printf("Error while handling error: %s", err2.Error())
			}
			return err
		}

		return nil
	}

	fileName := artifact.DefaultS3URL()
	if err := bucket.PutReader(fileName, req.Body, artifact.Size, "binary/octet-stream", s3.PublicRead); err != nil {
		return cleanupAndReturn(fmt.Errorf("Error uploading to S3: %s", err))
	}
	bytesUploadedCounter.Add(artifact.Size)

	artifact.State = model.UPLOADED
	artifact.S3URL = fileName
	if err := db.UpdateArtifact(artifact); err != nil {
		return err
	}
	return nil
}

// Returns nil on error.
//
// TODO return errors on error
func GetArtifact(bucket *model.Bucket, artifact_name string, db database.Database) *model.Artifact {
	if bucket == nil {
		return nil
	}

	if artifact, err := db.GetArtifactByName(bucket.Id, artifact_name); err != nil {
		return nil
	} else {
		return artifact
	}
}
