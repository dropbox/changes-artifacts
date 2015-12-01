package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"golang.org/x/net/context"
	"golang.org/x/net/context/ctxhttp"

	"github.com/cenkalti/backoff"
	"github.com/dropbox/changes-artifacts/model"
)

const MAX_PENDING_REPORTS = 100

// Default timeout for any HTTP requests. On timeout, mark the operation as failed, but retriable.
const DefaultReqTimeout = 30 * time.Second

func ignoreBody(body io.ReadCloser, err *ArtifactsError) *ArtifactsError {
	if body != nil {
		body.Close()
	}

	return err
}

type ArtifactsError struct {
	errStr    string
	retriable bool
}

func (e *ArtifactsError) Error() string {
	return e.errStr
}

func (e *ArtifactsError) IsRetriable() bool {
	return e.retriable
}

func NewRetriableError(errStr string) *ArtifactsError {
	return &ArtifactsError{retriable: true, errStr: errStr}
}

func NewRetriableErrorf(format string, args ...interface{}) *ArtifactsError {
	return NewRetriableError(fmt.Sprintf(format, args...))
}

func NewTerminalError(errStr string) *ArtifactsError {
	return &ArtifactsError{retriable: false, errStr: errStr}
}

func NewTerminalErrorf(format string, args ...interface{}) *ArtifactsError {
	return NewTerminalError(fmt.Sprintf(format, args...))
}

type ArtifactStoreClient struct {
	server  string
	ctx     context.Context
	timeout time.Duration
}

func NewArtifactStoreClient(serverURL string) *ArtifactStoreClient {
	return NewArtifactStoreClientWithContext(serverURL, DefaultReqTimeout, context.TODO())
}

// NewArtifactStoreClientWithContext creates a new client with given context and per-request
// timeout.
func NewArtifactStoreClientWithContext(serverURL string, timeout time.Duration, ctx context.Context) *ArtifactStoreClient {
	return &ArtifactStoreClient{server: serverURL, timeout: timeout, ctx: ctx}
}

func (c *ArtifactStoreClient) getAPI(path string) (io.ReadCloser, *ArtifactsError) {
	url := c.server + path
	ctx, cancel := context.WithTimeout(c.ctx, c.timeout)
	defer cancel()

	if resp, err := ctxhttp.Get(ctx, nil, url); err != nil {
		return nil, NewRetriableError(err.Error())
	} else {
		if resp.StatusCode != http.StatusOK {
			return nil, determineResponseError(resp, url, "POST")
		}
		return resp.Body, nil
	}
}

func (c *ArtifactStoreClient) postAPIJSON(path string, params map[string]interface{}) (io.ReadCloser, *ArtifactsError) {
	mJSON, err := json.Marshal(params)
	if err != nil {
		// Marshalling is deterministic so we can't retry in this scenario.
		return nil, NewTerminalError(err.Error())
	}

	return c.postAPI(path, "application/json", bytes.NewReader(mJSON))
}

func (c *ArtifactStoreClient) postAPI(path string, contentType string, body io.Reader) (io.ReadCloser, *ArtifactsError) {
	url := c.server + path
	ctx, cancel := context.WithTimeout(c.ctx, c.timeout)
	defer cancel()

	if resp, err := ctxhttp.Post(ctx, nil, url, contentType, body); err != nil {
		// If there was an error connecting to the server, it is likely to be transient and should be
		// retried.
		return nil, NewRetriableError(err.Error())
	} else {
		if resp.StatusCode != http.StatusOK {
			return nil, determineResponseError(resp, url, "POST")
		}
		return resp.Body, nil
	}
}

func (c *ArtifactStoreClient) parseBucketFromResponse(body io.ReadCloser) (*Bucket, *ArtifactsError) {
	bText, err := ioutil.ReadAll(body)
	if err != nil {
		return nil, NewRetriableError(err.Error())
	}
	body.Close()

	bucket := new(model.Bucket)
	if err := json.Unmarshal(bText, bucket); err != nil {
		return nil, NewTerminalError(err.Error())
	}

	return &Bucket{
		client: c,
		bucket: bucket,
	}, nil
}

