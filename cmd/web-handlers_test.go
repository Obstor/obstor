/*
 * MinIO Cloud Storage, (C) 2016, 2017, 2018 MinIO, Inc.
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
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime/pprof"
	"strconv"
	"strings"
	"testing"
	"time"

	humanize "github.com/dustin/go-humanize"
	jwtgo "github.com/golang-jwt/jwt/v4"
	"github.com/gorilla/mux"
	xjwt "github.com/obstor/obstor/cmd/jwt"
	"github.com/obstor/obstor/pkg/auth"
	"github.com/obstor/obstor/pkg/handlers"
	"github.com/obstor/obstor/pkg/hash"
	iampolicy "github.com/obstor/obstor/pkg/iam/policy"
	"github.com/obstor/obstor/pkg/madmin"
)

// User-scoped usage check
func TestScopedUsageOnlyAllowedBuckets(t *testing.T) {
	bucketsUsage := map[string]madmin.BucketUsageInfo{
		"bucket-a": {Size: 100, ObjectsCount: 1},
		"bucket-b": {Size: 900, ObjectsCount: 9},
	}
	// Caller is allowed only bucket-a.
	used, objects, buckets := scopedUsage(bucketsUsage, func(b string) bool { return b == "bucket-a" })
	if used != 100 {
		t.Fatalf("scoped usage leaked instance-wide total: got used=%d want 100", used)
	}
	if objects != 1 || buckets != 1 {
		t.Fatalf("scoped usage not limited to allowed bucket: objects=%d buckets=%d want 1 and 1", objects, buckets)
	}
}

// Implement a dummy flush writer.
type flushWriter struct {
	io.Writer
}

// Flush writer is a dummy writer compatible with http.Flusher and http.ResponseWriter.
func (f *flushWriter) Flush()                            {}
func (f *flushWriter) Write(b []byte) (n int, err error) { return f.Writer.Write(b) }
func (f *flushWriter) Header() http.Header               { return http.Header{} }
func (f *flushWriter) WriteHeader(code int)              {}

func newFlushWriter(writer io.Writer) http.ResponseWriter {
	return &flushWriter{writer}
}

// Tests private function writeWebErrorResponse.
func TestWriteWebErrorResponse(t *testing.T) {
	var buffer bytes.Buffer
	testCases := []struct {
		webErr     error
		apiErrCode APIErrorCode
	}{
		// List of various errors and their corresponding API errors.
		{
			webErr:     StorageFull{},
			apiErrCode: ErrStorageFull,
		},
		{
			webErr:     BucketNotFound{},
			apiErrCode: ErrNoSuchBucket,
		},
		{
			webErr:     BucketNameInvalid{},
			apiErrCode: ErrInvalidBucketName,
		},
		{
			webErr:     hash.BadDigest{},
			apiErrCode: ErrBadDigest,
		},
		{
			webErr:     IncompleteBody{},
			apiErrCode: ErrIncompleteBody,
		},
		{
			webErr:     ObjectExistsAsDirectory{},
			apiErrCode: ErrObjectExistsAsDirectory,
		},
		{
			webErr:     ObjectNotFound{},
			apiErrCode: ErrNoSuchKey,
		},
		{
			webErr:     ObjectNameInvalid{},
			apiErrCode: ErrNoSuchKey,
		},
		{
			webErr:     InsufficientWriteQuorum{},
			apiErrCode: ErrWriteQuorum,
		},
		{
			webErr:     InsufficientReadQuorum{},
			apiErrCode: ErrReadQuorum,
		},
		{
			webErr:     NotImplemented{},
			apiErrCode: ErrNotImplemented,
		},
	}

	// Validate all the test cases.
	for i, testCase := range testCases {
		writeWebErrorResponse(newFlushWriter(&buffer), testCase.webErr)
		desc := getAPIError(testCase.apiErrCode).Description
		if testCase.apiErrCode == ErrNotImplemented {
			desc = "Functionality not implemented"
		}
		recvDesc := buffer.Bytes()
		// Check if the written desc is same as the one expected.
		if !bytes.Equal(recvDesc, []byte(desc)) {
			t.Errorf("Test %d: Unexpected response, expecting %s, got %s", i+1, desc, buffer.String())
		}
		buffer.Reset()
	}
}

// Authenticate and get JWT token - will be called before every webrpc handler invocation
func getWebRPCToken(apiRouter http.Handler, accessKey, secretKey string) (token string, err error) {
	return authenticateWeb(accessKey, secretKey)
}

// Wrapper for calling Login Web Handler
func TestWebHandlerLogin(t *testing.T) {
	ExecObjectLayerTest(t, testLoginWebHandler)
}

// testLoginWebHandler - Test login web handler
func testLoginWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	// Register the API end points with Erasure/FS object layer.
	apiRouter := initTestWebRPCEndPoint(obj)
	credentials := globalActiveCred

	// test cases with sample input and expected output.
	testCases := []struct {
		username string
		password string
		success  bool
	}{
		{"", "", false},
		{"azerty", "foo", false},
		{"", "foo", false},
		{"azerty", "", false},
		{"azerty", "foo", false},
		{"azerty", "azerty123", false},
		{credentials.AccessKey, credentials.SecretKey, true},
	}

	// Iterating over the test cases, calling the function under test and asserting the response.
	for i, testCase := range testCases {
		_, err := getWebRPCToken(apiRouter, testCase.username, testCase.password)
		if err != nil && testCase.success {
			t.Fatalf("Test %d: Expected to succeed but it failed, %v", i+1, err)
		}
		if err == nil && !testCase.success {
			t.Fatalf("Test %d: Expected to fail but it didn't, %v", i+1, err)
		}

	}
}

// Error code and message from web RPC reply, empty on success
func webRPCError(t TestErrHandler, rec *httptest.ResponseRecorder) (code, message string) {
	t.Helper()
	var reply struct {
		Error *struct {
			Message string          `json:"message"`
			Data    json.RawMessage `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
		t.Fatalf("decode webrpc reply %q: %v", rec.Body.String(), err)
	}
	if reply.Error == nil {
		return "", ""
	}
	var data struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(reply.Error.Data, &data) // Codec parse errors may have non-object data
	return data.Code, reply.Error.Message
}

func TestToJSONErrorCode(t *testing.T) {
	for _, test := range []struct {
		name    string
		err     error
		params  []string
		code    string
		message string
	}{
		{name: "access denied", err: errAccessDenied, code: "AccessDenied", message: errAccessDenied.Error()},
		{name: "custom message keeps the code", err: BucketNotFound{Bucket: "photos"}, params: []string{"photos"}, code: "NoSuchBucket", message: "The specified bucket photos does not exist."},
		{name: "server starting", err: errServerNotInitialized, code: "XObstorServerNotInitialized", message: errServerNotInitialized.Error()},
		{name: "wrong current secret", err: errIncorrectCreds, code: "InvalidCurrentSecret", message: errIncorrectCreds.Error()},
	} {
		jerr := toJSONError(context.Background(), test.err, test.params...)
		data, _ := jerr.Data.(map[string]string)
		if data["code"] != test.code || jerr.Message != test.message {
			t.Errorf("%s: code %q message %q, want %q %q", test.name, data["code"], jerr.Message, test.code, test.message)
		}
	}
}

func TestWebHandlerLoginErrors(t *testing.T) {
	ExecObjectLayerTest(t, testLoginErrorsWebHandler)
}

// Login distinguishes starting server, unknown keys match wrong secrets
func testLoginErrorsWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	savedIAM := globalIAMSys
	defer func() { globalIAMSys = savedIAM }()
	globalIAMSys = NewIAMSys()
	router := initTestWebRPCEndPoint(obj)
	login := func(user, secret string) (code, message, token string) {
		t.Helper()
		req, err := newTestWebRPCRequest("web.Login", "", LoginArgs{Username: user, Password: secret})
		if err != nil {
			t.Fatalf("%s: Login request: %v", instanceType, err)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		code, message = webRPCError(t, rec)
		var rep LoginRep
		_ = getTestWebRPCResponse(rec, &rep) // webRPCError already read error
		return code, message, rep.Token
	}

	if code, message, _ := login("loginiam", "loginiam-secret"); code != "XObstorServerNotInitialized" {
		t.Fatalf("%s: non-root login during IAM init = %q %q, want XObstorServerNotInitialized", instanceType, code, message)
	}
	if code, message, token := login(globalActiveCred.AccessKey, globalActiveCred.SecretKey); code != "" || token == "" {
		t.Fatalf("%s: root login during IAM init = %q %q, want a token", instanceType, code, message)
	}

	globalIAMSys.InitStore(obj)
	cred := makeTestIAMUser(t, "loginiam", "s3:ListBucket")
	unknownCode, unknownMessage, _ := login("nosuchuser", "nosuchuser-secret")
	wrongCode, wrongMessage, _ := login(cred.AccessKey, "wrong-secret")
	if unknownCode != "AccessDenied" || wrongCode != unknownCode || wrongMessage != unknownMessage || unknownMessage != errAuthentication.Error() {
		t.Fatalf("%s: unknown key = %q %q, wrong secret = %q %q, want both AccessDenied %q", instanceType, unknownCode, unknownMessage, wrongCode, wrongMessage, errAuthentication.Error())
	}
	if code, message, token := login(cred.AccessKey, cred.SecretKey); code != "" || token == "" {
		t.Fatalf("%s: IAM login = %q %q, want a token", instanceType, code, message)
	}
}

func TestWebHandlerSetAuthErrors(t *testing.T) {
	ExecObjectLayerTest(t, testSetAuthErrorsWebHandler)
}

// SetAuth error codes for wrong secret and root credentials
func testSetAuthErrorsWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	savedIAM := globalIAMSys
	defer func() { globalIAMSys = savedIAM }()
	globalIAMSys = NewIAMSys()
	globalIAMSys.InitStore(obj)
	router := initTestWebRPCEndPoint(obj)
	setAuth := func(token, current, next string) (code, message string) {
		t.Helper()
		req, err := newTestWebRPCRequest("web.SetAuth", token, SetAuthArgs{CurrentSecretKey: current, NewSecretKey: next})
		if err != nil {
			t.Fatalf("%s: SetAuth request: %v", instanceType, err)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return webRPCError(t, rec)
	}
	cred := makeTestIAMUser(t, "setautherrs", "s3:ListBucket")
	userToken, err := getWebRPCToken(router, cred.AccessKey, cred.SecretKey)
	if err != nil {
		t.Fatalf("%s: login as %s: %v", instanceType, cred.AccessKey, err)
	}
	rootToken, err := getWebRPCToken(router, globalActiveCred.AccessKey, globalActiveCred.SecretKey)
	if err != nil {
		t.Fatalf("%s: root login: %v", instanceType, err)
	}

	if code, message := setAuth(userToken, "wrong-secret", "setautherrs-new-secret"); code != "InvalidCurrentSecret" || message != errIncorrectCreds.Error() {
		t.Fatalf("%s: wrong current secret = %q %q, want InvalidCurrentSecret %q", instanceType, code, message, errIncorrectCreds.Error())
	}
	if code, message := setAuth(rootToken, globalActiveCred.SecretKey, "root-new-secret"); code != "MethodNotAllowed" {
		t.Fatalf("%s: root SetAuth = %q %q, want MethodNotAllowed", instanceType, code, message)
	}
	if code, message := setAuth(userToken, cred.SecretKey, "setautherrs-new-secret"); code != "" || message != "" {
		t.Fatalf("%s: SetAuth with the right secret = %q %q, want no error", instanceType, code, message)
	}
}

func TestWebHandlerSetAuthAccountKinds(t *testing.T) {
	ExecObjectLayerTest(t, testSetAuthAccountKindsWebHandler)
}

// Only long-term IAM users change secrets, 8 to 40 characters, including after reload
func testSetAuthAccountKindsWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	savedIAM := globalIAMSys
	defer func() { globalIAMSys = savedIAM }()
	globalIAMSys = NewIAMSys()
	globalIAMSys.InitStore(obj)
	router := initTestWebRPCEndPoint(obj)
	setAuth := func(token, current, next string) (code, message string) {
		t.Helper()
		req, err := newTestWebRPCRequest("web.SetAuth", token, SetAuthArgs{CurrentSecretKey: current, NewSecretKey: next})
		if err != nil {
			t.Fatalf("%s: SetAuth request: %v", instanceType, err)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return webRPCError(t, rec)
	}
	user := makeTestIAMUser(t, "setauthkinds", "s3:ListBucket")
	svc, err := globalIAMSys.NewServiceAccount(context.Background(), user.AccessKey, nil, newServiceAccountOpts{})
	if err != nil {
		t.Fatalf("%s: new service account: %v", instanceType, err)
	}
	temp := auth.Credentials{
		AccessKey:    "setauthtemp",
		SecretKey:    "setauthtemp-secret",
		SessionToken: "setauthtemp-session",
		Expiration:   UTCNow().Add(time.Hour),
		ParentUser:   user.AccessKey,
	}
	if err = globalIAMSys.SetTempUser(temp.AccessKey, temp, ""); err != nil {
		t.Fatalf("%s: set temp user: %v", instanceType, err)
	}
	// Service token uses own secret, STS uses root secret
	signToken := func(accessKey, secret string) string {
		t.Helper()
		claims := xjwt.NewMapClaims()
		claims.SetExpiry(UTCNow().Add(time.Hour))
		claims.SetAccessKey(accessKey)
		token, err := jwtgo.NewWithClaims(jwtgo.SigningMethodHS512, claims).SignedString([]byte(secret))
		if err != nil {
			t.Fatalf("%s: sign token for %s: %v", instanceType, accessKey, err)
		}
		return token
	}
	svcToken := signToken(svc.AccessKey, svc.SecretKey)
	tempToken := signToken(temp.AccessKey, globalActiveCred.SecretKey)
	if code, message := setAuth(svcToken, svc.SecretKey, "setauthkinds-svc-new"); code != "MethodNotAllowed" {
		t.Fatalf("%s: service account SetAuth = %q %q, want MethodNotAllowed", instanceType, code, message)
	}
	if code, message := setAuth(tempToken, temp.SecretKey, "setauthkinds-temp-new"); code != "MethodNotAllowed" {
		t.Fatalf("%s: temp SetAuth = %q %q, want MethodNotAllowed", instanceType, code, message)
	}

	// Reloaded IAM map loads these entries from store on GetUser
	globalIAMSys.store.lock()
	delete(globalIAMSys.iamUsersMap, svc.AccessKey)
	delete(globalIAMSys.iamUsersMap, temp.AccessKey)
	globalIAMSys.store.unlock()
	for _, key := range []string{svc.AccessKey, temp.AccessKey} {
		if credChangeAllowed(key) {
			t.Fatalf("%s: credChangeAllowed(%q) after reload = true, want false", instanceType, key)
		}
	}
	for _, key := range []string{svc.AccessKey, temp.AccessKey} {
		if cred, ok := globalIAMSys.GetUser(key); ok && !cred.IsTemp() && !cred.IsServiceAccount() {
			t.Fatalf("%s: GetUser(%q) lazy-loaded as a regular user", instanceType, key)
		}
	}

	userToken, err := getWebRPCToken(router, user.AccessKey, user.SecretKey)
	if err != nil {
		t.Fatalf("%s: login as %s: %v", instanceType, user.AccessKey, err)
	}
	wantLong := "secret key must be maximum 40 or fewer characters long"
	if code, message := setAuth(userToken, user.SecretKey, strings.Repeat("a", 41)); message != wantLong {
		t.Fatalf("%s: 41-char secret = %q %q, want %q", instanceType, code, message, wantLong)
	}
	if cred, _ := globalIAMSys.GetUser(user.AccessKey); cred.SecretKey != user.SecretKey {
		t.Fatalf("%s: refused 41-char secret still changed the stored secret", instanceType)
	}
	if code, message := setAuth(userToken, user.SecretKey, strings.Repeat("b", 40)); code != "" || message != "" {
		t.Fatalf("%s: 40-char secret = %q %q, want no error", instanceType, code, message)
	}
	if !credChangeAllowed(user.AccessKey) {
		t.Fatalf("%s: credChangeAllowed(%q) = false for a regular IAM user", instanceType, user.AccessKey)
	}
}

// Both web RPC routes reject bodies over 1 MiB before decode
func TestWebRPCBodyLimit(t *testing.T) {
	savedIAM := globalIAMSys
	defer func() { globalIAMSys = savedIAM }()
	globalIAMSys = NewIAMSys()
	full := mux.NewRouter().SkipClean(true)
	if err := registerWebRouter(full); err != nil {
		t.Fatal(err)
	}
	rpcOnly := mux.NewRouter().SkipClean(true)
	if err := registerWebRPCRouter(rpcOnly); err != nil {
		t.Fatal(err)
	}
	for name, router := range map[string]http.Handler{"registerWebRouter": full, "registerWebRPCRouter": rpcOnly} {
		req, err := newTestWebRPCRequest("web.Login", "", LoginArgs{Username: strings.Repeat("a", 1<<20), Password: "wrong-secret"})
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if code, message := webRPCError(t, rec); rec.Code != http.StatusBadRequest || code != "EntityTooLarge" || !strings.Contains(message, "http: request body too large") {
			t.Fatalf("%s: oversized body = %d %q %q, want 400 EntityTooLarge \"http: request body too large\"", name, rec.Code, code, message)
		}

		req, err = newTestWebRPCRequest("web.Login", "", LoginArgs{Username: globalActiveCred.AccessKey, Password: globalActiveCred.SecretKey})
		if err != nil {
			t.Fatal(err)
		}
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if code, message := webRPCError(t, rec); rec.Code != http.StatusOK || code != "" || message != "" {
			t.Fatalf("%s: small body = %d %q %q, want 200 and a token", name, rec.Code, code, message)
		}
	}
}

// Wrapper for calling StorageInfo Web Handler
func TestWebHandlerStorageInfo(t *testing.T) {
	ExecObjectLayerTest(t, testStorageInfoWebHandler)
}

// testStorageInfoWebHandler - Test StorageInfo web handler
func testStorageInfoWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	// get random bucket name.
	// Register the API end points with Erasure/FS object layer.
	apiRouter := initTestWebRPCEndPoint(obj)
	credentials := globalActiveCred

	authorization, err := getWebRPCToken(apiRouter, credentials.AccessKey, credentials.SecretKey)
	if err != nil {
		t.Fatal("Cannot authenticate")
	}

	rec := httptest.NewRecorder()

	storageInfoRequest := &WebGenericArgs{}
	storageInfoReply := &StorageInfoRep{}
	req, err := newTestWebRPCRequest("web.StorageInfo", authorization, storageInfoRequest)
	if err != nil {
		t.Fatalf("Failed to create HTTP request: <ERROR> %v", err)
	}
	apiRouter.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected the response status to be 200, but instead found `%d`", rec.Code)
	}
	if err = getTestWebRPCResponse(rec, &storageInfoReply); err != nil {
		t.Fatalf("Failed %v", err)
	}
}

// List servers wrapper
func TestWebHandlerListServers(t *testing.T) {
	ExecObjectLayerTest(t, testListServersWebHandler)
}

// testListServersWebHandler - owner must still see node topology after the callerCanSeeInstanceTotals gate.
func testListServersWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	apiRouter := initTestWebRPCEndPoint(obj)
	credentials := globalActiveCred

	authorization, err := getWebRPCToken(apiRouter, credentials.AccessKey, credentials.SecretKey)
	if err != nil {
		t.Fatal("Cannot authenticate")
	}

	rec := httptest.NewRecorder()

	listServersReply := &ServerListRep{}
	req, err := newTestWebRPCRequest("web.ListServers", authorization, &ListServersArgs{})
	if err != nil {
		t.Fatalf("Failed to create HTTP request: <ERROR> %v", err)
	}
	apiRouter.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected the response status to be 200, but instead found `%d`", rec.Code)
	}
	if err = getTestWebRPCResponse(rec, &listServersReply); err != nil {
		t.Fatalf("owner must see topology: %v", err)
	}
	if listServersReply.Total < 1 || len(listServersReply.Servers) < 1 {
		t.Fatalf("owner must see at least the local node: total=%d servers=%d", listServersReply.Total, len(listServersReply.Servers))
	}
}

// Backend has no globalNotificationSys, ListServers must handle nil
func TestWebHandlerListServersBackend(t *testing.T) {
	ExecObjectLayerTest(t, func(obj ObjectLayer, instanceType string, t TestErrHandler) {
		defer func(b bool, n *NotificationSys) { globalIsBackend, globalNotificationSys = b, n }(globalIsBackend, globalNotificationSys)
		globalIsBackend, globalNotificationSys = true, nil

		apiRouter := initTestWebRPCEndPoint(obj)
		authorization, err := getWebRPCToken(apiRouter, globalActiveCred.AccessKey, globalActiveCred.SecretKey)
		if err != nil {
			t.Fatal("Cannot authenticate")
		}
		rec := httptest.NewRecorder()
		req, err := newTestWebRPCRequest("web.ListServers", authorization, &ListServersArgs{})
		if err != nil {
			t.Fatalf("Failed to create HTTP request: <ERROR> %v", err)
		}
		apiRouter.ServeHTTP(rec, req)
		reply := &ServerListRep{}
		if err = getTestWebRPCResponse(rec, &reply); err != nil {
			t.Fatalf("%s: expected empty list, got %v", instanceType, err)
		}
		if reply.Total != 0 || len(reply.Servers) != 0 {
			t.Fatalf("%s: expected no nodes, got total=%d", instanceType, reply.Total)
		}
	})
}

// Folder delete respects sub-prefix Deny
func TestWebHandlerRemoveFolderSubPrefixDeny(t *testing.T) {
	ExecObjectLayerTest(t, testRemoveFolderSubPrefixDeny)
}

func testRemoveFolderSubPrefixDeny(obj ObjectLayer, instanceType string, t TestErrHandler) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func(s *IAMSys) { globalIAMSys = s }(globalIAMSys)
	globalIAMSys = NewIAMSys()
	globalIAMSys.Init(ctx, obj)

	bucketName := getRandomBucketName()
	if err := obj.MakeBucketWithLocation(ctx, bucketName, BucketOptions{}); err != nil {
		t.Fatalf("%s : %s", instanceType, err)
	}
	data := []byte("x")
	// Keys after denial keep Walk blocked sending
	for _, name := range []string{"a/keep", "a/locked/secret", "a/z1", "a/z2"} {
		if _, err := obj.PutObject(ctx, bucketName, name, mustGetPutObjReader(t, bytes.NewReader(data), 1, "", ""), ObjectOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	doc := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[
{"Effect":"Allow","Action":["s3:ListBucket","s3:GetBucketLocation"],"Resource":["arn:aws:s3:::%[1]s"]},
{"Effect":"Allow","Action":["s3:DeleteObject"],"Resource":["arn:aws:s3:::%[1]s/*"]},
{"Effect":"Deny","Action":["s3:DeleteObject"],"Resource":["arn:aws:s3:::%[1]s/a/locked/*"]}]}`, bucketName)
	p, err := iampolicy.ParseConfig(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if err = globalIAMSys.SetPolicy("lockdeny", *p); err != nil {
		t.Fatal(err)
	}
	if err = globalIAMSys.CreateUser("denyuser", madmin.UserInfo{SecretKey: "denyuser-secret", Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	if err = globalIAMSys.PolicyDBSet("denyuser", "lockdeny", false); err != nil {
		t.Fatal(err)
	}

	apiRouter := initTestWebRPCEndPoint(obj)
	authorization, err := getWebRPCToken(apiRouter, "denyuser", "denyuser-secret")
	if err != nil {
		t.Fatalf("Cannot authenticate: %v", err)
	}
	baseline := walkProducers()
	rec := httptest.NewRecorder()
	req, err := newTestWebRPCRequest("web.RemoveObject", authorization, RemoveObjectArgs{BucketName: bucketName, Objects: []string{"a/"}})
	if err != nil {
		t.Fatalf("Failed to create HTTP request: <ERROR> %v", err)
	}
	apiRouter.ServeHTTP(rec, req)
	if err = getTestWebRPCResponse(rec, &WebGenericRep{}); err == nil {
		t.Fatalf("%s: expected access denied for folder holding a denied key", instanceType)
	}
	if _, err = obj.GetObjectInfo(ctx, bucketName, "a/locked/secret", ObjectOptions{}); err != nil {
		t.Fatalf("%s: denied key was deleted: %v", instanceType, err)
	}
	for deadline := time.Now().Add(2 * time.Second); walkProducers() > baseline; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("%s: Walk producer goroutine leaked after denied folder delete: %d > %d", instanceType, walkProducers(), baseline)
		}
	}
}

// Count active object Walk producers
func walkProducers() int {
	var b bytes.Buffer
	pprof.Lookup("goroutine").WriteTo(&b, 2)
	n := 0
	for _, g := range strings.Split(b.String(), "\n\ngoroutine ") {
		if strings.Contains(g, "cmd.fsWalk.func") || strings.Contains(g, "cmd.(*erasureServerPools).Walk.func") {
			n++
		}
	}
	return n
}

// Locations require Fleet tier, bucket user denied and root allowed
func TestWebHandlerGetObjectLocations(t *testing.T) {
	ExecObjectLayerTest(t, testGetObjectLocationsAdminGate)
}

func testGetObjectLocationsAdminGate(obj ObjectLayer, instanceType string, t TestErrHandler) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func(s *IAMSys) { globalIAMSys = s }(globalIAMSys)
	globalIAMSys = NewIAMSys()
	globalIAMSys.Init(ctx, obj)
	defer func(e EndpointServerPools) { globalEndpoints = e }(globalEndpoints)
	globalEndpoints = mustGetPoolEndpoints("/tmp/obstor-loc-test")

	bucketName, otherBucket := getRandomBucketName(), getRandomBucketName()
	for _, b := range []string{bucketName, otherBucket} {
		if err := obj.MakeBucketWithLocation(ctx, b, BucketOptions{}); err != nil {
			t.Fatalf("%s : %s", instanceType, err)
		}
	}
	if _, err := obj.PutObject(ctx, bucketName, "obj", mustGetPutObjReader(t, bytes.NewReader([]byte("x")), 1, "", ""), ObjectOptions{}); err != nil {
		t.Fatal(err)
	}

	doc := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[
{"Effect":"Allow","Action":["s3:ListBucket","s3:GetBucketLocation"],"Resource":["arn:aws:s3:::%[1]s"]},
{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::%[1]s/*"]}]}`, bucketName)
	p, err := iampolicy.ParseConfig(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if err = globalIAMSys.SetPolicy("onebucket", *p); err != nil {
		t.Fatal(err)
	}
	if err = globalIAMSys.CreateUser("scopeduser", madmin.UserInfo{SecretKey: "scopeduser-secret", Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	if err = globalIAMSys.PolicyDBSet("scopeduser", "onebucket", false); err != nil {
		t.Fatal(err)
	}

	apiRouter := initTestWebRPCEndPoint(obj)
	call := func(accessKey, secretKey string) *httptest.ResponseRecorder {
		authorization, err := getWebRPCToken(apiRouter, accessKey, secretKey)
		if err != nil {
			t.Fatalf("Cannot authenticate %s: %v", accessKey, err)
		}
		rec := httptest.NewRecorder()
		req, err := newTestWebRPCRequest("web.GetObjectLocations", authorization, GetObjectLocationsArgs{BucketName: bucketName, Objects: []string{"obj"}})
		if err != nil {
			t.Fatalf("Failed to create HTTP request: <ERROR> %v", err)
		}
		apiRouter.ServeHTTP(rec, req)
		return rec
	}

	if code, msg := webRPCError(t, call("scopeduser", "scopeduser-secret")); code != "AccessDenied" {
		t.Fatalf("%s: bucket-scoped user must be denied object locations, got code=%q message=%q", instanceType, code, msg)
	}

	reply := &GetObjectLocationsRep{}
	if err := getTestWebRPCResponse(call(globalActiveCred.AccessKey, globalActiveCred.SecretKey), &reply); err != nil {
		t.Fatalf("%s: root must see object locations: %v", instanceType, err)
	}
	if len(reply.Objects) != 1 || reply.Objects[0].Name != "obj" || len(reply.Objects[0].Endpoints) == 0 {
		t.Fatalf("%s: root expected endpoints for obj, got %+v", instanceType, reply.Objects)
	}
}

// Locations enforce name limit and validation, preserve request order
func TestWebHandlerGetObjectLocationsInput(t *testing.T) {
	ExecObjectLayerTest(t, testGetObjectLocationsInput)
}

func testGetObjectLocationsInput(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer func(e EndpointServerPools) { globalEndpoints = e }(globalEndpoints)
	globalEndpoints = mustGetPoolEndpoints("/tmp/obstor-loc-test")
	router := initTestWebRPCEndPoint(obj)
	bucket := makeTestBucket(t, obj, BucketOptions{})
	if _, err := obj.PutObject(context.Background(), bucket, "b", mustGetPutObjReader(t, bytes.NewReader([]byte("x")), 1, "", ""), ObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	locate := func(objects []string) (*GetObjectLocationsRep, error) {
		reply := &GetObjectLocationsRep{}
		err := rootWebRPC(t, router, "GetObjectLocations", &GetObjectLocationsArgs{BucketName: bucket, Objects: objects}, reply)
		return reply, err
	}

	tooMany := make([]string, maxObjectLocations+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("o%d", i)
	}
	if _, err := locate(tooMany); err == nil || err.Error() != "Ask for at most 1000 objects per call." {
		t.Fatalf("%s: 1001 names: got %v, want the per-call cap error", instanceType, err)
	}
	if _, err := locate([]string{"b", "dir/"}); err == nil || err.Error() != "Object name is not valid: dir/" {
		t.Fatalf("%s: folder name: got %v, want the invalid name error", instanceType, err)
	}

	reply, err := locate([]string{})
	if err != nil || reply.Objects == nil || len(reply.Objects) != 0 {
		t.Fatalf("%s: no names: got %+v, %v; want an empty list", instanceType, reply.Objects, err)
	}

	reply, err = locate([]string{"zz-missing", "b"})
	if err != nil {
		t.Fatalf("%s: two names: %v", instanceType, err)
	}
	if len(reply.Objects) != 2 || reply.Objects[0].Name != "zz-missing" || reply.Objects[1].Name != "b" {
		t.Fatalf("%s: want rows zz-missing then b, got %+v", instanceType, reply.Objects)
	}
	for _, row := range reply.Objects {
		if len(row.Endpoints) == 0 {
			t.Fatalf("%s: single-pool row %q has no endpoints", instanceType, row.Name)
		}
	}
}

// Erasure returns hashed set drives, FS all endpoints, multiple pools probe first
func TestObjectLocationEndpoints(t *testing.T) {
	ExecObjectLayerTest(t, testObjectLocationEndpoints)
	testObjectLocationEndpointsPools(t)
}

func testObjectLocationEndpoints(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer func(e EndpointServerPools) { globalEndpoints = e }(globalEndpoints)
	globalEndpoints = mustGetPoolEndpoints("/tmp/obstor-loc-1", "/tmp/obstor-loc-2", "/tmp/obstor-loc-3", "/tmp/obstor-loc-4")
	var all []string
	for _, ep := range globalEndpoints[0].Endpoints {
		all = append(all, ep.String())
	}

	for _, name := range []string{"a", "dir/b", "missing"} {
		want := all
		if instanceType == ErasureTestStr {
			want = obj.(*erasureServerPools).serverPools[0].getHashedSet(name).getEndpoints()
		}
		got := objectLocationEndpoints(context.Background(), obj, "anybucket", name)
		if len(want) == 0 || strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("%s: %q endpoints = %v, want %v", instanceType, name, got, want)
		}
	}
}

func testObjectLocationEndpointsPools(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dirs, err := getRandomDisks(8)
	if err != nil {
		t.Fatal(err)
	}
	defer removeRoots(dirs)
	obj, _, err := initObjectLayer(ctx, EndpointServerPools{
		{SetCount: 1, DrivesPerSet: 4, Endpoints: mustGetNewEndpoints(dirs[:4]...)},
		{SetCount: 1, DrivesPerSet: 4, Endpoints: mustGetNewEndpoints(dirs[4:]...)},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Shutdown(context.Background())
	z := obj.(*erasureServerPools)

	bucket := makeTestBucket(t, obj, BucketOptions{})
	if _, err = obj.PutObject(ctx, bucket, "placed", mustGetPutObjReader(t, bytes.NewReader([]byte("x")), 1, "", ""), ObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, pool := range z.serverPools {
		if _, err := pool.GetObjectInfo(ctx, bucket, "placed", ObjectOptions{}); err == nil {
			want = pool.getHashedSet("placed").getEndpoints()
		}
	}
	got := objectLocationEndpoints(ctx, obj, bucket, "placed")
	if len(want) == 0 || strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("pools: placed endpoints = %v, want %v", got, want)
	}
	if got := objectLocationEndpoints(ctx, obj, bucket, "missing"); got != nil {
		t.Fatalf("pools: missing object endpoints = %v, want nil", got)
	}
}

// Wrapper for calling ServerInfo Web Handler
func TestWebHandlerServerInfo(t *testing.T) {
	ExecObjectLayerTest(t, testServerInfoWebHandler)
}

// testServerInfoWebHandler - Test ServerInfo web handler
func testServerInfoWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	// Register the API end points with Erasure/FS object layer.
	apiRouter := initTestWebRPCEndPoint(obj)
	credentials := globalActiveCred

	authorization, err := getWebRPCToken(apiRouter, credentials.AccessKey, credentials.SecretKey)
	if err != nil {
		t.Fatal("Cannot authenticate")
	}

	rec := httptest.NewRecorder()

	serverInfoRequest := &WebGenericArgs{}
	serverInfoReply := &ServerInfoRep{}
	req, err := newTestWebRPCRequest("web.ServerInfo", authorization, serverInfoRequest)
	if err != nil {
		t.Fatalf("Failed to create HTTP request: <ERROR> %v", err)
	}
	apiRouter.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected the response status to be 200, but instead found `%d`", rec.Code)
	}
	err = getTestWebRPCResponse(rec, &serverInfoReply)
	if err != nil {
		t.Fatalf("Failed, %v", err)
	}
	if serverInfoReply.ObstorVersion != Version {
		t.Fatalf("Cannot get obstor version from server info handler")
	}
}

// Settings fields require owner or admin:ServerInfo
func TestWebHandlerServerInfoSettingsTier(t *testing.T) {
	ExecObjectLayerTest(t, testServerInfoSettingsTierWebHandler)
}

func testServerInfoSettingsTierWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	defer useTestKMS(t)()
	defer func(iam *IAMSys, tls bool, boot time.Time) {
		globalIAMSys, globalIsTLS, globalBootTime = iam, tls, boot
	}(globalIAMSys, globalIsTLS, globalBootTime)
	defer func() { _ = handlers.SetTrustedProxies(nil) }()

	globalIAMSys = NewIAMSys()
	globalIAMSys.InitStore(obj)
	makeDocIAMUser(t, "infouser", `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["admin:ServerInfo"]}]}`)
	plain := makeTestIAMUser(t, "plainuser", "s3:ListBucket")

	globalAutoEncryption = true
	globalIsTLS = true
	globalBootTime = time.Date(2026, 10, 1, 8, 30, 15, 500, time.UTC)
	if err := handlers.SetTrustedProxies([]string{"127.0.0.1"}); err != nil {
		t.Fatalf("%s: set trusted proxies: %v", instanceType, err)
	}

	router := initTestWebRPCEndPoint(obj)
	serverInfo := func(accessKey, secretKey string) ServerInfoRep {
		var reply ServerInfoRep
		if err := userWebRPC(t, router, accessKey, secretKey, "ServerInfo", &WebGenericArgs{}, &reply); err != nil {
			t.Fatalf("%s: ServerInfo as %s: %v", instanceType, accessKey, err)
		}
		return reply
	}
	wantStarted := "2026-10-01T08:30:15Z"
	check := func(who string, reply ServerInfoRep, settings bool) {
		if reply.ObstorVersion != Version || reply.UIVersion != Version {
			t.Fatalf("%s: %s version %q ui %q, want %q for every caller", instanceType, who, reply.ObstorVersion, reply.UIVersion, Version)
		}
		if reply.CanSeeSettings != settings {
			t.Fatalf("%s: %s canSeeSettings %v, want %v", instanceType, who, reply.CanSeeSettings, settings)
		}
		if !settings {
			if reply.Commit != "" || reply.StartedAt != "" || reply.ObstorPlatform != "" || reply.ObstorRuntime != "" || reply.SFTPAddress != "" ||
				reply.KMS || reply.AutoEncryption || reply.TLS || reply.TrustedProxies {
				t.Fatalf("%s: %s got Settings-tier fields: %+v", instanceType, who, reply)
			}
			return
		}
		if reply.Commit != ShortCommitID {
			t.Fatalf("%s: %s commit %q, want %q", instanceType, who, reply.Commit, ShortCommitID)
		}
		started, err := time.Parse(time.RFC3339, reply.StartedAt)
		if err != nil || reply.StartedAt != wantStarted || !started.Equal(globalBootTime.Truncate(time.Second)) {
			t.Fatalf("%s: %s startedAt %q (%v), want %q", instanceType, who, reply.StartedAt, err, wantStarted)
		}
		if !reply.KMS || !reply.AutoEncryption || !reply.TLS || !reply.TrustedProxies {
			t.Fatalf("%s: %s kms=%v autoEncryption=%v tls=%v trustedProxies=%v, want all true",
				instanceType, who, reply.KMS, reply.AutoEncryption, reply.TLS, reply.TrustedProxies)
		}
		if !strings.HasPrefix(reply.ObstorPlatform, "Host: ") || !strings.HasPrefix(reply.ObstorRuntime, "Version: ") {
			t.Fatalf("%s: %s platform %q runtime %q, want both filled", instanceType, who, reply.ObstorPlatform, reply.ObstorRuntime)
		}
	}

	check("root", serverInfo(globalActiveCred.AccessKey, globalActiveCred.SecretKey), true)
	check("infouser", serverInfo("infouser", "infouser-secret"), true)
	check("plainuser", serverInfo(plain.AccessKey, plain.SecretKey), false)

	// JSON names read by console
	wire, err := json.Marshal(serverInfo(globalActiveCred.AccessKey, globalActiveCred.SecretKey))
	if err != nil {
		t.Fatalf("%s: marshal reply: %v", instanceType, err)
	}
	for _, key := range []string{"canSeeSettings", "commit", "startedAt", "kms", "autoEncryption", "tls", "trustedProxies", "ObstorPlatform", "ObstorRuntime"} {
		if !strings.Contains(string(wire), `"`+key+`":`) {
			t.Fatalf("%s: reply JSON lacks %q: %s", instanceType, key, wire)
		}
	}

	// Plain server reports flags off
	GlobalKMS, globalAutoEncryption, globalIsTLS = nil, false, false
	if err = handlers.SetTrustedProxies(nil); err != nil {
		t.Fatalf("%s: clear trusted proxies: %v", instanceType, err)
	}
	off := serverInfo(globalActiveCred.AccessKey, globalActiveCred.SecretKey)
	if !off.CanSeeSettings || off.KMS || off.AutoEncryption || off.TLS || off.TrustedProxies {
		t.Fatalf("%s: plain server: canSeeSettings=%v kms=%v autoEncryption=%v tls=%v trustedProxies=%v, want true then all false",
			instanceType, off.CanSeeSettings, off.KMS, off.AutoEncryption, off.TLS, off.TrustedProxies)
	}
}

// Wrapper for calling MakeBucket Web Handler
func TestWebHandlerMakeBucket(t *testing.T) {
	ExecObjectLayerTest(t, testMakeBucketWebHandler)
}

// testMakeBucketWebHandler - Test MakeBucket web handler
func testMakeBucketWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	// Register the API end points with Erasure/FS object layer.
	apiRouter := initTestWebRPCEndPoint(obj)
	credentials := globalActiveCred

	authorization, err := getWebRPCToken(apiRouter, credentials.AccessKey, credentials.SecretKey)
	if err != nil {
		t.Fatal("Cannot authenticate")
	}

	rec := httptest.NewRecorder()

	bucketName := getRandomBucketName()

	testCases := []struct {
		bucketName string
		success    bool
	}{
		{"", false},
		{".", false},
		{"ab", false},
		{"obstor", false},
		{obstorMetaBucket, false},
		{bucketName, true},
	}

	for i, testCase := range testCases {
		makeBucketRequest := MakeBucketArgs{BucketName: testCase.bucketName}
		makeBucketReply := &WebGenericRep{}
		req, err := newTestWebRPCRequest("web.MakeBucket", authorization, makeBucketRequest)
		if err != nil {
			t.Fatalf("Test %d: Failed to create HTTP request: <ERROR> %v", i+1, err)
		}
		apiRouter.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("Test %d: Expected the response status to be 200, but instead found `%d`", i+1, rec.Code)
		}
		err = getTestWebRPCResponse(rec, &makeBucketReply)
		if testCase.success && err != nil {
			t.Fatalf("Test %d: Should succeed but it didn't, %v", i+1, err)
		}
		if !testCase.success && err == nil {
			t.Fatalf("Test %d: Should fail but it didn't (%s)", i+1, testCase.bucketName)
		}
	}
}

// Wrapper for calling DeleteBucket handler
func TestWebHandlerDeleteBucket(t *testing.T) {
	ExecObjectLayerTest(t, testDeleteBucketWebHandler)
}

// testDeleteBucketWebHandler - Test DeleteBucket web handler
func testDeleteBucketWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	apiRouter := initTestWebRPCEndPoint(obj)

	credentials := globalActiveCred
	token, err := getWebRPCToken(apiRouter, credentials.AccessKey, credentials.SecretKey)
	if err != nil {
		t.Fatalf("could not get RPC token, %s", err.Error())
	}

	bucketName := getRandomBucketName()
	var opts ObjectOptions

	err = obj.MakeBucketWithLocation(context.Background(), bucketName, BucketOptions{})
	if err != nil {
		t.Fatalf("failed to create bucket: %s (%s)", err.Error(), instanceType)
	}

	testCases := []struct {
		bucketName string
		// Whether or not to put an object into the bucket.
		initWithObject bool
		token          string
		// Expected error (error must only contain this string to pass test)
		// Empty string = no error
		expect string
	}{
		{"", false, token, "Bucket Name  is invalid. Lowercase letters, period, " +
			"hyphen, numerals are the only allowed characters and should be minimum " +
			"3 characters in length."},
		{".", false, "auth", "session token invalid"},
		{".", false, token, "Bucket Name . is invalid. Lowercase letters, period, " +
			"hyphen, numerals are the only allowed characters and should be minimum " +
			"3 characters in length."},
		{"..", false, token, "Bucket Name .. is invalid. Lowercase letters, period, " +
			"hyphen, numerals are the only allowed characters and should be minimum " +
			"3 characters in length."},
		{"ab", false, token, "Bucket Name ab is invalid. Lowercase letters, period, " +
			"hyphen, numerals are the only allowed characters and should be minimum " +
			"3 characters in length."},
		{"obstor", false, "false token", "session token invalid"},
		{"obstor", false, token, "Bucket Name obstor is invalid. Lowercase letters, period, " +
			"hyphen, numerals are the only allowed characters and should be minimum " +
			"3 characters in length."},
		{bucketName, false, token, ""},
		{bucketName, true, token, "The bucket you tried to delete is not empty"},
		{bucketName, false, "", "jwt token missing"},
	}

	for _, test := range testCases {
		if test.initWithObject {
			data := bytes.NewBufferString("hello")
			_, err = obj.PutObject(context.Background(), test.bucketName, "object", mustGetPutObjReader(t, data, int64(data.Len()), "", ""), opts)
			// _, err = obj.PutObject(test.bucketName, "object", int64(data.Len()), data, nil, "")
			if err != nil {
				t.Fatalf("could not put object to %s, %s", test.bucketName, err.Error())
			}
		}

		rec := httptest.NewRecorder()

		makeBucketRequest := MakeBucketArgs{BucketName: test.bucketName}
		makeBucketReply := &WebGenericRep{}

		req, err := newTestWebRPCRequest("web.DeleteBucket", test.token, makeBucketRequest)
		if err != nil {
			t.Errorf("failed to create HTTP request: <ERROR> %v", err)
		}

		apiRouter.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected the response status to be `%d`, but instead found `%d`", http.StatusOK, rec.Code)
		}
		err = getTestWebRPCResponse(rec, &makeBucketReply)

		if test.expect != "" {
			if err == nil {
				// If we expected an error, but didn't get one.
				t.Errorf("expected `..%s..` but got nil error", test.expect)
			} else if !strings.Contains(err.Error(), test.expect) {
				// If we got an error that wasn't what we expected.
				t.Errorf("expected `..%s..` but got `%s`", test.expect, err.Error())
			}
		} else if test.expect == "" && err != nil {
			t.Errorf("expected test success, but got `%s`", err.Error())
		}

		// If we created the bucket with an object, now delete the object to cleanup.
		if test.initWithObject {
			_, err = obj.DeleteObject(context.Background(), test.bucketName, "object", ObjectOptions{})
			if err != nil {
				t.Fatalf("could not delete object, %s", err.Error())
			}
		}

		// If it did not succeed in deleting the bucket, don't try and recreate it.
		// Or, it'll fail if there was an object.
		if err != nil || test.initWithObject {
			continue
		}

		err = obj.MakeBucketWithLocation(context.Background(), bucketName, BucketOptions{})
		if err != nil {
			// failed to create new bucket, abort.
			t.Fatalf("failed to create new bucket (%s): %s", instanceType, err.Error())
		}
	}
}

// Wrapper for calling ListBuckets Web Handler
func TestWebHandlerListBuckets(t *testing.T) {
	ExecObjectLayerTest(t, testListBucketsWebHandler)
}

// testListBucketsHandler - Test ListBuckets web handler
func testListBucketsWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	// Register the API end points with Erasure/FS object layer.
	apiRouter := initTestWebRPCEndPoint(obj)
	credentials := globalActiveCred

	authorization, err := getWebRPCToken(apiRouter, credentials.AccessKey, credentials.SecretKey)
	if err != nil {
		t.Fatal("Cannot authenticate")
	}

	rec := httptest.NewRecorder()

	bucketName := getRandomBucketName()
	// Create bucket.
	err = obj.MakeBucketWithLocation(context.Background(), bucketName, BucketOptions{})
	if err != nil {
		// failed to create newbucket, abort.
		t.Fatalf("%s : %s", instanceType, err)
	}

	listBucketsRequest := WebGenericArgs{}
	listBucketsReply := &ListBucketsRep{}
	req, err := newTestWebRPCRequest("web.ListBuckets", authorization, listBucketsRequest)
	if err != nil {
		t.Fatalf("Failed to create HTTP request: <ERROR> %v", err)
	}
	apiRouter.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected the response status to be 200, but instead found `%d`", rec.Code)
	}
	err = getTestWebRPCResponse(rec, &listBucketsReply)
	if err != nil {
		t.Fatalf("Failed, %v", err)
	}
	if len(listBucketsReply.Buckets) == 0 {
		t.Fatalf("Cannot find the bucket already created by MakeBucket")
	}
	if listBucketsReply.Buckets[0].Name != bucketName {
		t.Fatalf("Found another bucket %q other than already created by MakeBucket", listBucketsReply.Buckets[0].Name)
	}
}

func TestWebHandlerListBucketsUsage(t *testing.T) {
	ExecObjectLayerTest(t, testListBucketsUsageWebHandler)
}

// Listable buckets get scanned usage, null before scan and on backends
func testListBucketsUsageWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	router := initTestWebRPCEndPoint(obj)
	used := makeTestBucket(t, obj, BucketOptions{})
	fresh := makeTestBucket(t, obj, BucketOptions{})
	asOf := time.Date(2026, time.September, 25, 6, 0, 0, 0, time.UTC)

	ctx := context.Background()
	snapshots := make(chan madmin.DataUsageInfo, 1)
	snapshots <- madmin.DataUsageInfo{
		LastUpdate:        asOf,
		ObjectsTotalCount: 3,
		ObjectsTotalSize:  2048,
		BucketsCount:      2,
		BucketsUsage:      map[string]madmin.BucketUsageInfo{used: {Size: 2048, ObjectsCount: 3}},
	}
	close(snapshots)
	storeDataUsageInBackend(ctx, obj, snapshots)
	if dui, err := loadDataUsageFromBackend(ctx, obj); err != nil || !dui.LastUpdate.Equal(asOf) {
		t.Fatalf("%s: seeded snapshot LastUpdate = %v (%v), want %v", instanceType, dui.LastUpdate, err, asOf)
	}

	// Pointers distinguish null from zero
	type usageRow struct {
		Name    string  `json:"name"`
		Size    *uint64 `json:"size"`
		Objects *uint64 `json:"objects"`
	}
	type usageReply struct {
		Buckets   []usageRow `json:"buckets"`
		UsageAsOf string     `json:"usageAsOf"`
	}
	show := func(p *uint64) string {
		if p == nil {
			return "null"
		}
		return strconv.FormatUint(*p, 10)
	}
	list := func() (map[string]usageRow, string) {
		var rep usageReply
		if err := rootWebRPC(t, router, "ListBuckets", &WebGenericArgs{}, &rep); err != nil {
			t.Fatalf("%s: ListBuckets: %v", instanceType, err)
		}
		rows := make(map[string]usageRow, len(rep.Buckets))
		for _, r := range rep.Buckets {
			rows[r.Name] = r
		}
		if _, ok := rows[used]; !ok {
			t.Fatalf("%s: ListBuckets has no row for %s", instanceType, used)
		}
		if _, ok := rows[fresh]; !ok {
			t.Fatalf("%s: ListBuckets has no row for %s", instanceType, fresh)
		}
		return rows, rep.UsageAsOf
	}

	rows, usageAsOf := list()
	if r := rows[used]; r.Size == nil || r.Objects == nil || *r.Size != 2048 || *r.Objects != 3 {
		t.Fatalf("%s: used row = size %s objects %s, want size 2048 objects 3", instanceType, show(r.Size), show(r.Objects))
	}
	if r := rows[fresh]; r.Size != nil || r.Objects != nil {
		t.Fatalf("%s: fresh row = size %s objects %s, want size null objects null", instanceType, show(r.Size), show(r.Objects))
	}
	if usageAsOf != "2026-09-25T06:00:00Z" {
		t.Fatalf("%s: usageAsOf = %q, want %q", instanceType, usageAsOf, "2026-09-25T06:00:00Z")
	}

	// Scoped caller sees usage of listable buckets only
	globalIAMSys.InitStore(obj)
	doc := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:ListBucket"],"Resource":["arn:aws:s3:::%s"]}]}`, used)
	p, err := iampolicy.ParseConfig(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if err = globalIAMSys.SetPolicy("usagepolicy", *p); err != nil {
		t.Fatal(err)
	}
	if err = globalIAMSys.CreateUser("usageuser", madmin.UserInfo{SecretKey: "usageuser-secret", Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	if err = globalIAMSys.PolicyDBSet("usageuser", "usagepolicy", false); err != nil {
		t.Fatal(err)
	}
	var scoped usageReply
	if err := userWebRPC(t, router, "usageuser", "usageuser-secret", "ListBuckets", &WebGenericArgs{}, &scoped); err != nil {
		t.Fatalf("%s: scoped ListBuckets: %v", instanceType, err)
	}
	if len(scoped.Buckets) != 1 || scoped.Buckets[0].Name != used || show(scoped.Buckets[0].Size) != "2048" {
		t.Fatalf("%s: scoped rows = %+v, want only %s with size 2048", instanceType, scoped.Buckets, used)
	}

	globalIsBackend = true
	rows, usageAsOf = list()
	for _, name := range []string{used, fresh} {
		if r := rows[name]; r.Size != nil || r.Objects != nil {
			t.Fatalf("%s: backend %s row = size %s objects %s, want size null objects null", instanceType, name, show(r.Size), show(r.Objects))
		}
	}
	if usageAsOf != "" {
		t.Fatalf("%s: backend usageAsOf = %q, want empty", instanceType, usageAsOf)
	}
}

// Wrapper for calling ListObjects Web Handler
func TestWebHandlerListObjects(t *testing.T) {
	ExecObjectLayerTest(t, testListObjectsWebHandler)
}

// testListObjectsHandler - Test ListObjects web handler
func testListObjectsWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	// Register the API end points with Erasure/FS object layer.
	apiRouter := initTestWebRPCEndPoint(obj)
	credentials := globalActiveCred

	rec := httptest.NewRecorder()

	authorization, err := getWebRPCToken(apiRouter, credentials.AccessKey, credentials.SecretKey)
	if err != nil {
		t.Fatal("Cannot authenticate")
	}

	bucketName := getRandomBucketName()
	objectName := "object"
	objectSize := 1 * humanize.KiByte

	// Create bucket.
	err = obj.MakeBucketWithLocation(context.Background(), bucketName, BucketOptions{})
	if err != nil {
		// failed to create newbucket, abort.
		t.Fatalf("%s : %s", instanceType, err)
	}

	data := bytes.Repeat([]byte("a"), objectSize)
	metadata := map[string]string{"etag": "c9a34cfc85d982698c6ac89f76071abd"}
	_, err = obj.PutObject(context.Background(), bucketName, objectName, mustGetPutObjReader(t, bytes.NewReader(data), int64(len(data)), metadata["etag"], ""), ObjectOptions{UserDefined: metadata})

	if err != nil {
		t.Fatalf("Was not able to upload an object, %v", err)
	}

	test := func(token string) (*ListObjectsRep, error) {
		listObjectsRequest := ListObjectsArgs{BucketName: bucketName, Prefix: ""}
		listObjectsReply := &ListObjectsRep{}
		var req *http.Request
		req, err = newTestWebRPCRequest("web.ListObjects", token, listObjectsRequest)
		if err != nil {
			return nil, err
		}
		apiRouter.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			return listObjectsReply, fmt.Errorf("expected the response status to be 200, but instead found `%d`", rec.Code)
		}
		err = getTestWebRPCResponse(rec, &listObjectsReply)
		if err != nil {
			return listObjectsReply, err
		}
		return listObjectsReply, nil
	}
	verifyReply := func(reply *ListObjectsRep) {
		if len(reply.Objects) == 0 {
			t.Fatalf("Cannot find the object")
		}
		if reply.Objects[0].Key != objectName {
			t.Fatalf("Found another object other than already created by PutObject")
		}
		if reply.Objects[0].Size != int64(objectSize) {
			t.Fatalf("Found a object with the same name but with a different size")
		}
	}

	// Authenticated ListObjects should succeed.
	reply, err := test(authorization)
	if err != nil {
		t.Fatal(err)
	}
	verifyReply(reply)

	// Unauthenticated ListObjects should fail.
	_, err = test("")
	if err == nil {
		t.Fatalf("Expected error `%s`", err)
	}
}

// Wrapper for calling RemoveObject Web Handler
func TestWebHandlerRemoveObject(t *testing.T) {
	ExecObjectLayerTest(t, testRemoveObjectWebHandler)
}

// testRemoveObjectWebHandler - Test RemoveObjectObject web handler
func testRemoveObjectWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	// Register the API end points with Erasure/FS object layer.
	apiRouter := initTestWebRPCEndPoint(obj)
	credentials := globalActiveCred

	rec := httptest.NewRecorder()
	authorization, err := getWebRPCToken(apiRouter, credentials.AccessKey, credentials.SecretKey)
	if err != nil {
		t.Fatal("Cannot authenticate")
	}

	bucketName := getRandomBucketName()
	objectName := "object"
	objectSize := 1 * humanize.KiByte

	// Create bucket.
	err = obj.MakeBucketWithLocation(context.Background(), bucketName, BucketOptions{})
	if err != nil {
		// failed to create newbucket, abort.
		t.Fatalf("%s : %s", instanceType, err)
	}

	data := bytes.Repeat([]byte("a"), objectSize)
	metadata := map[string]string{"etag": "c9a34cfc85d982698c6ac89f76071abd"}
	_, err = obj.PutObject(context.Background(), bucketName, objectName, mustGetPutObjReader(t, bytes.NewReader(data), int64(len(data)), metadata["etag"], ""), ObjectOptions{UserDefined: metadata})
	if err != nil {
		t.Fatalf("Was not able to upload an object, %v", err)
	}

	objectName = "a/object"
	metadata = map[string]string{"etag": "c9a34cfc85d982698c6ac89f76071abd"}
	_, err = obj.PutObject(context.Background(), bucketName, objectName, mustGetPutObjReader(t, bytes.NewReader(data), int64(len(data)), metadata["etag"], ""), ObjectOptions{UserDefined: metadata})
	if err != nil {
		t.Fatalf("Was not able to upload an object, %v", err)
	}

	removeRequest := RemoveObjectArgs{BucketName: bucketName, Objects: []string{"a/", "object"}}
	removeReply := &WebGenericRep{}
	req, err := newTestWebRPCRequest("web.RemoveObject", authorization, removeRequest)
	if err != nil {
		t.Fatalf("Failed to create HTTP request: <ERROR> %v", err)
	}
	apiRouter.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected the response status to be 200, but instead found `%d`", rec.Code)
	}
	err = getTestWebRPCResponse(rec, &removeReply)
	if err != nil {
		t.Fatalf("Failed, %v", err)
	}
	// Folder first still processes later names
	for _, name := range []string{"a/object", "object"} {
		if _, err = obj.GetObjectInfo(context.Background(), bucketName, name, ObjectOptions{}); !isErrObjectNotFound(err) {
			t.Fatalf("%s: expected %s deleted, got err=%v", instanceType, name, err)
		}
	}

	rec = httptest.NewRecorder()
	removeRequest = RemoveObjectArgs{BucketName: bucketName, Objects: []string{"a/", "object"}}
	removeReply = &WebGenericRep{}
	req, err = newTestWebRPCRequest("web.RemoveObject", authorization, removeRequest)
	if err != nil {
		t.Fatalf("Failed to create HTTP request: <ERROR> %v", err)
	}
	apiRouter.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected the response status to be 200, but instead found `%d`", rec.Code)
	}
	err = getTestWebRPCResponse(rec, &removeReply)
	if err != nil {
		t.Fatalf("Failed, %v", err)
	}

	removeRequest = RemoveObjectArgs{BucketName: bucketName}
	removeReply = &WebGenericRep{}
	req, err = newTestWebRPCRequest("web.RemoveObject", authorization, removeRequest)
	if err != nil {
		t.Fatalf("Failed to create HTTP request: <ERROR> %v", err)
	}
	apiRouter.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected the response status to be 200, but instead found `%d`", rec.Code)
	}
	b, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte("invalid arguments specified")) {
		t.Fatalf("Expected response wrong %s", string(b))
	}
}

func TestWebCreateURLToken(t *testing.T) {
	ExecObjectLayerTest(t, testCreateURLToken)
}

func getTokenString(accessKey, secretKey string) (string, error) {
	claims := xjwt.NewMapClaims()
	claims.SetExpiry(UTCNow().Add(defaultJWTExpiry))
	claims.SetAccessKey(accessKey)
	token := jwtgo.NewWithClaims(jwtgo.SigningMethodHS512, claims)
	return token.SignedString([]byte(secretKey))
}

func testCreateURLToken(obj ObjectLayer, instanceType string, t TestErrHandler) {
	apiRouter := initTestWebRPCEndPoint(obj)
	credentials := globalActiveCred

	authorization, err := getWebRPCToken(apiRouter, credentials.AccessKey, credentials.SecretKey)
	if err != nil {
		t.Fatal(err)
	}

	args := WebGenericArgs{}
	tokenReply := &URLTokenReply{}

	req, err := newTestWebRPCRequest("web.CreateURLToken", authorization, args)
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	apiRouter.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected the response status to be 200, but instead found `%d`", rec.Code)
	}

	err = getTestWebRPCResponse(rec, &tokenReply)
	if err != nil {
		t.Fatal(err)
	}

	// Ensure the token is valid now. It will expire later.
	if !isAuthTokenValid(tokenReply.Token) {
		t.Fatalf("token is not valid")
	}

	// Token is invalid.
	if isAuthTokenValid("") {
		t.Fatalf("token shouldn't be valid, but it is")
	}

	token, err := getTokenString("invalid-access", credentials.SecretKey)
	if err != nil {
		t.Fatal(err)
	}

	// Token has invalid access key.
	if isAuthTokenValid(token) {
		t.Fatalf("token shouldn't be valid, but it is")
	}
}

// Wrapper for calling Upload Handler
func TestWebHandlerUpload(t *testing.T) {
	ExecObjectLayerTest(t, testUploadWebHandler)
}

// testUploadWebHandler - Test Upload web handler
func testUploadWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	// Register the API end points with Erasure/FS object layer.
	apiRouter := initTestWebRPCEndPoint(obj)
	credentials := globalActiveCred

	content := []byte("temporary file's content")
	authorization, err := getWebRPCToken(apiRouter, credentials.AccessKey, credentials.SecretKey)
	if err != nil {
		t.Fatal("Cannot authenticate")
	}

	objectName := "test.file"
	bucketName := getRandomBucketName()

	test := func(token string, sendContentLength bool) int {
		rec := httptest.NewRecorder()
		req, rErr := http.NewRequest(http.MethodPut, "/obstor/upload/"+bucketName+SlashSeparator+objectName, nil)
		if rErr != nil {
			t.Fatalf("Cannot create upload request, %v", rErr)
		}

		req.Header.Set("x-amz-date", "20160814T114029Z")
		req.Header.Set("Accept", "*/*")
		req.Header.Set("User-Agent", "Mozilla")

		req.Body = io.NopCloser(bytes.NewReader(content))

		if !sendContentLength {
			req.ContentLength = -1
		} else {
			req.ContentLength = int64(len(content))
		}

		if token != "" {
			req.Header.Set("Authorization", "Bearer "+authorization)
		}
		apiRouter.ServeHTTP(rec, req)
		return rec.Code
	}
	// Create bucket.
	err = obj.MakeBucketWithLocation(context.Background(), bucketName, BucketOptions{})
	if err != nil {
		// failed to create newbucket, abort.
		t.Fatalf("%s : %s", instanceType, err)
	}

	// Authenticated upload should succeed.
	code := test(authorization, true)
	if code != http.StatusOK {
		t.Fatalf("Expected the response status to be 200, but instead found `%d`", code)
	}

	var byteBuffer bytes.Buffer
	err = GetObject(context.Background(), obj, bucketName, objectName, 0, int64(len(content)), &byteBuffer, "", ObjectOptions{})
	if err != nil {
		t.Fatalf("Failed, %v", err)
	}

	if !bytes.Equal(byteBuffer.Bytes(), content) {
		t.Fatalf("The upload file is different from the download file")
	}

	// Authenticated upload without content-length should fail
	code = test(authorization, false)
	if code != http.StatusBadRequest {
		t.Fatalf("Expected the response status to be 200, but instead found `%d`", code)
	}

	// Unauthenticated upload should fail.
	code = test("", true)
	if code != http.StatusForbidden {
		t.Fatalf("Expected the response status to be 403, but instead found `%d`", code)
	}

}

