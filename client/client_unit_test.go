package client

import (
	"net/http"
	"testing"
	"time"

	"golang.org/x/net/context"

	"github.com/dropbox/changes-artifacts/client/testserver"
	"github.com/dropbox/changes-artifacts/model"
	"github.com/stretchr/testify/assert"
)

func TestNewBucketSuccessStateWithWrongName(t *testing.T) {
	ts := testserver.NewTestServer(t)
	defer ts.CloseAndAssertExpectations()

	client := NewArtifactStoreClient(ts.URL)

	ts.ExpectAndRespond("POST", "/buckets/", http.StatusOK, `{"Id": "1234"}`)

	b, err := client.NewBucket("foo", "bar", 32)
	assert.Nil(t, b)
	assert.Error(t, err)
	assert.False(t, err.IsRetriable(), "Error %s should not be retriable", err)
}

func TestNewBucketSuccessfully(t *testing.T) {
	ts := testserver.NewTestServer(t)
	defer ts.CloseAndAssertExpectations()

	client := NewArtifactStoreClient(ts.URL)

	ts.ExpectAndRespond("POST", "/buckets/", http.StatusOK, `{"Id": "foo"}`)

	b, err := client.NewBucket("foo", "bar", 32)
	assert.NotNil(t, b)
	assert.NoError(t, err)
}

func TestGetBucketSuccessStateWithWrongName(t *testing.T) {
	ts := testserver.NewTestServer(t)
	defer ts.CloseAndAssertExpectations()

	client := NewArtifactStoreClient(ts.URL)

	ts.ExpectAndRespond("GET", "/buckets/foo", http.StatusOK, `{"Id": "1234"}`)

	b, err := client.GetBucket("foo")
	assert.Nil(t, b)
	assert.Error(t, err)
	assert.False(t, err.IsRetriable(), "Error %s should not be retriable", err)
}

func TestGetBucketSuccessfully(t *testing.T) {
	ts := testserver.NewTestServer(t)
	defer ts.CloseAndAssertExpectations()

	client := NewArtifactStoreClient(ts.URL)

	ts.ExpectAndRespond("GET", "/buckets/foo", http.StatusOK, `{"Id": "foo"}`)

	b, err := client.GetBucket("foo")
	assert.NotNil(t, b)
	assert.NoError(t, err)
}

func TestNewStreamedArtifactWithWrongName(t *testing.T) {
	ts := testserver.NewTestServer(t)
	defer ts.CloseAndAssertExpectations()

	client := NewArtifactStoreClient(ts.URL)

	ts.ExpectAndRespond("POST", "/buckets/", http.StatusOK, `{"Id": "foo"}`)
	ts.ExpectAndRespond("POST", "/buckets/foo/artifacts", http.StatusOK, `{"Name": "not_correct_name"}`)

	b, _ := client.NewBucket("foo", "bar", 32)
	sa, err := b.NewStreamedArtifact("artifact", 10)
	assert.Nil(t, sa)
	assert.Error(t, err)
}

func TestNewStreamedArtifactSuccessfully(t *testing.T) {
	ts := testserver.NewTestServer(t)
	defer ts.CloseAndAssertExpectations()

	client := NewArtifactStoreClient(ts.URL)

	ts.ExpectAndRespond("POST", "/buckets/", http.StatusOK, `{"Id": "foo"}`)
	ts.ExpectAndRespond("POST", "/buckets/foo/artifacts", http.StatusOK, `{"Name": "artifact"}`)

	b, _ := client.NewBucket("foo", "bar", 32)
	sa, err := b.NewStreamedArtifact("artifact", 10)
	assert.NotNil(t, sa)
	assert.NoError(t, err)
}

func TestNewBucketErrors(t *testing.T) {
	testErrorCombinations(t, func(*testserver.TestServer, *ArtifactStoreClient) interface{} { return nil }, "POST", "/buckets/",
		func(c *ArtifactStoreClient, _ interface{}) (interface{}, *ArtifactsError) {
			return c.NewBucket("foo", "bar", 12)
		})
}