// Determine the parse error for the response, which corresponds
// to the "error" field in its json-encoded body.
func parseErrorForResponse(body io.ReadCloser) (string, error) {
	var bJson map[string]string

	bText, err := ioutil.ReadAll(body)
	if err != nil {
		return "", err
	}
	body.Close()

	err = json.Unmarshal(bText, &bJson)
	if err != nil {
		return "", err
	}
	parseError, ok := bJson["error"]
	if !ok {
		return "", errors.New("Response body did not contain error key")
	}
	return parseError, nil
}

// Return either a terminal or retriable error for a failed response
// depending on the type of status code in the response, and format
// it in a nice way showing the url and method.
func determineResponseError(resp *http.Response, url string, method string) *ArtifactsError {
	parsedError, err := parseErrorForResponse(resp.Body)
	if err != nil {
		parsedError = fmt.Sprintf("Unknown error, could not parse body: %s", err.Error())
	}
	if resp.StatusCode >= 500 {
		// Server error. Maybe DB is unreachable. Can be retried.
		return NewRetriableErrorf("Error %d [%s %s] %s", resp.StatusCode, method, url, parsedError)
	}
	return NewTerminalErrorf("Error %d [%s %s] %s", resp.StatusCode, method, url, parsedError)
}

func (c *ArtifactStoreClient) GetBucket(bucketName string) (*Bucket, *ArtifactsError) {
	body, err := c.getAPI(fmt.Sprintf("/buckets/%s", bucketName))

	if err != nil {
		return nil, err
	}

	bucket, e := c.parseBucketFromResponse(body)

	if e != nil {
		return nil, e
	}

	if bucket.bucket.Id != bucketName {
		return nil, NewTerminalError("Bucket created with wrong name")
	}

	return bucket, nil
}

// XXX deadlineMins is not used. Is this planned for something?
func (c *ArtifactStoreClient) NewBucket(bucketName string, owner string, deadlineMins int) (*Bucket, *ArtifactsError) {
	body, err := c.postAPIJSON("/buckets/", map[string]interface{}{
		"id":    bucketName,
		"owner": owner,
	})

	if err != nil {
		return nil, err
	}

	bucket, err := c.parseBucketFromResponse(body)
	if err != nil {
		return nil, err
	}

	if bucket.bucket.Id != bucketName {
		return nil, NewTerminalError("Bucket created with wrong name")
	}

	return bucket, err
}

type Bucket struct {
	client *ArtifactStoreClient
	bucket *model.Bucket
}

func (b *Bucket) parseArtifactFromResponse(body io.ReadCloser) (Artifact, *ArtifactsError) {
	bText, err := ioutil.ReadAll(body)
	if err != nil {
		return nil, NewRetriableError(err.Error())
	}
	body.Close()

	artifact := new(model.Artifact)
	if err := json.Unmarshal(bText, artifact); err != nil {
		return nil, NewTerminalError(err.Error())
	}

	return &ArtifactImpl{
		artifact: artifact,
		bucket:   b,
	}, nil
}

// Creates a new chunked artifact whose size does not have to be known. The name
// acts as an id for the artifact. Because of additional overhead if the size is
// already known then `NewStreamedArtifact` may be more applicable.
func (b *Bucket) NewChunkedArtifact(name string) (*ChunkedArtifact, *ArtifactsError) {
	body, err := b.client.postAPIJSON(fmt.Sprintf("/buckets/%s/artifacts", b.bucket.Id), map[string]interface{}{
		"chunked": true,
		"name":    name,
	})

	if err != nil {
		return nil, err
	}

	artifact, err := b.parseArtifactFromResponse(body)

	if err != nil {
		return nil, err
	}

	return (&ChunkedArtifact{ArtifactImpl: artifact.(*ArtifactImpl)}).init(), nil
}