// Wrapper for calling Download Handler
func TestWebHandlerDownload(t *testing.T) {
	ExecObjectLayerTest(t, testDownloadWebHandler)
}

// testDownloadWebHandler - Test Download web handler
func testDownloadWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	// Register the API end points with Erasure/FS object layer.
	apiRouter := initTestWebRPCEndPoint(obj)
	credentials := globalActiveCred

	authorization, err := getWebRPCToken(apiRouter, credentials.AccessKey, credentials.SecretKey)
	if err != nil {
		t.Fatal("Cannot authenticate")
	}

	objectName := "test.file"
	bucketName := getRandomBucketName()

	test := func(token string) (int, []byte) {
		rec := httptest.NewRecorder()
		path := "/obstor/download/" + bucketName + SlashSeparator + objectName + "?token="
		if token != "" {
			path = path + token
		}
		var req *http.Request
		req, err = http.NewRequest(http.MethodGet, path, nil)

		if err != nil {
			t.Fatalf("Cannot create upload request, %v", err)
		}

		req.Header.Set("User-Agent", "Mozilla")

		apiRouter.ServeHTTP(rec, req)
		return rec.Code, rec.Body.Bytes()
	}

	// Create bucket.
	err = obj.MakeBucketWithLocation(context.Background(), bucketName, BucketOptions{})
	if err != nil {
		// failed to create newbucket, abort.
		t.Fatalf("%s : %s", instanceType, err)
	}

	content := []byte("temporary file's content")
	metadata := map[string]string{"etag": "01ce59706106fe5e02e7f55fffda7f34"}
	_, err = obj.PutObject(context.Background(), bucketName, objectName, mustGetPutObjReader(t, bytes.NewReader(content), int64(len(content)), metadata["etag"], ""), ObjectOptions{UserDefined: metadata})
	if err != nil {
		t.Fatalf("Was not able to upload an object, %v", err)
	}

	// Authenticated download should succeed.
	code, bodyContent := test(authorization)

	if code != http.StatusOK {
		t.Fatalf("Expected the response status to be 200, but instead found `%d`", code)
	}

	if !bytes.Equal(bodyContent, content) {
		t.Fatalf("The downloaded file is corrupted")
	}

	// Temporary token should succeed.
	tmpToken, err := authenticateURL(credentials.AccessKey, credentials.SecretKey)
	if err != nil {
		t.Fatal(err)
	}

	code, bodyContent = test(tmpToken)

	if code != http.StatusOK {
		t.Fatalf("Expected the response status to be 200, but instead found `%d`", code)
	}

	if !bytes.Equal(bodyContent, content) {
		t.Fatalf("The downloaded file is corrupted")
	}

	// Old token should fail.
	code, bodyContent = test("eyJhbGciOiJIUzUxMiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE1MDAzMzIwOTUsImlhdCI6MTUwMDMzMjAzNSwic3ViIjoiRFlLSU01VlRZNDBJMVZQSE5VMTkifQ.tXQ45GJc8eOFet_a4VWVyeqJEOPWybotQYNr2zVxBpEOICkGbu_YWGhd9TkLLe1E65oeeiLHPdXSN8CzcbPoRA")
	if code != http.StatusForbidden {
		t.Fatalf("Expected the response status to be 403, but instead found `%d`", code)
	}

	if !bytes.Equal(bodyContent, bytes.NewBufferString("authentication failed, check your access credentials").Bytes()) {
		t.Fatalf("Expected authentication error message, got %s", string(bodyContent))
	}

	// Unauthenticated download should fail.
	code, _ = test("")
	if code != http.StatusForbidden {
		t.Fatalf("Expected the response status to be 403, but instead found `%d`", code)
	}
}

