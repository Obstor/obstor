/*
 * MinIO Cloud Storage, (C) 2017, 2018 MinIO, Inc.
 * PGG Obstor, (C) 2021-2026 PGG, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package http

import (
	"crypto/tls"
	"errors"
	"net/http"
	"os"
	"runtime/pprof"
	"sync"
	"sync/atomic"
	"time"

	humanize "github.com/dustin/go-humanize"

	"github.com/obstor/obstor-go/v7/pkg/set"
	"github.com/obstor/obstor/cmd/config"
	"github.com/obstor/obstor/cmd/config/api"
	"github.com/obstor/obstor/pkg/certs"
	"github.com/obstor/obstor/pkg/env"
	"github.com/obstor/obstor/pkg/fips"
)

const (
	shutdownPollInterval = 500 * time.Millisecond

	// Close established sessions gracefully before shitting down
	GracefulShutdownTimeout = 5 * time.Second

	// Close keep-alive past this duration
	ConnIdleTimeout = 30 * time.Second

	// How long we wait to transit requests (block slowloris attacks)
	HeaderReadTimeout = 30 * time.Second

	// Max byte size of HTTP headers (block memory abuse)
	MaxRequestHeaderBytes = 1 * humanize.MiByte
)

// Server - extended http.Server supports multiple addresses to serve and enhanced connection handling.
type Server struct {
	http.Server
	Addrs           []string      // addresses on which the server listens for new connection.
	ShutdownTimeout time.Duration // timeout used for graceful server shutdown.
	listenerMutex   sync.Mutex    // to guard 'listener' field.
	listener        *httpListener // HTTP listener for all 'Addrs' field.
	inShutdown      uint32        // indicates whether the server is in shutdown or not
	requestCount    int32         // counter holds no. of request in progress.
}

// GetRequestCount - returns number of request in progress.
func (srv *Server) GetRequestCount() int {
	return int(atomic.LoadInt32(&srv.requestCount))
}

// Start - start HTTP server
func (srv *Server) Start() (err error) {
	// Take a copy of server fields.
	var tlsConfig *tls.Config
	if srv.TLSConfig != nil {
		tlsConfig = srv.TLSConfig.Clone()
	}
	handler := srv.Handler // if srv.Handler holds non-synced state -> possible data race

	addrs := set.CreateStringSet(srv.Addrs...).ToSlice() // copy and remove duplicates

	// Create new HTTP listener.
	var listener *httpListener
	listener, err = newHTTPListener(
		addrs,
	)
	if err != nil {
		return err
	}

	// Wrap given handler to do additional
	// * return 503 (service unavailable) if the server in shutdown.
	wrappedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If server is in shutdown.
		if atomic.LoadUint32(&srv.inShutdown) != 0 {
			// To indicate disable keep-alives
			w.Header().Set("Connection", "close")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(http.ErrServerClosed.Error()))
			w.(http.Flusher).Flush()
			return
		}

		atomic.AddInt32(&srv.requestCount, 1)
		defer atomic.AddInt32(&srv.requestCount, -1)

		// Handle request using passed handler.
		handler.ServeHTTP(w, r)
	})

	srv.listenerMutex.Lock()
	srv.Handler = wrappedHandler
	srv.listener = listener
	srv.listenerMutex.Unlock()

	// Start servicing with listener.
	if tlsConfig != nil {
		return srv.Serve(tls.NewListener(listener, tlsConfig))
	}
	return srv.Serve(listener)
}

// Shutdown - shuts down HTTP server.
func (srv *Server) Shutdown() error {
	srv.listenerMutex.Lock()
	if srv.listener == nil {
		srv.listenerMutex.Unlock()
		return http.ErrServerClosed
	}
	srv.listenerMutex.Unlock()

	if atomic.AddUint32(&srv.inShutdown, 1) > 1 {
		// Shutdown in progress
		return http.ErrServerClosed
	}

	// Close underneath HTTP listener.
	srv.listenerMutex.Lock()
	err := srv.listener.Close()
	srv.listenerMutex.Unlock()
	if err != nil {
		return err
	}

	// Wait for opened connection to be closed up to Shutdown timeout.
	shutdownTimeout := srv.ShutdownTimeout
	shutdownTimer := time.NewTimer(shutdownTimeout)
	ticker := time.NewTicker(shutdownPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-shutdownTimer.C:
			// Write all running goroutines.
			tmp, err := os.CreateTemp("", "obstor-goroutines-*.txt")
			if err == nil {
				_ = pprof.Lookup("goroutine").WriteTo(tmp, 1)
				_ = tmp.Close()
				return errors.New("timed out. some connections are still active. goroutines written to " + tmp.Name())
			}
			return errors.New("timed out. some connections are still active")
		case <-ticker.C:
			if atomic.LoadInt32(&srv.requestCount) <= 0 {
				return nil
			}
		}
	}
}

// NewServer - creates new HTTP server using given arguments.
func NewServer(addrs []string, handler http.Handler, getCert certs.GetCertificateFunc) *Server {
	secureCiphers := env.Get(api.EnvAPISecureCiphers, config.EnableOn) == config.EnableOn

	var tlsConfig *tls.Config
	if getCert != nil {
		tlsConfig = &tls.Config{
			MinVersion:     tls.VersionTLS12,
			NextProtos:     []string{"http/1.1", "h2"},
			GetCertificate: getCert,
		}
		if secureCiphers || fips.Enabled() {
			tlsConfig.CipherSuites = fips.CipherSuitesTLS()
			tlsConfig.CurvePreferences = fips.EllipticCurvesTLS()
		} else {
			// Reject deprecated algorithms such as 3DES/RC4.
			rejected := make(map[uint16]struct{})
			for _, weak := range tls.InsecureCipherSuites() {
				rejected[weak.ID] = struct{}{}
			}
			allowed := make([]uint16, 0, len(tls.CipherSuites()))
			for _, cs := range tls.CipherSuites() {
				if _, blocked := rejected[cs.ID]; !blocked {
					allowed = append(allowed, cs.ID)
				}
			}
			tlsConfig.CipherSuites = allowed
		}
	}

	httpServer := &Server{
		Addrs:           addrs,
		ShutdownTimeout: GracefulShutdownTimeout,
	}
	httpServer.Handler = handler
	httpServer.TLSConfig = tlsConfig
	httpServer.MaxHeaderBytes = MaxRequestHeaderBytes
	httpServer.IdleTimeout = ConnIdleTimeout
	httpServer.ReadHeaderTimeout = HeaderReadTimeout

	return httpServer
}