func TestGetBucketErrors(t *testing.T) {
	testErrorCombinations(t, func(*testserver.TestServer, *ArtifactStoreClient) interface{} { return nil }, "GET", "/buckets/foo",
		func(c *ArtifactStoreClient, _ interface{}) (interface{}, *ArtifactsError) {
			return c.GetBucket("foo")
		})
}

func TestCreateStreamingArtifactErrors(t *testing.T) {
	testErrorCombinations(t,
		func(ts *testserver.TestServer, c *ArtifactStoreClient) interface{} {
			ts.ExpectAndRespond("POST", "/buckets/", http.StatusOK, `{"Id": "foo"}`)
			b, _ := c.NewBucket("foo", "bar", 32)
			return b
		},
		"POST", "/buckets/foo/artifacts",
		func(c *ArtifactStoreClient, b interface{}) (interface{}, *ArtifactsError) {
			return b.(*Bucket).NewStreamedArtifact("artifact", 10)
		})
}

func TestCreateChunkedArtifactErrors(t *testing.T) {
	testErrorCombinations(t,
		func(ts *testserver.TestServer, c *ArtifactStoreClient) interface{} {
			ts.ExpectAndRespond("POST", "/buckets/", http.StatusOK, `{"Id": "foo"}`)
			b, _ := c.NewBucket("foo", "bar", 32)
			return b
		},
		"POST", "/buckets/foo/artifacts",
		func(c *ArtifactStoreClient, b interface{}) (interface{}, *ArtifactsError) {
			return b.(*Bucket).NewChunkedArtifact("artifact")
		})
}

func TestGetArtifactErrors(t *testing.T) {
	testErrorCombinations(t,
		func(ts *testserver.TestServer, c *ArtifactStoreClient) interface{} {
			ts.ExpectAndRespond("POST", "/buckets/", http.StatusOK, `{"Id": "foo"}`)
			ts.ExpectAndRespond("POST", "/buckets/foo/artifacts", http.StatusOK, `{"Id": "bar"}`)
			b, _ := c.NewBucket("foo", "bar", 32)
			b.NewStreamedArtifact("bar", 1234)
			return b
		},
		"GET", "/buckets/foo/artifacts/bar",
		func(c *ArtifactStoreClient, b interface{}) (interface{}, *ArtifactsError) {
			return b.(*Bucket).GetArtifact("bar")
		})
}

func testErrorCombinations(t *testing.T,
	prerun func(*testserver.TestServer, *ArtifactStoreClient) interface{},
	method string,
	url string,
	test func(c *ArtifactStoreClient, obj interface{}) (interface{}, *ArtifactsError)) {
	{
		ts := testserver.NewTestServer(t)
		client := NewArtifactStoreClient(ts.URL)
		obj := prerun(ts, client)

		ts.CloseAndAssertExpectations()
		// Server is missing, network error
		op, err := test(client, obj)
		assert.Nil(t, op)
		assert.Error(t, err)
		assert.True(t, err.IsRetriable(), "Error %s should be retriable", err)
	}

	{
		// Server threw internal error
		ts := testserver.NewTestServer(t)
		defer ts.CloseAndAssertExpectations()

		client := NewArtifactStoreClient(ts.URL)
		obj := prerun(ts, client)
		ts.ExpectAndRespond(method, url, http.StatusInternalServerError, `{"error": "Something bad happened"}`)

		op, err := test(client, obj)
		assert.Nil(t, op)
		assert.Error(t, err)
		assert.True(t, err.IsRetriable(), "Error %s should be retriable", err)
	}

	{
		// Server indicated client error
		ts := testserver.NewTestServer(t)
		defer ts.CloseAndAssertExpectations()

		client := NewArtifactStoreClient(ts.URL)
		obj := prerun(ts, client)
		ts.ExpectAndRespond(method, url, http.StatusBadRequest, `{"error": "Bad client"}`)

		op, err := test(client, obj)
		assert.Nil(t, op)
		assert.Error(t, err)
		assert.False(t, err.IsRetriable(), "Error %s should not be retriable", err)
	}

	{
		// Proxy error - server was unreachable
		ts := testserver.NewTestServer(t)
		defer ts.CloseAndAssertExpectations()

		client := NewArtifactStoreClient(ts.URL)
		obj := prerun(ts, client)
		ts.ExpectAndRespond(method, url, http.StatusBadGateway, `<html>Foo</html>`)

		op, err := test(client, obj)
		assert.Nil(t, op)
		assert.Error(t, err)
		assert.True(t, err.IsRetriable(), "Error %s should be retriable", err)
	}

	{
		// Proxy/server error - mangled output
		ts := testserver.NewTestServer(t)
		defer ts.CloseAndAssertExpectations()

		client := NewArtifactStoreClient(ts.URL)
		obj := prerun(ts, client)
		ts.ExpectAndRespond(method, url, http.StatusOK, `<html></html>`)

		op, err := test(client, obj)
		assert.Nil(t, op)
		assert.Error(t, err)
		assert.False(t, err.IsRetriable(), "Error %s should not be retriable", err)
	}

	{
		// Proxy/server hangs and times out
		ts := testserver.NewTestServer(t)
		defer ts.CloseAndAssertExpectations()
		client := NewArtifactStoreClientWithContext(ts.URL, 100*time.Millisecond, context.Background())
		obj := prerun(ts, client)
		ts.ExpectAndHang(method, url)

		op, err := test(client, obj)
		assert.Nil(t, op)
		assert.Error(t, err)
		assert.True(t, err.IsRetriable(), "Error %s should be retriable", err)
	}
}

