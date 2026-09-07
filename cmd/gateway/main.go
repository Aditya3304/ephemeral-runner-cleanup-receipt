// The gateway has no credentials, archive access, or signing trust. Its only
// upstream is the private API; Docker publishes only the laptop loopback port.
package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	target, _ := url.Parse("http://api:8080")
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext, ResponseHeaderTimeout: 48 * time.Second, MaxIdleConnsPerHost: 4}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(503)
		w.Write([]byte("{\"error\":\"api_unavailable\"}\n"))
	}
	server := &http.Server{Addr: ":8080", Handler: gatewayHandler(proxy, os.DirFS("/ui")), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 50 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(c)
	}()
	if e := server.ListenAndServe(); e != nil && !errors.Is(e, http.ErrServerClosed) {
		log.Print("Local gateway unavailable")
		os.Exit(1)
	}
}
