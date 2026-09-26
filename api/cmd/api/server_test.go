package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A handler must never still be running (and committing) when the server
// gives up on writing its response: the client then sees a dropped
// connection for a write that succeeded, retries, and gets a spurious 409.
// So every request carries a deadline that ends well before WriteTimeout,
// and the DB work under it is cancelled (rolled back) instead.
func TestServerTimeouts_RequestDeadlineEndsBeforeWriteTimeout(t *testing.T) {
	to := defaultTimeouts()
	srv := newHTTPServer(":0", http.NotFoundHandler(), to)

	assert.Equal(t, to.Write, srv.WriteTimeout)
	assert.Greater(t, to.Request, time.Duration(0), "requests must have a deadline")
	assert.GreaterOrEqual(t, srv.WriteTimeout-to.Request, 10*time.Second,
		"WriteTimeout must leave room to write the response after the request deadline")
	assert.GreaterOrEqual(t, to.Request, 30*time.Second,
		"the write path (content save + version archive + task reconcile) needs headroom")
	assert.NotZero(t, srv.ReadHeaderTimeout)
}

func TestRequestDeadlineMiddleware_SetsContextDeadline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(requestDeadline(time.Minute))
	var deadline time.Time
	var ok bool
	r.GET("/", func(c *gin.Context) {
		deadline, ok = c.Request.Context().Deadline()
		c.Status(http.StatusNoContent)
	})

	base := serve(t, newHTTPServer("127.0.0.1:0", r, defaultTimeouts()))
	resp, err := http.Get(base + "/")
	require.NoError(t, err)
	resp.Body.Close()

	require.True(t, ok, "handler context has a deadline")
	assert.WithinDuration(t, time.Now().Add(time.Minute), deadline, 5*time.Second)
}

// End to end with short timeouts: a handler stuck on the database until its
// context is cancelled still gets its error response out, rather than the
// server closing the connection on it.
func TestServerTimeouts_SlowHandlerGetsResponseOut(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(requestDeadline(100 * time.Millisecond))
	r.PUT("/slow", func(c *gin.Context) {
		<-c.Request.Context().Done() // e.g. pgx returning context.DeadlineExceeded
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "timeout"})
	})

	to := timeouts{Read: time.Second, ReadHeader: time.Second, Write: time.Second, Idle: time.Second, Request: 100 * time.Millisecond}
	base := serve(t, newHTTPServer("127.0.0.1:0", r, to))

	req, _ := http.NewRequest(http.MethodPut, base+"/slow", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "response must arrive, not a dropped connection")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func serve(t *testing.T, srv *http.Server) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("serve: %v", err)
		}
	}()
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })
	return "http://" + ln.Addr().String()
}
