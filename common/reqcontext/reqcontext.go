package reqcontext

import (
	"net/http"

	"golang.org/x/net/context"

	"github.com/go-martini/martini"
)

// Unexported to avoid collisions
type key int

const requestKey key = 0

func withReq(ctx context.Context, req *http.Request) context.Context {
	return context.WithValue(ctx, requestKey, req)
}

// ReqFromContext fetches an instance of http.Request embedded in given context.Context.
func ReqFromContext(ctx context.Context) (*http.Request, bool) {
	r, ok := ctx.Value(requestKey).(*http.Request)
	return r, ok
}

// ContextHandler returns a middleware handler that populates a Context instance with current
// request and sentry information. There are no requirements on where the handler is to be installed
// in the handler chain.
func ContextHandler(rootCtx context.Context) martini.Handler {
	return func(res http.ResponseWriter, req *http.Request, c martini.Context) {
		c.Map(withReq(rootCtx, req))
	}
}
