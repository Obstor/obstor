/*
 * MinIO Cloud Storage, (C) 2016 MinIO, Inc.
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

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"

	"github.com/obstor/obstor/browser"
	xjwt "github.com/obstor/obstor/cmd/jwt"
	"github.com/obstor/obstor/cmd/logger"
	jsonrpc "github.com/obstor/obstor/pkg/rpc"
	"github.com/obstor/obstor/pkg/rpc/json2"
)

// webAPI container for Web API.
type webAPIHandlers struct {
	ObjectAPI func() ObjectLayer
	CacheAPI  func() CacheObjectLayer
}

// indexHandler - Handler to serve index.html
type indexHandler struct {
	handler http.Handler
}

func (h indexHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.URL.Path = obstorReservedBucketPath + SlashSeparator
	h.handler.ServeHTTP(w, r)
}

const assetPrefix = "production"

// specialAssets are files which are unique files not embedded inside index_bundle.js.
const specialAssets = "index_bundle.*.js|loader.css|logo.svg|firefox.png|safari.png|chrome.png|favicon-16x16.png|favicon-32x32.png|favicon-96x96.png"

// RPC body limit before json2 decodes unauthenticated args
const webRPCMaxBodySize = 1 << 20

// Buffer up to body limit, reply HTTP 400 and json2 error above it
func capWebRPCBody(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, webRPCMaxBodySize))
		if err != nil {
			code := "IncompleteBody"
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				code = "EntityTooLarge"
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": json2.Version,
				"id":      nil,
				"error":   &json2.Error{Code: json2.E_PARSE, Message: err.Error(), Data: map[string]string{"code": code}},
			})
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		h.ServeHTTP(w, r)
	})
}

type webRPCAuthKey struct{}

type webRPCAuth struct {
	claims *xjwt.MapClaims
	err    error
}

// Authenticate before SetAuth rotates token's signing secret
func withWebRPCAuth(r *http.Request) *http.Request {
	claims, _, err := webRequestAuthenticate(r)
	return withWebActivityGate(r.WithContext(context.WithValue(r.Context(), webRPCAuthKey{}, webRPCAuth{claims, err})))
}

// Limit body, then authenticate before handler
func webRPCHandler(s *jsonrpc.Server) http.Handler {
	return capWebRPCBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.ServeHTTP(w, withWebRPCAuth(r))
	}))
}

// After RPC, set audit vars, trace, audit log, then activity log
func webRPCAfter(ri *jsonrpc.RequestInfo) {
	if ri == nil {
		return
	}
	auth, ok := ri.Request.Context().Value(webRPCAuthKey{}).(webRPCAuth)
	if !ok {
		auth.err = errNoAuthToken
	}
	claims, authErr := auth.claims, auth.err
	bucketName, objectName := extractBucketObject(ri.Args)
	ri.Request = mux.SetURLVars(ri.Request, map[string]string{
		"bucket": bucketName,
		"object": objectName,
	})
	if globalTrace.NumSubscribers() > 0 {
		globalTrace.Publish(WebTrace(ri))
	}
	ctx := newContext(ri.Request, ri.ResponseWriter, ri.Method)
	logger.AuditLog(ctx, ri.ResponseWriter, ri.Request, claims.Map())
	accessKey := ""
	if authErr == nil {
		accessKey = claims.AccessKey
	}
	recordWebActivity(ctx, strings.TrimPrefix(ri.Method, "web."), ri.Args, accessKey, webActivityGatePassed(ri.Request), ri.Error)
}

// registerWebRPCRouter register JSON-RPC upload/download handlers
func registerWebRPCRouter(router *mux.Router) error {
	web := &webAPIHandlers{
		ObjectAPI: newObjectLayerFn,
		CacheAPI:  newCachedObjectLayerFn,
	}

	codec := json2.NewCodec()
	webBrowserRouter := router.PathPrefix(obstorReservedBucketPath).HeadersRegexp("User-Agent", ".*Mozilla.*").Subrouter()

	webRPC := jsonrpc.NewServer()
	webRPC.RegisterCodec(codec, "application/json")
	webRPC.RegisterCodec(codec, "application/json; charset=UTF-8")
	webRPC.RegisterAfterFunc(webRPCAfter)

	if err := webRPC.RegisterService(web, "web"); err != nil {
		return err
	}

	webBrowserRouter.Methods(http.MethodPost).Path("/webrpc").Handler(webRPCHandler(webRPC))
	webBrowserRouter.Methods(http.MethodPut).Path("/upload/{bucket}/{object:.+}").HandlerFunc(httpTraceHdrs(web.Upload))
	webBrowserRouter.Methods(http.MethodGet).Path("/download/{bucket}/{object:.+}").Queries("token", "{token:.*}").HandlerFunc(httpTraceHdrs(web.Download))
	webBrowserRouter.Methods(http.MethodPost).Path("/zip").Queries("token", "{token:.*}").HandlerFunc(httpTraceHdrs(web.DownloadZip))

	return nil
}

// registerWebRouter - registers web router for serving obstor browser.
func registerWebRouter(router *mux.Router) error {
	// Initialize Web.
	web := &webAPIHandlers{
		ObjectAPI: newObjectLayerFn,
		CacheAPI:  newCachedObjectLayerFn,
	}

	// Initialize a new json2 codec.
	codec := json2.NewCodec()

	// Obstor browser router.
	webBrowserRouter := router.PathPrefix(obstorReservedBucketPath).HeadersRegexp("User-Agent", ".*Mozilla.*").Subrouter()

	// Initialize json rpc handlers.
	webRPC := jsonrpc.NewServer()
	webRPC.RegisterCodec(codec, "application/json")
	webRPC.RegisterCodec(codec, "application/json; charset=UTF-8")
	webRPC.RegisterAfterFunc(webRPCAfter)

	// Register RPC handlers with server
	if err := webRPC.RegisterService(web, "web"); err != nil {
		return err
	}

	// RPC handler at URI - /obstor/webrpc
	webBrowserRouter.Methods(http.MethodPost).Path("/webrpc").Handler(webRPCHandler(webRPC))
	webBrowserRouter.Methods(http.MethodPut).Path("/upload/{bucket}/{object:.+}").HandlerFunc(httpTraceHdrs(web.Upload))

	// These methods use short-expiry tokens in the URLs. These tokens may unintentionally
	// be logged, so a new one must be generated for each request.
	webBrowserRouter.Methods(http.MethodGet).Path("/download/{bucket}/{object:.+}").Queries("token", "{token:.*}").HandlerFunc(httpTraceHdrs(web.Download))
	webBrowserRouter.Methods(http.MethodPost).Path("/zip").Queries("token", "{token:.*}").HandlerFunc(httpTraceHdrs(web.DownloadZip))

	// Create compressed assets handler
	assetFS, err := fs.Sub(browser.GetStaticAssets(), assetPrefix)
	if err != nil {
		panic(err)
	}
	compressAssets := handlers.CompressHandler(http.StripPrefix(obstorReservedBucketPath, http.FileServer(http.FS(assetFS))))

	// Serve javascript files and favicon from assets.
	webBrowserRouter.Path(fmt.Sprintf("/{assets:%s}", specialAssets)).Handler(compressAssets)

	// Serve index.html from assets for rest of the requests.
	webBrowserRouter.Path("/{index:.*}").Handler(indexHandler{compressAssets})

	return nil
}