// Test web.DownloadZip
func TestWebHandlerDownloadZip(t *testing.T) {
	ExecObjectLayerTest(t, testWebHandlerDownloadZip)
}

func testWebHandlerDownloadZip(obj ObjectLayer, instanceType string, t TestErrHandler) {
	apiRouter := initTestWebRPCEndPoint(obj)
	credentials := globalActiveCred
	var opts ObjectOptions

	authorization, err := authenticateURL(credentials.AccessKey, credentials.SecretKey)
	if err != nil {
		t.Fatal("Cannot authenticate")
	}

	bucket := getRandomBucketName()
	fileOne := "aaaaaaaaaaaaaa"
	fileTwo := "bbbbbbbbbbbbbb"
	fileThree := "cccccccccccccc"

	// Create bucket.
	err = obj.MakeBucketWithLocation(context.Background(), bucket, BucketOptions{})
	if err != nil {
		// failed to create newbucket, abort.
		t.Fatalf("%s : %s", instanceType, err)
	}

	for objName, value := range map[string]string{
		"a/one":     fileOne,
		"a/b/two":   fileTwo,
		"a/c/three": fileThree,
	} {
		_, err = obj.PutObject(context.Background(), bucket, objName, mustGetPutObjReader(t, strings.NewReader(value), int64(len(value)), "", ""), opts)
		if err != nil {
			t.Fatal(err)
		}
	}

	test := func(token string) (int, []byte) {
		rec := httptest.NewRecorder()
		path := "/obstor/zip" + "?token="
		if token != "" {
			path = path + token
		}
		args := DownloadZipArgs{
			Objects:    []string{"one", "b/", "c/"},
			Prefix:     "a/",
			BucketName: bucket,
		}

		var argsData []byte
		argsData, err = json.Marshal(args)
		if err != nil {
			return 0, nil
		}
		var req *http.Request
		req, err = http.NewRequest(http.MethodPost, path, bytes.NewBuffer(argsData))

		if err != nil {
			t.Fatalf("Cannot create upload request, %v", err)
		}

		req.Header.Set("User-Agent", "Mozilla")

		apiRouter.ServeHTTP(rec, req)
		return rec.Code, rec.Body.Bytes()
	}
	code, _ := test("")
	if code != 403 {
		t.Fatal("Expected to receive authentication error")
	}
	code, data := test(authorization)
	if code != 200 {
		t.Fatal("web.DownloadsZip() failed")
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	h := md5.New()
	for _, file := range reader.File {
		fileReader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(h, fileReader)
	}
	// Verify the md5 of the response.
	if hex.EncodeToString(h.Sum(nil)) != "ac7196449b14bea42775d29e8bb29f50" {
		t.Fatal("Incorrect zip contents")
	}
}

// Wrapper for calling PresignedGet handler
func TestWebHandlerPresignedGetHandler(t *testing.T) {
	ExecObjectLayerTest(t, testWebPresignedGetHandler)
}

func testWebPresignedGetHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	// Register the API end points with Erasure/FS object layer.
	apiRouter := initTestWebRPCEndPoint(obj)
	credentials := globalActiveCred

	authorization, err := getWebRPCToken(apiRouter, credentials.AccessKey, credentials.SecretKey)
	if err != nil {
		t.Fatal("Cannot authenticate")
	}

	rec := httptest.NewRecorder()

	bucketName := getRandomBucketName()
	objectName := "object"
	objectSize := 1 * humanize.KiByte

	// Create bucket.
	err = obj.MakeBucketWithLocation(context.Background(), bucketName, BucketOptions{})
	if err != nil {
		// failed to create newbucket, abort.
		t.Fatalf("%s : %s", instanceType, err)
	}

	data := bytes.Repeat([]byte("a"), objectSize)
	metadata := map[string]string{"etag": "c9a34cfc85d982698c6ac89f76071abd"}
	_, err = obj.PutObject(context.Background(), bucketName, objectName, mustGetPutObjReader(t, bytes.NewReader(data), int64(len(data)), metadata["etag"], ""), ObjectOptions{UserDefined: metadata})
	if err != nil {
		t.Fatalf("Was not able to upload an object, %v", err)
	}

	presignGetReq := PresignedGetArgs{
		HostName:   "",
		BucketName: bucketName,
		ObjectName: objectName,
		Expiry:     1000,
	}
	presignGetRep := &PresignedGetRep{}
	req, err := newTestWebRPCRequest("web.PresignedGet", authorization, presignGetReq)
	if err != nil {
		t.Fatalf("Failed to create HTTP request: <ERROR> %v", err)
	}
	apiRouter.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected the response status to be 200, but instead found `%d`", rec.Code)
	}
	err = getTestWebRPCResponse(rec, &presignGetRep)
	if err != nil {
		t.Fatalf("Failed, %v", err)
	}

	// Register the API end points with Erasure/FS object layer.
	apiRouter = initTestAPIEndPoints(obj, []string{"GetObject"})

	// Initialize a new api recorder.
	arec := httptest.NewRecorder()

	req, err = newTestRequest(http.MethodGet, presignGetRep.URL, 0, nil)
	req.Header.Del("x-amz-content-sha256")
	if err != nil {
		t.Fatal("Failed to initialized a new request", err)
	}
	apiRouter.ServeHTTP(arec, req)
	if arec.Code != http.StatusOK {
		t.Fatalf("Expected the response status to be 200, but instead found `%d`", arec.Code)
	}
	savedData, err := io.ReadAll(arec.Body)
	if err != nil {
		t.Fatal("Reading body failed", err)
	}
	if !bytes.Equal(data, savedData) {
		t.Fatal("Read data is not equal was what was expected")
	}

	// Register the API end points with Erasure/FS object layer.
	apiRouter = initTestWebRPCEndPoint(obj)

	presignGetReq = PresignedGetArgs{
		HostName:   "",
		BucketName: "",
		ObjectName: "",
	}
	presignGetRep = &PresignedGetRep{}
	req, err = newTestWebRPCRequest("web.PresignedGet", authorization, presignGetReq)
	if err != nil {
		t.Fatalf("Failed to create HTTP request: <ERROR> %v", err)
	}
	apiRouter.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected the response status to be 200, but instead found `%d`", rec.Code)
	}
	err = getTestWebRPCResponse(rec, &presignGetRep)
	if err == nil {
		t.Fatalf("Failed, %v", err)
	}
	if err.Error() != "Bucket and Object are mandatory arguments." {
		t.Fatalf("Unexpected, expected `Bucket and Object are mandatory arguments`, got %s", err)
	}
}