// NewStreamedArtifact creates a new streamed (fixed-size) artifact given a file path and size.
// The artifact name (which serves as its id) is computed from the file name (this is a hint to the
// server which is free to modify the artifact name).
//
// The artifact does not actually get uploaded here - that will need to be perfomeed in
// UploadArtifact. The artifact will only be complete when the server has received exactly "size"
// bytes. This is only suitable for static content such as files.
func (b *Bucket) NewStreamedArtifact(path string, size int64) (*StreamedArtifact, *ArtifactsError) {
	name := filepath.Base(path)
	body, err := b.client.postAPIJSON(fmt.Sprintf("/buckets/%s/artifacts", b.bucket.Id), map[string]interface{}{
		"chunked":      false,
		"name":         name,
		"size":         size,
		"relativePath": path,
	})

	if err != nil {
		return nil, err
	}

	artifact, err := b.parseArtifactFromResponse(body)

	if err != nil {
		return nil, err
	}

	return &StreamedArtifact{
		ArtifactImpl: artifact.(*ArtifactImpl),
	}, nil
}

func (b *Bucket) GetArtifact(name string) (Artifact, *ArtifactsError) {
	body, err := b.client.getAPI(fmt.Sprintf("/buckets/%s/artifacts/%s", b.bucket.Id, name))

	if err != nil {
		return nil, err
	}

	return b.parseArtifactFromResponse(body)
}

func (b *Bucket) ListArtifacts() ([]Artifact, *ArtifactsError) {
	body, err := b.client.getAPI(fmt.Sprintf("/buckets/%s/artifacts/", b.bucket.Id))
	if err != nil {
		return nil, err
	}

	return b.parseArtifactListFromResponse(body)
}

func (b *Bucket) parseArtifactListFromResponse(body io.ReadCloser) ([]Artifact, *ArtifactsError) {
	bText, err := ioutil.ReadAll(body)
	if err != nil {
		return nil, NewRetriableError(err.Error())
	}
	body.Close()

	artifacts := []model.Artifact{}
	if err := json.Unmarshal(bText, &artifacts); err != nil {
		return nil, NewTerminalError(err.Error())
	}

	wrappedArtifacts := make([]Artifact, len(artifacts))
	for i, _ := range artifacts {
		wrappedArtifacts[i] = &ArtifactImpl{
			artifact: &artifacts[i],
			bucket:   b,
		}
	}

	return wrappedArtifacts, nil
}

func (b *Bucket) Close() *ArtifactsError {
	return ignoreBody(b.client.postAPIJSON(fmt.Sprintf("/buckets/%s/close", b.bucket.Id), map[string]interface{}{}))
}

type Artifact interface {
	// Returns a read-only copy of the raw model.Artifact instance associated with the artifact
	GetArtifactModel() *model.Artifact

	// Returns a handle to the bucket containing the artifact
	GetBucket() *Bucket

	// Return raw contents of the artifact (artifact file or text of a log stream)
	// as an io.ReadCloser. It is the responsibility of the caller to close the
	// io.ReadCloser
	GetContent() (io.ReadCloser, *ArtifactsError)

	// Returns a direct link to the raw contents of this artifact
	GetContentURL() string
}

type ArtifactImpl struct {
	artifact *model.Artifact
	bucket   *Bucket
}

func (ai *ArtifactImpl) GetArtifactModel() *model.Artifact {
	return ai.artifact
}

func (ai *ArtifactImpl) GetBucket() *Bucket {
	return ai.bucket
}

// GetContentURL returns a direct link to the raw contents of an artifact
func (ai *ArtifactImpl) GetContentURL() string {
	return fmt.Sprintf("%s/buckets/%s/artifacts/%s/content", ai.bucket.client.server, ai.bucket.bucket.Id, ai.artifact.Name)
}

// A chunked artifact is one which can be sent in chunks of
// varying size. It is only complete upon the client manually
// telling the server that it is complete, and is useful for
// logs and other other artifacts whose size is not known
// at the same they are streaming.
type ChunkedArtifact struct {
	*ArtifactImpl
	offset     int
	bytestream chan []byte
	fatalErr   chan *ArtifactsError
	complete   chan bool
}

