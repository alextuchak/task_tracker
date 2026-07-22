package integration

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
)

type emailServiceMock struct {
	srv      *httptest.Server
	received atomic.Int64
	failing  atomic.Bool
}

func newEmailServiceMock() *emailServiceMock {
	f := &emailServiceMock{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.failing.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		f.received.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	return f
}
