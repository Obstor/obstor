package cmd

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/obstor/obstor/cmd/rest"
	xnet "github.com/obstor/obstor/pkg/net"
)

// Real peer client, auth and serialization through local transport, can simulate older v14 peer
type peerHandlerTransport struct{ handler http.Handler }

func (p peerHandlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	p.handler.ServeHTTP(w, r)
	return w.Result(), nil
}

func inProcessPeerClient(t *testing.T, handler http.Handler) *peerRESTClient {
	t.Helper()
	host, err := xnet.ParseHost("peer.test:9000")
	if err != nil {
		t.Fatal(err)
	}
	return &peerRESTClient{
		host: host,
		restClient: rest.NewClient(&url.URL{Scheme: "http", Host: host.String(), Path: peerRESTPath},
			peerHandlerTransport{handler}, newAuthToken),
	}
}

func TestPeerExistingRoutesRemainV14(t *testing.T) {
	router := mux.NewRouter()
	registerPeerRESTHandlers(router)
	count := 0
	err := router.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		path, err := route.GetPathTemplate()
		if err != nil || path == peerRESTPrefix {
			return nil
		}
		if !strings.HasPrefix(path, peerRESTPrefix+"/v14/") {
			t.Errorf("existing peer route moved away from v14: %s", path)
		}
		count++
		return nil
	})
	if err != nil || count < 30 {
		t.Fatalf("peer route inspection: %d routes, %v", count, err)
	}
	// Old peer path independent of version constant
	oldClient := rest.NewClient(&url.URL{Scheme: "http", Host: "peer.test:9000", Path: peerRESTPrefix + "/v14"},
		peerHandlerTransport{router}, newAuthToken)
	body, err := oldClient.Call(context.Background(), "/health", nil, nil, -1)
	if err != nil {
		t.Fatalf("old v14 client cannot reach new node health: %v", err)
	}
	body.Close()
}

// Require peer token, cap reply at activityMaxLimit+1
func TestPeerListActivityHandlerAuth(t *testing.T) {
	l := pinActivityLog(t)
	base := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	for i := range activityMaxLimit + 5 {
		l.append(activityEvent{Time: base.Add(time.Duration(i) * time.Millisecond), AccessKey: "obstoradmin", Source: "s3",
			API: "PutObject", Bucket: "alpha", Object: fmt.Sprintf("obj-%03d", i), Status: 200})
	}
	router := mux.NewRouter()
	registerPeerRESTHandlers(router)

	f := activityFilter{AllBuckets: true, Kinds: []activityKind{activityKindObject}, Limit: 1000}
	var body bytes.Buffer
	if err := gob.NewEncoder(&body).Encode(f); err != nil {
		t.Fatalf("encode filter: %v", err)
	}
	for name, auth := range map[string]string{"no token": "", "bad token": "Bearer not-a-token"} {
		req := httptest.NewRequest(http.MethodPost, peerRESTPath+peerRESTMethodListActivity, bytes.NewReader(body.Bytes()))
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		req.Header.Set("X-Obstor-Time", UTCNow().Format(time.RFC3339))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		var page activityPage
		if rec.Code == http.StatusOK || gob.NewDecoder(rec.Body).Decode(&page) == nil {
			t.Fatalf("%s: status %d, page %+v; want a refusal and no page", name, rec.Code, page)
		}
	}

	client := inProcessPeerClient(t, router)
	page, err := client.ListActivity(context.Background(), f)
	if err != nil {
		t.Fatalf("ListActivity through the peer client: %v", err)
	}
	if len(page.Events) != activityMaxLimit+1 || page.Events[0].Node != "test-node:9000" ||
		page.Events[0].Object != fmt.Sprintf("obj-%03d", activityMaxLimit+4) {
		t.Fatalf("peer page = %d events, first %+v; want %d newest first", len(page.Events), page.Events, activityMaxLimit+1)
	}

	// Older v14 peer has health endpoint without activity API
	oldRouter := mux.NewRouter()
	oldRouter.HandleFunc(peerRESTPrefix+"/v14/health", (&peerRESTServer{}).HealthHandler).Methods(http.MethodPost)
	oldClient := inProcessPeerClient(t, oldRouter)
	if _, err := oldClient.callWithContext(context.Background(), peerRESTMethodHealth, nil, nil, -1); err != nil {
		t.Fatalf("new client cannot call an existing API on an old v14 peer: %v", err)
	}
	sys := &NotificationSys{peerClients: []*peerRESTClient{client, nil, oldClient}}
	pages, missing := sys.ListActivity(context.Background(), f)
	if missing != 1 || len(pages) != 1 || len(pages[0].Events) != activityMaxLimit+1 {
		t.Fatalf("NotificationSys.ListActivity = %d pages, %d missing; want 1 page and 1 missing", len(pages), missing)
	}
}
