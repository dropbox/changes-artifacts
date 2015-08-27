package stats

import (
	"expvar"
	"fmt"
	"net/http"

	"github.com/go-martini/martini"
)

var requestCounter = expvar.NewInt("requests")

// Counter counts number of requests made to the server
func Counter() martini.Handler {
	return func(res http.ResponseWriter, req *http.Request, c martini.Context) {
		requestCounter.Add(1)
	}
}

// Handler display a JSON object showing number of requests received
// Copied from https://golang.org/src/expvar/expvar.go#L305
func Handler(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", "application/json; charset=utf-8")
	fmt.Fprintf(res, "{\n")
	first := true
	expvar.Do(func(kv expvar.KeyValue) {
		if !first {
			fmt.Fprintf(res, ",\n")
		}
		first = false
		fmt.Fprintf(res, "%q: %s", kv.Key, kv.Value)
	})
	fmt.Fprintf(res, "\n}\n")
}
