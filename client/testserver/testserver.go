package testserver

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type request struct {
	method        string
	url           string
	responseCode  int
	responseBytes string
	shouldHang    bool
	next          *request
}

// TestServer wraps around httptest.Server to support expectations and timeout tests.
// Use ExpectAndRespond and ExpectAndHang methods to indicate expected requests in the order they
// should arrive.
//
// Currently, parallel requests are not supported.
type TestServer struct {
	reqChain     *request
	t            *testing.T
	s            *httptest.Server
	URL          string
	waiter       *sync.Cond
	reqChainLock sync.Mutex
}

// NewTestServer creates a new TestServer associate with given testing.T instance
func NewTestServer(t *testing.T) *TestServer {
	var m sync.Mutex
	ts := &TestServer{t: t, waiter: sync.NewCond(&m)}
	ts.run()
	ts.URL = ts.s.URL
	return ts
}

// ExpectAndRespond specifies the next request to be expected (using request method and url) and
// specifies what response needs to be provided.
func (ts *TestServer) ExpectAndRespond(method string, url string, responseCode int, responseBytes string) *TestServer {
	return ts.insertNextReq(request{
		method:        method,
		url:           url,
		responseCode:  responseCode,
		responseBytes: responseBytes,
		shouldHang:    false,
	})
}

// ExpectAndHang specifies the next request expected, the server hangs and the request will not be
// responded to. This is useful to test client timeouts. To stop the hanging server, call
// CloseAndAssertExpectations.
func (ts *TestServer) ExpectAndHang(method string, url string) *TestServer {
	return ts.insertNextReq(request{
		method:     method,
		url:        url,
		shouldHang: true,
	})
}

func (ts *TestServer) insertNextReq(nextReq request) *TestServer {
	defer ts.reqChainLock.Unlock()
	ts.reqChainLock.Lock()

	if ts.reqChain == nil {
		ts.reqChain = &nextReq
	} else {
		req := ts.reqChain

		for req.next != nil {
			req = req.next
		}

		req.next = &nextReq
	}

	return ts
}

func (ts *TestServer) run() {
	ts.s = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts.t.Logf("Received request: %s %s\n", r.Method, r.URL)

		ts.reqChainLock.Lock()
		nextReq := ts.reqChain
		ts.reqChain = ts.reqChain.next
		ts.reqChainLock.Unlock()

		if nextReq == nil {
			w.WriteHeader(http.StatusExpectationFailed)
			ts.t.Errorf("Unexpected request %s %s\n", r.Method, r.URL)
			return
		}

		if nextReq.method != r.Method || nextReq.url != r.URL.String() {
			w.WriteHeader(http.StatusExpectationFailed)
			ts.t.Errorf("Expected request: %s %s\nGot request: %s %s", nextReq.method, nextReq.url, r.Method, r.URL)
			return
		}

		if nextReq.shouldHang {
			ts.t.Log("Hanging on response")
			ts.waiter.L.Lock()
			ts.waiter.Wait()
			ts.waiter.L.Unlock()
		} else {
			ts.t.Logf("Responding with status %d\n", nextReq.responseCode)
			w.WriteHeader(nextReq.responseCode)
			w.Write([]byte(nextReq.responseBytes))
		}
	}))

	ts.t.Logf("Running httptest server on %s\n", ts.s.URL)
}

// CloseAndAssertExpectations will stop any hanging requests and shutdown the test server. Any
// remaining expectations will flag a test error.
func (ts *TestServer) CloseAndAssertExpectations() {
	ts.reqChainLock.Lock()
	if ts.reqChain != nil {
		ts.t.Errorf("Some expected requests were never called, next one being %s %s",
			ts.reqChain.method, ts.reqChain.url)
	}
	ts.reqChainLock.Unlock()

	ts.waiter.L.Lock()
	ts.waiter.Broadcast()
	ts.waiter.L.Unlock()
	ts.s.Close()
}
