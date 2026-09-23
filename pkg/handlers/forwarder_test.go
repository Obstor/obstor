/*
 * PGG Obstor, (C) 2026 PGG, Inc.
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

package handlers

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

type forwarderRoundTripper func(*http.Request) (*http.Response, error)

func (fn forwarderRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestForwarder(t *testing.T) {
	for _, test := range []struct {
		name       string
		passHost   bool
		requestURI bool
		headers    http.Header
		wantHost   string
		want       http.Header
	}{
		{
			name:       "forwarding-chain",
			passHost:   true,
			requestURI: true,
			headers: http.Header{
				"Forwarded":         {"for=192.0.2.1;proto=https"},
				"X-Forwarded-For":   {"192.0.2.1", "192.0.2.2"},
				"X-Forwarded-Host":  {"public.example"},
				"X-Forwarded-Proto": {"https"},
				"X-Forwarded-Port":  {"443"},
				"X-Real-Ip":         {"192.0.2.1"},
			},
			wantHost: "original.example:8443",
			want: http.Header{
				"Forwarded":         {"for=192.0.2.1;proto=https"},
				"X-Forwarded-For":   {"192.0.2.1, 192.0.2.2, 192.0.2.3"},
				"X-Forwarded-Host":  {"public.example"},
				"X-Forwarded-Proto": {"https"},
				"X-Forwarded-Port":  {"443"},
				"X-Real-Ip":         {"192.0.2.1"},
			},
		},
		{
			name:     "url-without-request-uri",
			wantHost: "node.example:9000",
			want: http.Header{
				"X-Forwarded-For":   {"192.0.2.3"},
				"X-Forwarded-Host":  {"node.example:9000"},
				"X-Forwarded-Proto": {"https"},
				"X-Forwarded-Port":  {"9000"},
				"X-Real-Ip":         {"192.0.2.3"},
			},
		},
		{
			name:     "omit-forwarded-for",
			passHost: true,
			headers:  http.Header{"X-Forwarded-For": nil},
			wantHost: "original.example:8443",
			want:     http.Header{"X-Forwarded-For": nil},
		},
		{
			name:       "hop-by-hop-headers",
			passHost:   true,
			requestURI: true,
			headers: http.Header{
				"Connection":        {"X-Forwarded-For, X-Forwarded-Host, X-Forwarded-Proto, Forwarded, X-Hop"},
				"Forwarded":         {"for=192.0.2.1"},
				"X-Forwarded-For":   {"192.0.2.1"},
				"X-Forwarded-Host":  {"removed.example"},
				"X-Forwarded-Proto": {"http"},
				"X-Hop":             {"removed"},
			},
			wantHost: "original.example:8443",
			want: http.Header{
				"X-Forwarded-For":   {"192.0.2.3"},
				"X-Forwarded-Host":  {"original.example:8443"},
				"X-Forwarded-Proto": {"https"},
				"Forwarded":         nil,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := &http.Request{
				Method: http.MethodGet,
				URL: &url.URL{
					Scheme:   "http",
					Host:     "node.example:9000",
					Path:     "/bucket/key/part",
					RawPath:  "/bucket/key%2Fpart",
					RawQuery: "X-Amz-Signature=signature&key=a;b&invalid=%zz",
				},
				Host:       "original.example:8443",
				Header:     make(http.Header),
				RemoteAddr: "192.0.2.3:1234",
				TLS:        &tls.ConnectionState{},
			}
			if test.requestURI {
				req.RequestURI = req.URL.RequestURI()
			}
			for name, values := range test.headers {
				req.Header[name] = values
			}
			req.Header.Set("Authorization", "AWS4-HMAC-SHA256 signed-request")
			original := req.Clone(req.Context())
			called := false
			f := NewForwarder(&Forwarder{
				PassHost: test.passHost,
				RoundTripper: forwarderRoundTripper(func(out *http.Request) (*http.Response, error) {
					called = true
					if *out.URL != *original.URL {
						t.Errorf("forwarded URL = %v, want %v", out.URL, original.URL)
					}
					if out.Host != test.wantHost || out.RequestURI != "" {
						t.Errorf("Host = %q, RequestURI = %q; want %q and empty", out.Host, out.RequestURI, test.wantHost)
					}
					for name, want := range test.want {
						if !reflect.DeepEqual(out.Header[name], want) {
							t.Errorf("%s = %v, want %v", name, out.Header[name], want)
						}
					}
					if out.Header.Get("Authorization") != original.Header.Get("Authorization") {
						t.Error("authorization header changed")
					}
					if out.Header.Get("Connection") != "" || out.Header.Get("X-Hop") != "" {
						t.Error("hop-by-hop headers were forwarded")
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader("forwarded")),
					}, nil
				}),
			})
			response := httptest.NewRecorder()
			f.ServeHTTP(response, req)
			if !called || response.Code != http.StatusOK || response.Body.String() != "forwarded" {
				t.Fatalf("unexpected proxy response: %d %q", response.Code, response.Body.String())
			}
			if !reflect.DeepEqual(req.Header, original.Header) || *req.URL != *original.URL || req.Host != original.Host || req.RequestURI != original.RequestURI {
				t.Error("forwarder modified the incoming request")
			}
		})
	}
}