func TestUploadLogChunksAndFlushSuccessfully(t *testing.T) {
	ts := testserver.NewTestServer(t)
	defer ts.CloseAndAssertExpectations()

	client := NewArtifactStoreClient(ts.URL)

	ts.ExpectAndRespond("POST", "/buckets/", http.StatusOK, `{"Id": "foo"}`)
	ts.ExpectAndRespond("POST", "/buckets/foo/artifacts", http.StatusOK, `{"Name": "artifact"}`)

	b, _ := client.NewBucket("foo", "bar", 32)
	sa, err := b.NewChunkedArtifact("artifact")
	assert.NotNil(t, sa)
	assert.NoError(t, err)

	{
		// Content request might come later, even as late as Flush()
		ts.ExpectAndRespond("POST", "/buckets/foo/artifacts/artifact", 200, `{}`)
		err := sa.AppendLog("console contents")
		assert.NoError(t, err)
	}

	{
		// Content request might come later, even as late as Flush()
		ts.ExpectAndRespond("POST", "/buckets/foo/artifacts/artifact", 200, `{}`)
		err := sa.AppendLog("more console contents")
		assert.NoError(t, err)
	}

	{
		err := sa.Flush()
		assert.NoError(t, err)
	}
}

func TestUploadLogChunksAndCloseSuccessfully(t *testing.T) {
	ts := testserver.NewTestServer(t)
	defer ts.CloseAndAssertExpectations()

	client := NewArtifactStoreClient(ts.URL)

	ts.ExpectAndRespond("POST", "/buckets/", http.StatusOK, `{"Id": "foo"}`)
	ts.ExpectAndRespond("POST", "/buckets/foo/artifacts", http.StatusOK, `{"Name": "artifact"}`)

	b, _ := client.NewBucket("foo", "bar", 32)
	sa, err := b.NewChunkedArtifact("artifact")
	assert.NotNil(t, sa)
	assert.NoError(t, err)

	{
		// Content request might come later, even as late as Flush()
		ts.ExpectAndRespond("POST", "/buckets/foo/artifacts/artifact", 200, `{}`)
		err := sa.AppendLog("console contents")
		assert.NoError(t, err)
	}

	{
		// Content request might come later, even as late as Flush()
		ts.ExpectAndRespond("POST", "/buckets/foo/artifacts/artifact", 200, `{}`)
		err := sa.AppendLog("more console contents")
		assert.NoError(t, err)
	}

	{
		ts.ExpectAndRespond("POST", "/buckets/foo/artifacts/artifact/close", 200, `{}`)
		err := sa.Close()
		assert.NoError(t, err)
	}
}

