/*
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

package rest

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// Internode calls run over a trusted channel with header-only auth
func TestCallDoesNotSendExpectContinue(t *testing.T) {
	var gotExpect string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotExpect = r.Header.Get("Expect")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	// Non-zero ExpectContinueTimeout mirrors prod
	tr := &http.Transport{ExpectContinueTimeout: 15 * time.Second}
	c := NewClient(u, tr, func(string) string { return "" })

	body := []byte("hello-internode")
	rc, err := c.Call(context.Background(), "/test", url.Values{}, bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	io.Copy(io.Discard, rc)
	rc.Close()

	if gotExpect != "" {
		t.Fatalf("server saw Expect header %q; internode calls must not send 100-continue", gotExpect)
	}
	if string(gotBody) != string(body) {
		t.Fatalf("server got body %q, want %q", gotBody, body)
	}
}
