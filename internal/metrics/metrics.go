// Package metrics keeps a few process counters and renders them in the Prometheus text
// format. No external dependency: the label sets are small and fixed.
package metrics

import (
	"fmt"
	"io"
	"sort"
	"sync"
	"sync/atomic"
)

type Registry struct {
	mu       sync.Mutex
	counters map[string]*atomic.Int64 // key: name{labels}
	httpSum  atomic.Int64             // nanoseconds
	httpN    atomic.Int64
}

func New() *Registry { return &Registry{counters: map[string]*atomic.Int64{}} }

func (r *Registry) counter(name string, labels string) *atomic.Int64 {
	key := name
	if labels != "" {
		key += "{" + labels + "}"
	}
	r.mu.Lock()
	c, ok := r.counters[key]
	if !ok {
		c = &atomic.Int64{}
		r.counters[key] = c
	}
	r.mu.Unlock()
	return c
}

// Inc adds delta to a counter, e.g. Inc("filezam_logins_total", `result="ok"`, 1).
func (r *Registry) Inc(name, labels string, delta int64) { r.counter(name, labels).Add(delta) }

// HTTP records one request. Status classes keep cardinality low.
func (r *Registry) HTTP(method string, status int, nanos int64) {
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
	default:
		method = "other"
	}
	r.Inc("filezam_http_requests_total", fmt.Sprintf(`method=%q,code=%q`, method, fmt.Sprintf("%dxx", status/100)), 1)
	r.httpSum.Add(nanos)
	r.httpN.Add(1)
}

// Gauge is a value computed at scrape time.
type Gauge struct {
	Name, Help, Labels string
	Value              float64
}

// Write renders every counter plus the gauges in exposition format.
func (r *Registry) Write(w io.Writer, gauges []Gauge) {
	r.mu.Lock()
	keys := make([]string, 0, len(r.counters))
	for k := range r.counters {
		keys = append(keys, k)
	}
	r.mu.Unlock()
	sort.Strings(keys)
	lastName := ""
	for _, k := range keys {
		name := k
		if i := indexByte(k, '{'); i >= 0 {
			name = k[:i]
		}
		if name != lastName {
			fmt.Fprintf(w, "# TYPE %s counter\n", name)
			lastName = name
		}
		r.mu.Lock()
		v := r.counters[k].Load()
		r.mu.Unlock()
		fmt.Fprintf(w, "%s %d\n", k, v)
	}
	fmt.Fprintf(w, "# TYPE filezam_http_request_seconds summary\nfilezam_http_request_seconds_sum %g\nfilezam_http_request_seconds_count %d\n", float64(r.httpSum.Load())/1e9, r.httpN.Load())
	for _, g := range gauges {
		if g.Help != "" {
			fmt.Fprintf(w, "# HELP %s %s\n", g.Name, g.Help)
		}
		fmt.Fprintf(w, "# TYPE %s gauge\n", g.Name)
		if g.Labels != "" {
			fmt.Fprintf(w, "%s{%s} %g\n", g.Name, g.Labels, g.Value)
		} else {
			fmt.Fprintf(w, "%s %g\n", g.Name, g.Value)
		}
	}
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
