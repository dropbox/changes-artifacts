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
	"time"

	"github.com/cenkalti/backoff"
	"github.com/dropbox/changes-artifacts/model"
)

const MAX_PENDING_REPORTS = 100

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
	server string
}

func NewArtifactStoreClient(serverURL string) *ArtifactStoreClient {
	return &ArtifactStoreClient{server: serverURL}
}

func getApiJson(url string) (io.ReadCloser, *ArtifactsError) {
	// XXX until we use a real logging system, this is too verbose
	// log.Printf("[artifactsclient] GET %s\n", url)
	if resp, err := http.Get(url); err != nil {
		return nil, NewRetriableError(err.Error())
	} else {
		if resp.StatusCode != http.StatusOK {
			return nil, determineResponseError(resp, url, "POST")
		}
		return resp.Body, nil
	}
}

func postApiJson(url string, params map[string]interface{}) (io.ReadCloser, *ArtifactsError) {
	// XXX until we use a real logging system, this is too verbose
	// log.Printf("[artifactsclient] POST %s\n", url)
	if mJson, err := json.Marshal(params); err != nil {
		// Marshalling is deterministic so we can't retry in this scenario.
		return nil, NewTerminalError(err.Error())
	} else if resp, err := http.Post(url, "application/json", bytes.NewReader(mJson)); err != nil {
		return nil, NewTerminalErrorf(err.Error())
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
	body, err := getApiJson(c.server + "/buckets/" + bucketName)

	if err != nil {
		return nil, err
	}

	return c.parseBucketFromResponse(body)
}

// XXX deadlineMins is not used. Is this planned for something?
func (c *ArtifactStoreClient) NewBucket(bucketName string, owner string, deadlineMins int) (*Bucket, *ArtifactsError) {
	body, err := postApiJson(c.server+"/buckets/", map[string]interface{}{
		"id":    bucketName,
		"owner": owner,
	})

	if err != nil {
		return nil, err
	}

	return c.parseBucketFromResponse(body)
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
	body, err := postApiJson(b.client.server+"/buckets/"+b.bucket.Id+"/artifacts", map[string]interface{}{
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

// Creates a new streamed (fixed-size) artifact with the specified name (acting as an id)
// and size. The artifact will only be complete when the server has received exactly
// "size" bytes. This is only suitable for static content such as files.
func (b *Bucket) NewStreamedArtifact(name string, size int64) (*StreamedArtifact, *ArtifactsError) {
	body, err := postApiJson(b.client.server+"/buckets/"+b.bucket.Id+"/artifacts", map[string]interface{}{
		"chunked": false,
		"name":    name,
		"size":    size,
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
	body, err := getApiJson(b.client.server + "/buckets/" + b.bucket.Id + "/artifacts/" + name)

	if err != nil {
		return nil, err
	}

	return b.parseArtifactFromResponse(body)
}

func (b *Bucket) ListArtifacts() ([]Artifact, *ArtifactsError) {
	body, err := getApiJson(b.client.server + "/buckets/" + b.bucket.Id + "/artifacts/")
	if err != nil {
		return nil, NewRetriableError(err.Error())
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
	_, err := postApiJson(b.client.server+"/buckets/"+b.bucket.Id+"/close", map[string]interface{}{})
	return err
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

// A chunked artifact is one which can be sent in chunks of
// varying size. It is only complete upon the client manually
// telling the server that it is complete, and is useful for
// logs and other other artifacts whose size is not known
// at the same they are streaming.
type ChunkedArtifact struct {
	*ArtifactImpl
	offset       int
	stringStream chan string
	fatalErr     chan *ArtifactsError
	complete     chan bool
}

func (artifact *ChunkedArtifact) init() *ChunkedArtifact {
	artifact.offset = 0
	artifact.stringStream = make(chan string, MAX_PENDING_REPORTS)
	artifact.complete = make(chan bool)
	artifact.fatalErr = make(chan *ArtifactsError)
	go artifact.pushLogChunks()

	return artifact
}

func (artifact *ChunkedArtifact) Flush() *ArtifactsError {
	close(artifact.stringStream)

	select {
	case err := <-artifact.fatalErr:
		return err
	case _ = <-artifact.complete:

		// Recreate the stream and start pushing again.
		artifact.stringStream = make(chan string, MAX_PENDING_REPORTS)
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

func (artifact *ChunkedArtifact) pushLogChunks() {
	var err *ArtifactsError
	b := backoff.NewExponentialBackOff()
	b.MaxInterval = 15 * time.Second
	for logChunk := range artifact.stringStream {
		ticker := backoff.NewTicker(b)
		for _ = range ticker.C {
			_, err = postApiJson(fmt.Sprintf("%s/buckets/%s/artifacts/%s", artifact.bucket.client.server, artifact.bucket.bucket.Id, artifact.artifact.Name), map[string]interface{}{
				"size":       len(logChunk),
				"content":    logChunk,
				"byteoffset": artifact.offset,
			})

			if err != nil {
				continue
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
	artifact.stringStream <- chunk

	// TODO: Verify that the created logchunk matches our expectations?
	return nil
}

func (a *StreamedArtifact) UploadArtifact(stream io.Reader) *ArtifactsError {
	url := fmt.Sprintf("%s/buckets/%s/artifacts/%s", a.bucket.client.server, a.bucket.bucket.Id, a.artifact.Name)
	log.Printf("[artifactsclient] POST %s\n", url)
	if resp, err := http.Post(url, "application/octet-stream", stream); err != nil {
		return NewTerminalErrorf(err.Error())
	} else {
		if resp.StatusCode != http.StatusOK {
			return determineResponseError(resp, url, "POST")
		}
		return nil
	}
}

func (a *ChunkedArtifact) Close() *ArtifactsError {
	if err := a.Flush(); err != nil {
		return err
	}

	_, err := postApiJson(a.bucket.client.server+"/buckets/"+a.bucket.bucket.Id+"/artifacts/"+a.artifact.Name+"/close", map[string]interface{}{})
	return err
}

func (a ArtifactImpl) GetContent() (io.ReadCloser, *ArtifactsError) {
	url := fmt.Sprintf("%s/buckets/%s/artifacts/%s/content",
		a.bucket.client.server, a.bucket.bucket.Id, a.artifact.Name)
	if resp, err := http.Get(url); err != nil {
		return nil, NewRetriableError(err.Error())
	} else {
		if resp.StatusCode != http.StatusOK {
			return nil, determineResponseError(resp, url, "GET")
		}
		return resp.Body, nil
	}
}