// TestWebCheckAuthorization - Test Authorization for all web handlers
func TestWebCheckAuthorization(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Prepare Erasure backend
	obj, fsDirs, err := prepareErasure16(ctx)
	if err != nil {
		t.Fatalf("Initialization of object layer failed for Erasure setup: %s", err)
	}
	// Executing the object layer tests for Erasure.
	defer removeRoots(fsDirs)

	// Register the API end points with Erasure/FS object layer.
	apiRouter := initTestWebRPCEndPoint(obj)

	// initialize the server and obtain the credentials and root.
	// credentials are necessary to sign the HTTP request.
	err = newTestConfig(globalObstorDefaultRegion, obj)
	if err != nil {
		t.Fatal("Init Test config failed", err)
	}

	rec := httptest.NewRecorder()

	// Check if web rpc calls return unauthorized request with an incorrect token
	webRPCs := []string{
		"ServerInfo", "StorageInfo", "MakeBucket",
		"ListBuckets", "ListObjects", "RemoveObject",
		"SetAuth", "GetBucketPolicy", "SetBucketPolicy",
		"ListAllBucketPolicies", "PresignedGet",
		"GetBucketToggles", "SetBucketToggles",
		"BucketFeatures", "GetBucketConfig",
		"SetBucketVersioning", "SetBucketQuota",
		"SetBucketEncryption",
		"SetBucketTags",
	}
	for _, rpcCall := range webRPCs {
		reply := &WebGenericRep{}
		req, nerr := newTestWebRPCRequest("web."+rpcCall, "Bearer fooauthorization", &WebGenericArgs{})
		if nerr != nil {
			t.Fatalf("Test %s: Failed to create HTTP request: <ERROR> %v", rpcCall, nerr)
		}
		apiRouter.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("Test %s: Expected the response status to be 200, but instead found `%d`", rpcCall, rec.Code)
		}
		err = getTestWebRPCResponse(rec, &reply)
		if err == nil {
			t.Fatalf("Test %s: Should fail", rpcCall)
		} else {
			if !strings.Contains(err.Error(), errWebTokenInvalid.Error()) {
				t.Fatalf("Test %s: should fail with Unauthorized request. Found error: %v", rpcCall, err)
			}
		}
	}

	rec = httptest.NewRecorder()
	// Test authorization of web.Download
	req, err := http.NewRequest(http.MethodGet, "/obstor/download/bucket/object?token=wrongauth", nil)
	if err != nil {
		t.Fatalf("Cannot create upload request, %v", err)
	}
	req.Header.Set("User-Agent", "Mozilla")
	apiRouter.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Expected the response status to be 403, but instead found `%d`", rec.Code)
	}
	resp := rec.Body.String()
	if !strings.EqualFold(resp, errAuthentication.Error()) {
		t.Fatalf("Unexpected error message, expected: %s, found: `%s`", errAuthentication, resp)
	}

	rec = httptest.NewRecorder()
	// Test authorization of web.Upload
	content := []byte("temporary file's content")
	req, err = http.NewRequest(http.MethodPut, "/obstor/upload/bucket/object", nil)
	req.Header.Set("Authorization", "Bearer foo-authorization")
	req.Header.Set("User-Agent", "Mozilla")
	req.Header.Set("Content-Length", strconv.Itoa(len(content)))
	req.Header.Set("x-amz-date", "20160814T114029Z")
	req.Header.Set("Accept", "*/*")
	req.Body = io.NopCloser(bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Cannot create upload request, %v", err)
	}
	apiRouter.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("Expected the response status to be 401, but instead found `%d`", rec.Code)
	}
	resp = rec.Body.String()
	if !strings.EqualFold(resp, errWebTokenInvalid.Error()) {
		t.Fatalf("Unexpected error message, expected: `%s`, found: `%s`", errWebTokenInvalid, resp)
	}
}