func TestPushLogChunkServerSucceedOnRetry(t *testing.T) {
	ts := testserver.NewTestServer(t)
	defer ts.CloseAndAssertExpectations()

	client := NewArtifactStoreClient(ts.URL)

	ts.ExpectAndRespond("POST", "/buckets/", http.StatusOK, `{"Id": "foo"}`)
	ts.ExpectAndRespond("POST", "/buckets/foo/artifacts", http.StatusOK, `{"Name": "artifact"}`)

	b, _ := client.NewBucket("foo", "bar", 32)
	sa, err := b.NewChunkedArtifact("artifact")
	assert.NotNil(t, sa)
	assert.NoError(t, err)

	{
		// Fail with a retriable error first
		ts.ExpectAndRespond("POST", "/buckets/foo/artifacts/artifact", 500, `{}`)
		// Then succeed on retry
		ts.ExpectAndRespond("POST", "/buckets/foo/artifacts/artifact", 200, `{}`)
		err := sa.AppendLog("console contents")
		assert.NoError(t, err)
	}

	{
		ts.ExpectAndRespond("POST", "/buckets/foo/artifacts/artifact/close", 200, `{}`)
		err := sa.Close()
		assert.NoError(t, err)
	}
}

func TestPushLogChunkServerFailWithTerminalError(t *testing.T) {
	ts := testserver.NewTestServer(t)
	defer ts.CloseAndAssertExpectations()

	client := NewArtifactStoreClientWithContext(ts.URL, 100*time.Millisecond, context.Background())

	ts.ExpectAndRespond("POST", "/buckets/", http.StatusOK, `{"Id": "foo"}`)
	ts.ExpectAndRespond("POST", "/buckets/foo/artifacts", http.StatusOK, `{"Name": "artifact"}`)

	b, _ := client.NewBucket("foo", "bar", 32)
	sa, err := b.NewChunkedArtifact("artifact")
	assert.NotNil(t, sa)
	assert.NoError(t, err)

	{
		// Fail with a terminal error
		ts.ExpectAndRespond("POST", "/buckets/foo/artifacts/artifact", 400, `{}`)
		err := sa.AppendLog("console contents")
		assert.NoError(t, err)
		err = sa.Close()
		assert.Error(t, err)
		assert.False(t, err.IsRetriable())
	}
}

func TestPushLogChunkCancelledContext(t *testing.T) {
	ts := testserver.NewTestServer(t)
	defer ts.CloseAndAssertExpectations()

	ctx, cancel := context.WithCancel(context.Background())
	client := NewArtifactStoreClientWithContext(ts.URL, 100*time.Millisecond, ctx)

	ts.ExpectAndRespond("POST", "/buckets/", http.StatusOK, `{"Id": "foo"}`)
	ts.ExpectAndRespond("POST", "/buckets/foo/artifacts", http.StatusOK, `{"Name": "artifact"}`)

	b, _ := client.NewBucket("foo", "bar", 32)
	sa, err := b.NewChunkedArtifact("artifact")
	assert.NotNil(t, sa)
	assert.NoError(t, err)

	// Cancel the context to prevent any further requests
	cancel()

	{
		err := sa.AppendLog("console contents")
		assert.NoError(t, err)
		err = sa.Close()
		assert.Error(t, err)
		assert.False(t, err.IsRetriable())
	}
}

func TestArtifactURL(t *testing.T) {
	client := NewArtifactStoreClient("http://foo")
	bucket := &Bucket{
		bucket: &model.Bucket{
			Id: "bkt",
		},
		client: client,
	}

	{
		streamedArtifact := &StreamedArtifact{
			ArtifactImpl: &ArtifactImpl{
				artifact: &model.Artifact{
					Name: "safct",
				},
				bucket: bucket,
			},
		}
		assert.Equal(t, "http://foo/buckets/bkt/artifacts/safct/content", streamedArtifact.GetContentURL())
	}

	{
		chunkedArtifact := &ChunkedArtifact{
			ArtifactImpl: &ArtifactImpl{
				artifact: &model.Artifact{
					Name: "cafct",
				},
				bucket: bucket,
			},
		}
		assert.Equal(t, "http://foo/buckets/bkt/artifacts/cafct/content", chunkedArtifact.GetContentURL())
	}
}
