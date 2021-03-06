package stats

import (
	"expvar"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/quipo/statsd"
)

// Stat holds a named integer counter, which is exported to expvar and statsd
type Stat struct {
	key string
	exp *expvar.Int
}

// NewStat creates a named statistic with the given name
func NewStat(key string) *Stat {
	return &Stat{key, expvar.NewInt(key)}
}

// Add increments the integer value stored in this stat
func (v *Stat) Add(delta int64) {
	v.exp.Add(delta)
	lock.RLock()
	defer lock.RUnlock()
	stats.Incr(v.key, delta)
}

func (v *Stat) String() string {
	return v.exp.String()
}

// TimingStat holds a timer statistic, internally represented as two counters, total time consumed
// and number of observations.
type TimingStat struct {
	timeNs, counter *Stat
}

// NewTimingStat creates a timing statistic with the given name
func NewTimingStat(key string) *TimingStat {
	timeNs := NewStat(key + "_time_ns")
	counter := NewStat(key + "_count")
	return &TimingStat{timeNs: timeNs, counter: counter}
}

// AddTimeSince calculates time elapsed since given start time and adds it to the stat.
func (ts *TimingStat) AddTimeSince(start time.Time) {
	ts.Add(time.Since(start))
}

// Add notes a timing event of given duration (increases observation count by one and adds time
// duration to total time spent in event)
func (ts *TimingStat) Add(delta time.Duration) {
	ts.counter.Add(1)
	ts.timeNs.Add(delta.Nanoseconds())
}

// Interval between batched stats updates pushed to the statsd instance.
const updateInterval = 5 * time.Second

var requestCounter = NewStat("requests")
var lock sync.RWMutex
var noopClient = &statsd.NoopClient{}
var stats statsd.Statsd = noopClient

// CreateStatsdClient creates a local instances of a statsd client. Any errors will be logged to
// console and ignored.
func CreateStatsdClient(statsdURL, statsdPrefix string) error {
	lock.Lock()
	defer lock.Unlock()

	if stats != noopClient {
		// Already initialized. Don't overwrite
		return nil
	}

	if statsdURL != "" {
		hostname, err := os.Hostname()
		if err != nil {
			log.Printf("Could not read hostname. Using default noop statsd client: %s", err)
			return err
		}
		prefix := fmt.Sprintf("%s.%s.artifacts.", statsdPrefix, hostname)

		statsdClient := statsd.NewStatsdClient(statsdURL, prefix)

		if statsdClient != nil {
			stats = statsd.NewStatsdBuffer(updateInterval, statsdClient)
		}
	} else {
		log.Println("No statsd URL provided. Using default noop statsd client")
	}

	return nil
}

// ShutdownStatsdClient flushes any outstanding stats and terminates connections to statsd.
func ShutdownStatsdClient() {
	stats.Close()
}

// Counter counts number of requests made to the server
func Counter() gin.HandlerFunc {
	return func(_ *gin.Context) {
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