// TestWebObjectLayerFaultyDisks - Test Web RPC responses with faulty disks
func TestWebObjectLayerFaultyDisks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Prepare Erasure backend
	obj, fsDirs, err := prepareErasure16(ctx)
	if err != nil {
		t.Fatalf("Initialization of object layer failed for Erasure setup: %s", err)
	}
	// Executing the object layer tests for Erasure.
	defer removeRoots(fsDirs)

	// initialize the server and obtain the credentials and root.
	// credentials are necessary to sign the HTTP request.
	err = newTestConfig(globalObstorDefaultRegion, obj)
	if err != nil {
		t.Fatal("Init Test config failed", err)
	}

	bucketName := "mybucket"
	err = obj.MakeBucketWithLocation(context.Background(), bucketName, BucketOptions{})
	if err != nil {
		t.Fatal("Cannot make bucket:", err)
	}

	// Set faulty disks to Erasure backend
	z := obj.(*erasureServerPools)
	xl := z.serverPools[0].sets[0]
	erasureDisks := xl.getDisks()
	z.serverPools[0].erasureDisksMu.Lock()
	xl.getDisks = func() []StorageAPI {
		for i, d := range erasureDisks {
			erasureDisks[i] = newNaughtyDisk(d, nil, errFaultyDisk)
		}
		return erasureDisks
	}
	z.serverPools[0].erasureDisksMu.Unlock()

	// Initialize web rpc endpoint.
	apiRouter := initTestWebRPCEndPoint(obj)

	rec := httptest.NewRecorder()

	credentials := globalActiveCred
	authorization, err := getWebRPCToken(apiRouter, credentials.AccessKey, credentials.SecretKey)
	if err != nil {
		t.Fatal("Cannot authenticate", err)
	}

	// Check if web rpc calls return errors with faulty disks.  ServerInfo, GenerateAuth, SetAuth, GetAuth are not concerned
	// RemoveObject is also not concerned since it always returns success.
	webRPCs := []struct {
		webRPCName string
		ReqArgs    interface{}
		RepArgs    interface{}
	}{
		{"MakeBucket", MakeBucketArgs{BucketName: bucketName}, WebGenericRep{}},
		{"ListBuckets", WebGenericArgs{}, ListBucketsRep{}},
		{"ListObjects", ListObjectsArgs{BucketName: bucketName, Prefix: ""}, ListObjectsRep{}},
		{"GetBucketPolicy", GetBucketPolicyArgs{BucketName: bucketName, Prefix: ""}, GetBucketPolicyRep{}},
		{"SetBucketPolicy", SetBucketPolicyWebArgs{BucketName: bucketName, Prefix: "", Policy: "none"}, WebGenericRep{}},
	}

	for _, rpcCall := range webRPCs {
		args := &rpcCall.ReqArgs
		reply := &rpcCall.RepArgs
		req, nerr := newTestWebRPCRequest("web."+rpcCall.webRPCName, authorization, args)
		if nerr != nil {
			t.Fatalf("Test %s: Failed to create HTTP request: <ERROR> %v", rpcCall, nerr)
		}
		apiRouter.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("Test %s: Expected the response status to be 200, but instead found `%d`", rpcCall, rec.Code)
		}
		err = getTestWebRPCResponse(rec, &reply)
		if err == nil {
			t.Errorf("Test %s: Should fail", rpcCall)
		}
	}

	// Test web.StorageInfo
	storageInfoRequest := &WebGenericArgs{}
	storageInfoReply := &StorageInfoRep{}
	req, err := newTestWebRPCRequest("web.StorageInfo", authorization, storageInfoRequest)
	if err != nil {
		t.Fatalf("Failed to create HTTP request: <ERROR> %v", err)
	}
	apiRouter.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected the response status to be 200, but instead found `%d`", rec.Code)
	}
	err = getTestWebRPCResponse(rec, &storageInfoReply)
	if err != nil {
		t.Fatalf("Failed %v", err)
	}

	// Test authorization of web.Download
	req, err = http.NewRequest(http.MethodGet, "/obstor/download/bucket/object?token="+authorization, nil)
	if err != nil {
		t.Fatalf("Cannot create upload request, %v", err)
	}
	apiRouter.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected the response status to be 200, but instead found `%d`", rec.Code)
	}

	// Test authorization of web.Upload
	content := []byte("temporary file's content")
	req, err = http.NewRequest(http.MethodPut, "/obstor/upload/bucket/object", nil)
	req.Header.Set("Authorization", "Bearer "+authorization)
	req.Header.Set("Content-Length", strconv.Itoa(len(content)))
	req.Header.Set("x-amz-date", "20160814T114029Z")
	req.Header.Set("Accept", "*/*")
	req.Body = io.NopCloser(bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Cannot create upload request, %v", err)
	}
	apiRouter.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected the response status to be 200, but instead found `%d`", rec.Code)
	}
}

