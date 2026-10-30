package cmd

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/obstor/obstor/cmd/logger"
	iampolicy "github.com/obstor/obstor/pkg/iam/policy"
	"github.com/obstor/obstor/pkg/madmin"
)

// Create IAM user with name-secret and given statements, requires InitStore
func makeActivityUser(t *testing.T, name string, statements ...string) {
	t.Helper()
	doc := `{"Version":"2012-10-17","Statement":[` + strings.Join(statements, ",") + `]}`
	p, err := iampolicy.ParseConfig(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("parse policy %s: %v", name, err)
	}
	if err = globalIAMSys.SetPolicy(name, *p); err != nil {
		t.Fatalf("set policy %s: %v", name, err)
	}
	if err = globalIAMSys.CreateUser(name, madmin.UserInfo{SecretKey: name + "-secret", Status: madmin.AccountEnabled}); err != nil {
		t.Fatalf("create user %s: %v", name, err)
	}
	if err = globalIAMSys.PolicyDBSet(name, name, false); err != nil {
		t.Fatalf("attach policy %s: %v", name, err)
	}
}

// Call ListActivity with token, return reply and error code and message
func callListActivity(t *testing.T, router http.Handler, token string, args ListActivityArgs) (ListActivityRep, string, string) {
	t.Helper()
	req, err := newTestWebRPCRequest("web.ListActivity", token, &args)
	if err != nil {
		t.Fatalf("ListActivity request: %v", err)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	code, message := webRPCError(t, rec)
	var rep ListActivityRep
	if message == "" {
		if err := getTestWebRPCResponse(rec, &rep); err != nil {
			t.Fatalf("ListActivity reply: %v", err)
		}
	}
	return rep, code, message
}

// API, bucket/object and access key per event
func activityRows(events []activityEvent) string {
	rows := make([]string, 0, len(events))
	for _, e := range events {
		rows = append(rows, fmt.Sprintf("%s %s/%s %s", e.API, e.Bucket, e.Object, e.AccessKey))
	}
	sort.Strings(rows)
	return strings.Join(rows, "\n")
}

// Log in root and named users with name-secret
func activityTestTokens(t *testing.T, router http.Handler, users ...string) map[string]string {
	t.Helper()
	tokens := map[string]string{}
	root, err := getWebRPCToken(router, globalActiveCred.AccessKey, globalActiveCred.SecretKey)
	if err != nil {
		t.Fatalf("login root: %v", err)
	}
	tokens["root"] = root
	for _, name := range users {
		token, err := getWebRPCToken(router, name, name+"-secret")
		if err != nil {
			t.Fatalf("login %s: %v", name, err)
		}
		tokens[name] = token
	}
	return tokens
}

// Allow all S3 actions on alpha
const activityAlphaAll = `{"Effect":"Allow","Action":["s3:*"],"Resource":["arn:aws:s3:::alpha","arn:aws:s3:::alpha/*"]}`

func TestWebHandlerListActivityCompleteAuthorizationPrefix(t *testing.T) {
	ExecObjectLayerTest(t, func(obj ObjectLayer, instanceType string, _ TestErrHandler) {
		t.Run(instanceType, func(t *testing.T) {
			defer pinBucketGlobals(instanceType)()
			l := pinActivityLog(t)
			globalIAMSys.InitStore(obj)
			router := initTestWebRPCEndPoint(obj)
			if err := obj.MakeBucketWithLocation(context.Background(), "alpha", BucketOptions{}); err != nil {
				t.Fatal(err)
			}
			prefix := strings.Repeat("a", 260) + "/docs/"
			makeActivityUser(t, "longprefix", activityAlphaAll,
				`{"Effect":"Deny","Action":["s3:ListBucket"],"Resource":["arn:aws:s3:::alpha"],"Condition":{"StringLike":{"s3:prefix":["`+prefix+`*"]}}}`)
			for _, object := range []string{prefix + "secret.txt", strings.Repeat("é", 140) + "/visible.txt", "public.txt"} {
				l.submit(activityEvent{AccessKey: "longprefix", API: "PutObject", Bucket: "alpha", Object: object, Status: http.StatusOK})
			}
			l.drain()
			stored := l.snapshot()
			if stored[0].AuthorizationPrefix != prefix || !stored[0].AuthorizationPrefixKnown {
				t.Fatalf("complete prefix lost: %+v", stored[0])
			}
			for _, e := range stored {
				if len(e.Object) > activityObjectMax || !utf8.ValidString(e.Object) {
					t.Fatalf("invalid display object %q", e.Object)
				}
			}
			// Full authorization target survives disk and peer serialization without display truncation
			segment, err := encodeActivitySegment(stored)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := decodeActivitySegment(segment)
			if err != nil {
				t.Fatal(err)
			}
			var peer bytes.Buffer
			if err := gob.NewEncoder(&peer).Encode(activityPage{Events: loaded}); err != nil {
				t.Fatal(err)
			}
			var page activityPage
			if err := gob.NewDecoder(&peer).Decode(&page); err != nil {
				t.Fatal(err)
			}
			if page.Events[0].AuthorizationPrefix != prefix || !page.Events[0].AuthorizationPrefixKnown {
				t.Fatal("persistence/peer serialization lost the authorization prefix")
			}

			// Old segments lack authorization fields even for complete-looking root names
			type legacyEvent struct {
				Time                           time.Time
				API, Bucket, Object, AccessKey string
			}
			var legacy bytes.Buffer
			legacy.WriteByte(activityFileVersion)
			if err := gob.NewEncoder(&legacy).Encode([]legacyEvent{
				{Time: UTCNow(), API: "PutObject", Bucket: "alpha", Object: strings.Repeat("a", 256), AccessKey: "longprefix"},
				{Time: UTCNow(), API: "PutObject", Bucket: "alpha", Object: "old.txt", AccessKey: "longprefix"},
				{Time: UTCNow(), API: "PutBucket", Bucket: "alpha", AccessKey: "longprefix"},
			}); err != nil {
				t.Fatal(err)
			}
			old, err := decodeActivitySegment(legacy.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			l.mu.Lock()
			l.next, l.size = 0, 0
			for _, e := range append(page.Events, old...) {
				l.putRing(e)
			}
			l.mu.Unlock()
			tokens := activityTestTokens(t, router, "longprefix")
			rep, code, message := callListActivity(t, router, tokens["longprefix"], ListActivityArgs{})
			if code != "" || len(rep.Events) != 3 {
				t.Fatalf("scoped activity = %+v, %q %q; want two allowed new objects and the legacy bucket event", rep.Events, code, message)
			}
			for _, e := range rep.Events {
				if e.Object == stored[0].Object || e.Object == "old.txt" {
					t.Fatalf("denied or unverifiable object escaped: %+v", e)
				}
			}
			rep, code, message = callListActivity(t, router, tokens["root"], ListActivityArgs{})
			if code != "" || len(rep.Events) != 6 {
				t.Fatalf("root activity = %+v, %q %q; want all six events", rep.Events, code, message)
			}
			raw, err := json.Marshal(rep)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(raw, []byte(prefix)) || bytes.Contains(raw, []byte("AuthorizationPrefix")) {
				t.Fatal("internal authorization prefix exposed in the browser response")
			}
		})
	})
}

// Allow admin action without Resource
func activityAdminAllow(action string) string {
	return `{"Effect":"Allow","Action":["` + action + `"]}`
}

func TestWebHandlerListActivityTiers(t *testing.T) {
	ExecObjectLayerTest(t, func(obj ObjectLayer, instanceType string, _ TestErrHandler) {
		t.Run(instanceType, func(t *testing.T) { testListActivityTiers(t, obj, instanceType) })
	})
}

func testListActivityTiers(t *testing.T, obj ObjectLayer, instanceType string) {
	defer pinBucketGlobals(instanceType)()
	l := pinActivityLog(t)
	globalIAMSys.InitStore(obj)
	router := initTestWebRPCEndPoint(obj)
	ctx := context.Background()
	for _, b := range []string{"alpha", "beta"} {
		if err := obj.MakeBucketWithLocation(ctx, b, BucketOptions{}); err != nil {
			t.Fatalf("make bucket %s: %v", b, err)
		}
	}

	makeActivityUser(t, "scoped", activityAlphaAll)
	makeActivityUser(t, "keeper", `{"Effect":"Allow","Action":["s3:*"],"Resource":["arn:aws:s3:::gone","arn:aws:s3:::gone/*"]}`)
	makeActivityUser(t, "lister", activityAlphaAll, activityAdminAllow("admin:ListUsers"))
	makeActivityUser(t, "policier", activityAdminAllow("admin:ListUserPolicies"))
	makeActivityUser(t, "operator", activityAlphaAll, activityAdminAllow("admin:ServerInfo"))
	makeActivityUser(t, "nodocs", activityAlphaAll,
		`{"Effect":"Deny","Action":["s3:ListBucket"],"Resource":["arn:aws:s3:::alpha"],"Condition":{"StringLike":{"s3:prefix":["docs/*"]}}}`)
	makeActivityUser(t, "pair", `{"Effect":"Allow","Action":["s3:*"],"Resource":["arn:aws:s3:::alpha","arn:aws:s3:::alpha/*","arn:aws:s3:::beta","arn:aws:s3:::beta/*"]}`)
	makeActivityUser(t, "fleet", activityAdminAllow("admin:ServerInfo"), `{"Effect":"Allow","Action":["s3:ListBucket"],"Resource":["arn:aws:s3:::*"]}`)
	tokens := activityTestTokens(t, router, "scoped", "keeper", "lister", "policier", "operator", "nodocs", "pair", "fleet")

	root := globalActiveCred.AccessKey
	audit := func(api, bucket, object, accessKey string, status int) {
		recordAuditActivity(ctx, &logger.ReqInfo{API: api, BucketName: bucket, ObjectName: object, AccessKey: accessKey}, status)
	}
	audit("PutObject", "alpha", "a.txt", root, 200)
	audit("PutObject", "alpha", "docs/c.txt", "scoped", 200)
	audit("DeleteObject", "alpha", "b.txt", root, 204)
	RecordActivity("sftp", root, "PutObject", "beta", "e.txt", 200)
	audit("PutObject", "gone", "old.txt", root, 200)
	audit("DeleteBucket", "gone", "", root, 204)
	audit("AddUser", "", "", root, 200)
	audit("AddCannedPolicy", "", "", root, 200)
	audit("SetConfigKV", "", "", root, 200)
	audit("Service", "", "", root, 200)
	audit("PutObject", "alpha", "anon.txt", "", 403)
	RecordActivity("sftp", "scoped", "PutObject", "beta", "x.txt", 403)
	l.drain()

	row := func(api, bucket, object, accessKey string) string {
		return fmt.Sprintf("%s %s/%s %s", api, bucket, object, accessKey)
	}
	rows := func(r ...string) string {
		sort.Strings(r)
		return strings.Join(r, "\n")
	}
	putA := func(k string) string { return row("PutObject", "alpha", "a.txt", k) }
	putDocs := func(k string) string { return row("PutObject", "alpha", "docs/c.txt", k) }
	delB := func(k string) string { return row("DeleteObject", "alpha", "b.txt", k) }
	putE := func(k string) string { return row("PutObject", "beta", "e.txt", k) }
	putOld := func(k string) string { return row("PutObject", "gone", "old.txt", k) }
	delGone := func(k string) string { return row("DeleteBucket", "gone", "", k) }
	addUser := func(k string) string { return row("AddUser", "", "", k) }
	addPolicy := func(k string) string { return row("AddCannedPolicy", "", "", k) }
	setConfig := func(k string) string { return row("SetConfigKV", "", "", k) }
	service := func(k string) string { return row("Service", "", "", k) }

	for _, c := range []struct {
		name   string
		caller string
		args   ListActivityArgs
		rows   string
		denied uint64
		code   string
	}{
		{name: "root sees everything unmasked", caller: "root",
			rows: rows(putA(root), putDocs("scoped"), delB(root), putE(root), putOld(root), delGone(root),
				addUser(root), addPolicy(root), setConfig(root), service(root)), denied: 2},
		{name: "root bucket filter keeps object and bucket events of that bucket", caller: "root", args: ListActivityArgs{BucketName: "alpha"},
			rows: rows(putA(root), putDocs("scoped"), delB(root)), denied: 2},
		{name: "root delete group", caller: "root", args: ListActivityArgs{API: "delete"}, rows: rows(delB(root)), denied: 2},
		{name: "scoped sees own bucket, own key, no IAM or counters", caller: "scoped",
			rows: rows(putA(""), putDocs("scoped"), delB(""))},
		{name: "scoped cannot read a bucket outside its policy", caller: "scoped", args: ListActivityArgs{BucketName: "gone"}, code: "AccessDenied"},
		{name: "keeper without a filter sees no deleted bucket", caller: "keeper", rows: ""},
		{name: "keeper reads the deleted bucket through an explicit filter", caller: "keeper", args: ListActivityArgs{BucketName: "gone"},
			rows: rows(putOld(""), delGone(""))},
		{name: "users tier sees user events and other keys", caller: "lister",
			rows: rows(putA(root), putDocs("scoped"), delB(root), addUser(root))},
		{name: "policies tier sees policy events only", caller: "policier", rows: rows(addPolicy(""))},
		{name: "settings tier sees service, config and counters", caller: "operator",
			rows: rows(putA(""), putDocs(""), delB(""), setConfig(""), service("")), denied: 2},
		{name: "prefix deny hides docs events", caller: "nodocs", rows: rows(putA(""), delB(""))},
		{name: "listing every live bucket stays scoped", caller: "pair",
			rows: rows(putA(""), putDocs(""), delB(""), putE(""))},
		{name: "fleet tier keeps deleted buckets", caller: "fleet",
			rows: rows(putA(""), putDocs(""), delB(""), putE(""), putOld(""), delGone(""), setConfig(""), service("")), denied: 2},
	} {
		rep, code, message := callListActivity(t, router, tokens[c.caller], c.args)
		if c.code != "" {
			if code != c.code {
				t.Errorf("%s: code %q (%s), want %q", c.name, code, message, c.code)
			}
			continue
		}
		if message != "" {
			t.Errorf("%s: unexpected error %q (%s)", c.name, message, code)
			continue
		}
		if rep.Events == nil {
			t.Errorf("%s: events is null, want an array", c.name)
		}
		if got := activityRows(rep.Events); got != c.rows {
			t.Errorf("%s: events\n%s\nwant\n%s", c.name, got, c.rows)
		}
		if rep.Denied != c.denied || rep.Dropped != 0 {
			t.Errorf("%s: denied %d dropped %d, want %d and 0", c.name, rep.Denied, rep.Dropped, c.denied)
		}
		if rep.Next != nil || rep.Partial || rep.MissingNodes != 0 {
			t.Errorf("%s: next %+v partial %v missing %d, want nil, false, 0", c.name, rep.Next, rep.Partial, rep.MissingNodes)
		}
	}
}

func TestWebHandlerListActivityInput(t *testing.T) {
	ExecObjectLayerTest(t, func(obj ObjectLayer, instanceType string, _ TestErrHandler) {
		t.Run(instanceType, func(t *testing.T) { testListActivityInput(t, obj, instanceType) })
	})
}

func testListActivityInput(t *testing.T, obj ObjectLayer, instanceType string) {
	defer pinBucketGlobals(instanceType)()
	l := pinActivityLog(t)
	globalIAMSys.InitStore(obj)
	router := initTestWebRPCEndPoint(obj)
	if err := obj.MakeBucketWithLocation(context.Background(), "alpha", BucketOptions{}); err != nil {
		t.Fatalf("make bucket: %v", err)
	}
	makeActivityUser(t, "scoped", activityAlphaAll)
	tokens := activityTestTokens(t, router, "scoped")
	root := globalActiveCred.AccessKey
	for i := 0; i < 60; i++ {
		RecordActivity("sftp", root, "PutObject", "alpha", fmt.Sprintf("f%02d.txt", i), 200)
	}
	RecordActivity("sftp", "scoped", "PutObject", "alpha", "mine.txt", 200)
	l.drain()

	for _, c := range []struct {
		name    string
		token   string
		args    ListActivityArgs
		code    string
		message string
	}{
		{name: "limit over 200", token: tokens["root"], args: ListActivityArgs{Limit: 201}, message: "Ask for at most 200 events per call."},
		{name: "negative limit", token: tokens["root"], args: ListActivityArgs{Limit: -1}, message: "Ask for at most 200 events per call."},
		{name: "unknown group", token: tokens["root"], args: ListActivityArgs{API: "bogus"}, message: "Unknown action type: bogus."},
		{name: "exact API name is not a group", token: tokens["root"], args: ListActivityArgs{API: "PutObject"}, message: "Unknown action type: PutObject."},
		{name: "reserved bucket", token: tokens["root"], args: ListActivityArgs{BucketName: ".obstor.sys"}, code: "InvalidBucketName"},
		{name: "invalid bucket", token: tokens["root"], args: ListActivityArgs{BucketName: "ab"}, code: "InvalidBucketName"},
		{name: "scoped filters by another key", token: tokens["scoped"], args: ListActivityArgs{AccessKey: root}, message: "You can only filter by your own access key."},
		{name: "bad token", token: "not-a-token", code: "WebTokenInvalid"},
		{name: "no token", token: "", code: "WebTokenMissing"},
	} {
		_, code, message := callListActivity(t, router, c.token, c.args)
		if code != c.code || (c.message != "" && message != c.message) || message == "" {
			t.Errorf("%s: code %q message %q, want code %q message %q", c.name, code, message, c.code, c.message)
		}
	}

	rep, _, message := callListActivity(t, router, tokens["scoped"], ListActivityArgs{AccessKey: "scoped"})
	if message != "" || activityRows(rep.Events) != "PutObject alpha/mine.txt scoped" {
		t.Fatalf("scoped own-key filter = %q (%s), want only its own event", activityRows(rep.Events), message)
	}

	rep, _, message = callListActivity(t, router, tokens["root"], ListActivityArgs{})
	if message != "" || len(rep.Events) != activityDefaultLimit || rep.Next == nil {
		t.Fatalf("limit 0 = %d events next %v (%s), want %d and a cursor", len(rep.Events), rep.Next, message, activityDefaultLimit)
	}
	rest, _, message := callListActivity(t, router, tokens["root"], ListActivityArgs{Before: rep.Next})
	if message != "" || len(rest.Events) != 11 || rest.Next != nil {
		t.Fatalf("second page = %d events next %v (%s), want 11 and nil", len(rest.Events), rest.Next, message)
	}
	seen := map[string]bool{}
	for _, e := range append(rep.Events, rest.Events...) {
		seen[e.Object] = true
	}
	if len(seen) != 61 {
		t.Fatalf("two pages hold %d distinct objects, want 61", len(seen))
	}
	all, _, message := callListActivity(t, router, tokens["root"], ListActivityArgs{Limit: activityMaxLimit})
	if message != "" || len(all.Events) != 61 || all.Next != nil {
		t.Fatalf("limit 200 = %d events next %v (%s), want 61 and nil", len(all.Events), all.Next, message)
	}
}