func (artifact *ChunkedArtifact) init() *ChunkedArtifact {
	artifact.offset = 0
	artifact.bytestream = make(chan []byte, MAX_PENDING_REPORTS)
	artifact.complete = make(chan bool)
	artifact.fatalErr = make(chan *ArtifactsError)
	go artifact.pushLogChunks()

	return artifact
}

func (artifact *ChunkedArtifact) Flush() *ArtifactsError {
	close(artifact.bytestream)

	select {
	case <-artifact.bucket.client.ctx.Done():
		return NewTerminalError(artifact.bucket.client.ctx.Err().Error())
	case err := <-artifact.fatalErr:
		return err
	case _ = <-artifact.complete:
		// Recreate the stream and start pushing again.
		artifact.bytestream = make(chan []byte, MAX_PENDING_REPORTS)
		go artifact.pushLogChunks()
		return nil
	}
}

// A streamed artifact is a fixed-size artifact whose size
// is known at the start of the transfer. It is therefore
// not suitable for logs but useful for static files, etc.
type StreamedArtifact struct {
	*ArtifactImpl
}

func newTicker() *backoff.Ticker {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = 100 * time.Millisecond
	b.MaxInterval = 5 * time.Second
	b.MaxElapsedTime = 15 * time.Second

	return backoff.NewTicker(b)
}

func (artifact *ChunkedArtifact) pushLogChunks() {
	var err *ArtifactsError
	for logChunk := range artifact.bytestream {
		ticker := newTicker()
		for {
			// If our parent context has been cancelled, we discard state and get out.
			select {
			case <-ticker.C:
			case <-artifact.bucket.client.ctx.Done():
				log.Println("Client context has closed during log upload. Bailing out without any further log upload operations.")
				ticker.Stop()
				return
			}

			err = ignoreBody(artifact.bucket.client.postAPIJSON(fmt.Sprintf("/buckets/%s/artifacts/%s", artifact.bucket.bucket.Id, artifact.artifact.Name), map[string]interface{}{
				"size":       len(logChunk),
				"bytes":      logChunk,
				"byteoffset": artifact.offset,
			}))

			if err != nil {
				if err.IsRetriable() {
					// Let's retry the request after a backoff
					continue
				} else {
					// This either means the server lost some of our logchunks (because of a rollback), or a
					// proxy timeout caused the previous request to succeed at the server but the proxy
					// returned an error.
					//
					// TODO: We should try to salvage the situation by seeing if we can skip a logchunk.
					// For now, bail.
					artifact.fatalErr <- err
					return
				}
			}

			artifact.offset += len(logChunk)
			ticker.Stop()
			break
		}
	}

	if err != nil {
		artifact.fatalErr <- err
	} else {
		artifact.complete <- true
	}
}

// Appends the log chunk to the stream. This is asynchronous so any errors
// in sending will occur when closing the artifact.
func (artifact *ChunkedArtifact) AppendLog(chunk string) *ArtifactsError {
	// TODO: There is no reason for this method to take in a string anymore.
	// Update to use []byte or support io.Writer interface.
	artifact.bytestream <- []byte(chunk)

	return nil
}

func (a *StreamedArtifact) UploadArtifact(stream io.Reader) *ArtifactsError {
	url := fmt.Sprintf("/buckets/%s/artifacts/%s", a.bucket.bucket.Id, a.artifact.Name)
	// TODO: Verify that the artifact that was stored matches the one we just uploaded.
	return ignoreBody(a.bucket.client.postAPI(url, "application/octet-stream", stream))
}

func (a *ChunkedArtifact) Close() *ArtifactsError {
	if err := a.Flush(); err != nil {
		return err
	}

	return ignoreBody(a.bucket.client.postAPIJSON(fmt.Sprintf("/buckets/%s/artifacts/%s/close", a.bucket.bucket.Id, a.artifact.Name), map[string]interface{}{}))
}

func (a ArtifactImpl) GetContent() (io.ReadCloser, *ArtifactsError) {
	url := fmt.Sprintf("/buckets/%s/artifacts/%s/content", a.bucket.bucket.Id, a.artifact.Name)
	return a.bucket.client.getAPI(url)
}