func TestStorageParityRatio(t *testing.T) {
	disks := func(pool int, sizes ...uint64) []madmin.Disk {
		out := make([]madmin.Disk, 0, len(sizes))
		for _, size := range sizes {
			out = append(out, madmin.Disk{PoolIndex: pool, TotalSpace: size})
		}
		return out
	}
	for _, tc := range []struct {
		name string
		si   StorageInfo
		want float64
	}{
		{"fs", StorageInfo{Disks: disks(0, 100), Backend: madmin.BackendInfo{Type: madmin.FS}}, 1},
		{"backend", StorageInfo{Backend: madmin.BackendInfo{Type: madmin.Gateway}}, 1},
		{"erasure without storage class", StorageInfo{Disks: disks(0, 100), Backend: madmin.BackendInfo{Type: madmin.Erasure}}, 1},
		{"one pool 2+2", StorageInfo{
			Disks:   disks(0, 100, 100, 100, 100),
			Backend: madmin.BackendInfo{Type: madmin.Erasure, StandardSCData: []int{2}, StandardSCParity: 2},
		}, 2},
		{"two pools weighted by capacity", StorageInfo{
			Disks:   append(disks(0, 50, 50), disks(1, 100, 100, 100)...),
			Backend: madmin.BackendInfo{Type: madmin.Erasure, StandardSCData: []int{4, 2}, StandardSCParity: 2},
		}, 1.875},
		{"zero capacity", StorageInfo{
			Disks:   disks(0, 0, 0, 0, 0),
			Backend: madmin.BackendInfo{Type: madmin.Erasure, StandardSCData: []int{2}, StandardSCParity: 2},
		}, 1},
		{"unassigned pool index", StorageInfo{
			Disks:   disks(-1, 100, 100),
			Backend: madmin.BackendInfo{Type: madmin.Erasure, StandardSCData: []int{2}, StandardSCParity: 2},
		}, 1},
		{"rrs ignored", StorageInfo{
			Disks: disks(0, 100, 100, 100, 100),
			Backend: madmin.BackendInfo{Type: madmin.Erasure, StandardSCData: []int{2}, StandardSCParity: 2,
				RRSCData: []int{3}, RRSCParity: 1},
		}, 2},
	} {
		if got := storageParityRatio(tc.si); got < tc.want-1e-9 || got > tc.want+1e-9 {
			t.Errorf("%s: storageParityRatio = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestWebAccountType(t *testing.T) {
	ExecObjectLayerTest(t, testWebAccountType)
}

// Root, IAM, service and STS have distinct account types
func testWebAccountType(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	globalIAMSys.InitStore(obj)
	user := makeTestIAMUser(t, "acctuser", "s3:ListBucket")
	svc, err := globalIAMSys.NewServiceAccount(context.Background(), user.AccessKey, nil, newServiceAccountOpts{})
	if err != nil {
		t.Fatalf("%s: new service account: %v", instanceType, err)
	}
	temp := auth.Credentials{
		AccessKey:    "accttemp",
		SecretKey:    "accttemp-secret",
		SessionToken: "accttemp-session",
		Expiration:   UTCNow().Add(time.Hour),
		ParentUser:   user.AccessKey,
	}
	if err = globalIAMSys.SetTempUser(temp.AccessKey, temp, ""); err != nil {
		t.Fatalf("%s: set temp user: %v", instanceType, err)
	}
	for _, tc := range []struct {
		owner     bool
		accessKey string
		want      string
	}{
		{true, globalActiveCred.AccessKey, "root"},
		{false, user.AccessKey, "iam"},
		{false, svc.AccessKey, "service"},
		{false, temp.AccessKey, "temporary"},
		{false, "no-such-user", "iam"},
	} {
		if got := webAccountType(tc.owner, tc.accessKey); got != tc.want {
			t.Errorf("%s: webAccountType(%v, %q) = %q, want %q", instanceType, tc.owner, tc.accessKey, got, tc.want)
		}
	}
}

// Create IAM user with policy doc
func makeDocIAMUser(t TestErrHandler, name, doc string) auth.Credentials {
	t.Helper()
	p, err := iampolicy.ParseConfig(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("parse policy %s: %v", name, err)
	}
	if err = globalIAMSys.SetPolicy(name, *p); err != nil {
		t.Fatalf("set policy %s: %v", name, err)
	}
	cred := auth.Credentials{AccessKey: name, SecretKey: name + "-secret"}
	if err = globalIAMSys.CreateUser(cred.AccessKey, madmin.UserInfo{SecretKey: cred.SecretKey, Status: madmin.AccountEnabled}); err != nil {
		t.Fatalf("create user %s: %v", name, err)
	}
	if err = globalIAMSys.PolicyDBSet(cred.AccessKey, name, false); err != nil {
		t.Fatalf("attach policy %s: %v", name, err)
	}
	return cred
}

// One bucket's listing permission grants no Fleet or Settings access
func makeScopedIAMUser(t TestErrHandler, name, bucket string) auth.Credentials {
	t.Helper()
	return makeDocIAMUser(t, name, fmt.Sprintf(`{"Version":"2012-10-17","Statement":[
{"Effect":"Allow","Action":["s3:ListBucket"],"Resource":["arn:aws:s3:::%s"]}]}`, bucket))
}

func TestWebHandlerServerInfoTiers(t *testing.T) {
	ExecObjectLayerTest(t, testServerInfoTiersWebHandler)
}

// Public server fields for all, SFTP for Settings tier, canSeeFleet for Fleet tier
func testServerInfoTiersWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	savedSFTP, savedStore, savedRegion := GlobalSFTPConfig, globalFleetStore, globalServerRegion
	defer func() { GlobalSFTPConfig, globalFleetStore, globalServerRegion = savedSFTP, savedStore, savedRegion }()
	GlobalSFTPConfig.Enabled, GlobalSFTPConfig.Address = true, "127.0.0.1:2022"
	globalFleetStore, globalServerRegion = nil, ""
	globalIAMSys.InitStore(obj)
	router := initTestWebRPCEndPoint(obj)
	allowed := makeTestBucket(t, obj, BucketOptions{})
	makeTestBucket(t, obj, BucketOptions{})
	fleet := makeTestIAMUser(t, "tierfleet", "s3:ListBucket")
	settings := makeDocIAMUser(t, "tiersettings", `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["admin:ServerInfo"]}]}`)
	scoped := makeScopedIAMUser(t, "tierscoped", allowed)
	mode := "fs"
	if instanceType == ErasureTestStr {
		mode = "erasure"
	}
	call := func(who string, cred auth.Credentials) ServerInfoRep {
		var rep ServerInfoRep
		if err := userWebRPC(t, router, cred.AccessKey, cred.SecretKey, "ServerInfo", &WebGenericArgs{}, &rep); err != nil {
			t.Fatalf("%s: ServerInfo as %s: %v", instanceType, who, err)
		}
		if rep.ObstorVersion != Version || rep.UIVersion != Version {
			t.Fatalf("%s: %s versions = %q/%q, want %q", instanceType, who, rep.ObstorVersion, rep.UIVersion, Version)
		}
		rep.ObstorVersion, rep.ObstorPlatform, rep.ObstorRuntime, rep.UIVersion = "", "", "", ""
		rep.Commit, rep.StartedAt = "", ""
		rep.KMS, rep.AutoEncryption, rep.TLS, rep.TrustedProxies = false, false, false, false
		return rep
	}
	public := func(fleet bool, sftpAddress, account string) ServerInfoRep {
		return ServerInfoRep{
			ReleaseTag:     ReleaseTag,
			CanSeeFleet:    fleet,
			CanSeeSettings: sftpAddress != "",
			Region:         "us-east-1",
			Mode:           mode,
			SFTPEnabled:    true,
			SFTPAddress:    sftpAddress,
			Account:        account,
			Usage:          true,
		}
	}
	for _, tc := range []struct {
		who  string
		cred auth.Credentials
		want ServerInfoRep
	}{
		{"root", globalActiveCred, public(true, "127.0.0.1:2022", "root")},
		{"bucket lister", fleet, public(false, "", "iam")},
		{"settings user", settings, public(true, "127.0.0.1:2022", "iam")},
		{"scoped user", scoped, public(false, "", "iam")},
	} {
		if got := call(tc.who, tc.cred); got != tc.want {
			t.Errorf("%s: %s ServerInfo =\n%+v\nwant\n%+v", instanceType, tc.who, got, tc.want)
		}
	}

	// SFTP off hides address, fleet store enables metrics, backend disables usage
	GlobalSFTPConfig.Enabled = false
	globalFleetStore = newInMemFleetStore(45*time.Second, "")
	globalIsBackend = true
	want := ServerInfoRep{ReleaseTag: ReleaseTag, CanSeeFleet: true, CanSeeSettings: true, Region: "us-east-1", Mode: "backend", Account: "root", Metrics: true}
	if got := call("root", globalActiveCred); got != want {
		t.Errorf("%s: root ServerInfo (backend, SFTP off, fleet store) =\n%+v\nwant\n%+v", instanceType, got, want)
	}
}

func TestWebHandlerStorageInfoTiers(t *testing.T) {
	ExecObjectLayerTest(t, testStorageInfoTiersWebHandler)
}

// Fleet sees totals and parity, usage stays scoped, scan time and region for all
func testStorageInfoTiersWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	savedRegion := globalServerRegion
	defer func() { globalServerRegion = savedRegion }()
	globalServerRegion = ""
	globalIAMSys.InitStore(obj)
	router := initTestWebRPCEndPoint(obj)
	// Root sees capacity with no buckets
	var empty StorageInfoRep
	if err := userWebRPC(t, router, globalActiveCred.AccessKey, globalActiveCred.SecretKey, "StorageInfo", &WebGenericArgs{}, &empty); err != nil {
		t.Fatalf("%s: StorageInfo as root with no buckets: %v", instanceType, err)
	}
	if empty.Total == 0 || empty.Free == 0 || empty.ParityRatio < 1 {
		t.Fatalf("%s: root with no buckets = %+v, want total and free > 0, parityRatio >= 1", instanceType, empty)
	}
	allowed := makeTestBucket(t, obj, BucketOptions{})
	other := makeTestBucket(t, obj, BucketOptions{})
	fleet := makeDocIAMUser(t, "storefleet", `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["admin:ServerInfo"]},{"Effect":"Allow","Action":["s3:ListBucket"],"Resource":["arn:aws:s3:::*"]}]}`)
	scoped := makeScopedIAMUser(t, "storescoped", allowed)
	call := func(who string, cred auth.Credentials) StorageInfoRep {
		var rep StorageInfoRep
		if err := userWebRPC(t, router, cred.AccessKey, cred.SecretKey, "StorageInfo", &WebGenericArgs{}, &rep); err != nil {
			t.Fatalf("%s: StorageInfo as %s: %v", instanceType, who, err)
		}
		return rep
	}

	for _, who := range []struct {
		name string
		cred auth.Credentials
	}{{"root", globalActiveCred}, {"fleet user", fleet}} {
		rep := call(who.name, who.cred)
		if rep.Total == 0 || rep.Free == 0 || rep.ParityRatio < 1 || rep.Region != "us-east-1" || rep.UsageAsOf != "" {
			t.Fatalf("%s: %s on a fresh layer = %+v, want total and free > 0, parityRatio >= 1, region us-east-1, no usageAsOf", instanceType, who.name, rep)
		}
		if instanceType == FSTestStr && rep.ParityRatio != 1 {
			t.Fatalf("%s: %s parityRatio = %v, want 1", instanceType, who.name, rep.ParityRatio)
		}
	}
	rep := call("scoped user", scoped)
	if rep.Total != 0 || rep.Free != 0 || rep.ParityRatio != 0 || rep.DisksOnline != 0 || rep.DisksOffline != 0 || rep.Region != "us-east-1" {
		t.Fatalf("%s: scoped user = %+v, want no totals, parityRatio 0, region us-east-1", instanceType, rep)
	}

	// Scan time reaches every caller in UTC
	usage := make(chan madmin.DataUsageInfo, 1)
	usage <- madmin.DataUsageInfo{
		LastUpdate: time.Date(2026, 9, 25, 8, 0, 0, 0, time.FixedZone("UTC+2", 2*60*60)),
		BucketsUsage: map[string]madmin.BucketUsageInfo{
			allowed: {Size: 100, ObjectsCount: 1},
			other:   {Size: 900, ObjectsCount: 9},
		},
	}
	close(usage)
	storeDataUsageInBackend(context.Background(), obj, usage)
	if rep := call("root", globalActiveCred); rep.UsageAsOf != "2026-09-25T06:00:00Z" || rep.Used != 1000 {
		t.Fatalf("%s: root after usage scan = %+v, want usageAsOf 2026-09-25T06:00:00Z, used 1000", instanceType, rep)
	}
	if rep := call("scoped user", scoped); rep.UsageAsOf != "2026-09-25T06:00:00Z" || rep.Used != 100 || rep.ObjectsCount != 1 {
		t.Fatalf("%s: scoped user after usage scan = %+v, want usageAsOf 2026-09-25T06:00:00Z, used 100, objects 1", instanceType, rep)
	}
}

func TestWebHandlerSetAuthAccountTypes(t *testing.T) {
	ExecObjectLayerTest(t, testSetAuthAccountTypesWebHandler)
}

// SetAuth refuses temporary, service and LDAP credentials without writes
func testSetAuthAccountTypesWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	savedIAM := globalIAMSys
	defer func() { globalIAMSys = savedIAM }()
	globalIAMSys = NewIAMSys()
	globalIAMSys.InitStore(obj)
	router := initTestWebRPCEndPoint(obj)

	setAuth := func(token, current, next string) *httptest.ResponseRecorder {
		t.Helper()
		req, err := newTestWebRPCRequest("web.SetAuth", token, SetAuthArgs{CurrentSecretKey: current, NewSecretKey: next})
		if err != nil {
			t.Fatalf("%s: SetAuth request: %v", instanceType, err)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	// Sign token as webTokenCallback expects, include session claims
	mint := func(accessKey, signingKey string, extra map[string]interface{}) string {
		t.Helper()
		claims := xjwt.NewMapClaims()
		claims.SetExpiry(UTCNow().Add(defaultJWTExpiry))
		claims.SetAccessKey(accessKey)
		for k, v := range extra {
			claims.MapClaims[k] = v
		}
		token, err := jwtgo.NewWithClaims(jwtgo.SigningMethodHS512, claims).SignedString([]byte(signingKey))
		if err != nil {
			t.Fatalf("%s: sign token for %s: %v", instanceType, accessKey, err)
		}
		return token
	}
	identity := func(accessKey string) ([]byte, error) {
		return readConfig(ctx, obj, getUserIdentityPath(accessKey, regularUser))
	}

	parent := makeTestIAMUser(t, "parentuser", "s3:ListBucket")
	parentToken, err := getWebRPCToken(router, parent.AccessKey, parent.SecretKey)
	if err != nil {
		t.Fatalf("%s: login as %s: %v", instanceType, parent.AccessKey, err)
	}
	const svcKey, svcSecret = "svcacct1", "svcacct1-secret"
	if _, err = globalIAMSys.NewServiceAccount(ctx, parent.AccessKey, nil, newServiceAccountOpts{accessKey: svcKey, secretKey: svcSecret}); err != nil {
		t.Fatalf("%s: create service account: %v", instanceType, err)
	}
	sts := auth.Credentials{
		AccessKey:    "stsuser01",
		SecretKey:    "stsuser01-secret",
		SessionToken: "tok",
		Expiration:   UTCNow().Add(time.Hour),
		ParentUser:   parent.AccessKey,
		Status:       auth.AccountOn,
	}
	if err = globalIAMSys.SetTempUser(sts.AccessKey, sts, parent.AccessKey); err != nil {
		t.Fatalf("%s: create temporary user: %v", instanceType, err)
	}

	refused := []struct {
		name, accessKey, secret, token string
	}{
		{"temporary", sts.AccessKey, sts.SecretKey, mint(sts.AccessKey, globalActiveCred.SecretKey,
			map[string]interface{}{iamPolicyClaimNameOpenID(): parent.AccessKey})},
		{"service account", svcKey, svcSecret, mint(svcKey, svcSecret,
			map[string]interface{}{parentClaim: parent.AccessKey, iamPolicyClaimNameSA(): "inherited-policy"})},
	}
	for _, c := range refused {
		if code, message := webRPCError(t, setAuth(c.token, c.secret, "newsecret123")); code != "MethodNotAllowed" {
			t.Errorf("%s: %s SetAuth = %q %q, want MethodNotAllowed", instanceType, c.name, code, message)
		}
		if _, err := identity(c.accessKey); err != errConfigNotFound {
			t.Errorf("%s: %s SetAuth wrote %s: read err %v, want %v", instanceType, c.name,
				getUserIdentityPath(c.accessKey, regularUser), err, errConfigNotFound)
		}
	}

	before, err := identity(parent.AccessKey)
	if err != nil {
		t.Fatalf("%s: read %s identity: %v", instanceType, parent.AccessKey, err)
	}
	globalIAMSys.usersSysType = LDAPUsersSysType
	code, message := webRPCError(t, setAuth(parentToken, parent.SecretKey, "newsecret123"))
	globalIAMSys.usersSysType = ObstorUsersSysType
	if code != "MethodNotAllowed" {
		t.Errorf("%s: LDAP SetAuth = %q %q, want MethodNotAllowed", instanceType, code, message)
	}
	if after, err := identity(parent.AccessKey); err != nil || !bytes.Equal(before, after) {
		t.Errorf("%s: LDAP SetAuth changed %s (read err %v)", instanceType, getUserIdentityPath(parent.AccessKey, regularUser), err)
	}

	if code, message := webRPCError(t, setAuth(parentToken, "wrong-secret", "newsecret123")); code != "InvalidCurrentSecret" {
		t.Errorf("%s: wrong current secret = %q %q, want InvalidCurrentSecret", instanceType, code, message)
	}
	rec := setAuth(parentToken, parent.SecretKey, "newsecret123")
	if code, message := webRPCError(t, rec); code != "" || message != "" {
		t.Fatalf("%s: IAM user SetAuth = %q %q, want no error", instanceType, code, message)
	}
	var reply SetAuthReply
	if err = getTestWebRPCResponse(rec, &reply); err != nil || reply.Token == "" {
		t.Fatalf("%s: IAM user SetAuth reply token %q, err %v, want a token", instanceType, reply.Token, err)
	}
}
