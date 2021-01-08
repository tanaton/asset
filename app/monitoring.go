package app

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"time"
)

type ResponseInfo struct {
	uri       string
	userAgent string
	status    int
	size      int
	start     time.Time
	end       time.Time
	method    string
	host      string
	protocol  string
	addr      string
}
type ResultMonitor struct {
	err                 error
	ResponseTimeSum     time.Duration
	ResponseCount       uint
	ResponseCodeOkCount uint
	ResponseCodeNgCount uint
}
type MonitoringResponseWriter struct {
	http.ResponseWriter
	ri   ResponseInfo
	rich chan<- ResponseInfo
}
type MonitoringResponseWriterWithCloseNotify struct {
	*MonitoringResponseWriter
}

// MonitoringHandler モニタリング用ハンドラ生成
func MonitoringHandler(h http.Handler, rich chan<- ResponseInfo) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mrw := newMonitoringResponseWriter(w, r, rich)
		defer mrw.Close()
		if _, ok := w.(http.CloseNotifier); ok {
			mrwcn := MonitoringResponseWriterWithCloseNotify{mrw}
			h.ServeHTTP(mrwcn, r)
		} else {
			h.ServeHTTP(mrw, r)
		}
	})
}

func newMonitoringResponseWriter(w http.ResponseWriter, r *http.Request, rich chan<- ResponseInfo) *MonitoringResponseWriter {
	return &MonitoringResponseWriter{
		ResponseWriter: w,
		ri: ResponseInfo{
			uri:       r.RequestURI,
			userAgent: r.UserAgent(),
			start:     time.Now().UTC(),
			method:    r.Method,
			protocol:  r.Proto,
			host:      r.Host,
			addr:      r.RemoteAddr,
		},
		rich: rich,
	}
}

// Write メソッドをオーバーライド
func (mrw *MonitoringResponseWriter) Write(buf []byte) (int, error) {
	if mrw.ri.status == 0 {
		mrw.ri.status = http.StatusOK
	}
	s, err := mrw.ResponseWriter.Write(buf)
	mrw.ri.size += s
	return s, err
}

// WriteHeader メソッドをオーバーライド
func (mrw *MonitoringResponseWriter) WriteHeader(statusCode int) {
	mrw.ri.status = statusCode
	mrw.ResponseWriter.WriteHeader(statusCode)
}

// Close io.Closerのような感じにしたけど特に意味は無い
func (mrw *MonitoringResponseWriter) Close() error {
	mrw.ri.end = time.Now().UTC()
	mrw.rich <- mrw.ri
	return nil
}

// インターフェイスのチェック
var _ http.ResponseWriter = &MonitoringResponseWriter{}
var _ http.CloseNotifier = &MonitoringResponseWriterWithCloseNotify{}
var _ http.Hijacker = &MonitoringResponseWriter{}
var _ http.Flusher = &MonitoringResponseWriter{}
var _ http.Pusher = &MonitoringResponseWriter{}

// CloseNotify http.CloseNotifier interface
func (mrw *MonitoringResponseWriterWithCloseNotify) CloseNotify() <-chan bool {
	return mrw.ResponseWriter.(http.CloseNotifier).CloseNotify()
}

// Hijack implements http.Hijacker. If the underlying ResponseWriter is a
// Hijacker, its Hijack method is returned. Otherwise an error is returned.
func (mrw *MonitoringResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := mrw.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, fmt.Errorf("http.Hijacker interface is not supported")
}

// Flush http.Flusher interface
func (mrw *MonitoringResponseWriter) Flush() {
	flusher, ok := mrw.ResponseWriter.(http.Flusher)
	if ok {
		flusher.Flush()
	}
}

// Push http.Pusher interface
// go1.8以上が必要
func (mrw *MonitoringResponseWriter) Push(target string, opts *http.PushOptions) error {
	pusher, ok := mrw.ResponseWriter.(http.Pusher)
	if ok && pusher != nil {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}
