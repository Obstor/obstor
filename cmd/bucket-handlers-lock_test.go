package cmd

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/obstor/obstor/pkg/auth"
	iampolicy "github.com/obstor/obstor/pkg/iam/policy"
	"github.com/obstor/obstor/pkg/madmin"
)

// Create IAM user with given bucket actions
func makeTestIAMUser(t TestErrHandler, name string, actions ...string) auth.Credentials {
	t.Helper()
	doc := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["` +
		strings.Join(actions, `","`) + `"],"Resource":["arn:aws:s3:::*"]}]}`
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

func TestWebHandlerMakeBucketLockPermissions(t *testing.T) {
	ExecObjectLayerTest(t, testMakeBucketLockPermissionsWebHandler)
}

// Non-root locked create needs lock and versioning permissions
func testMakeBucketLockPermissionsWebHandler(obj ObjectLayer, instanceType string, t TestErrHandler) {
	defer pinBucketGlobals(instanceType)()
	globalIAMSys.InitStore(obj)
	router := initTestWebRPCEndPoint(obj)
	create := func(cred auth.Credentials, lock bool) error {
		token, err := getWebRPCToken(router, cred.AccessKey, cred.SecretKey)
		if err != nil {
			t.Fatalf("%s: login as %s: %v", instanceType, cred.AccessKey, err)
		}
		req, err := newTestWebRPCRequest("web.MakeBucket", token, &MakeBucketArgs{BucketName: getRandomBucketName(), LockEnabled: lock})
		if err != nil {
			t.Fatalf("%s: MakeBucket request: %v", instanceType, err)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return getTestWebRPCResponse(rec, &WebGenericRep{})
	}
	createOnly := makeTestIAMUser(t, "rpclockcreateonly", "s3:CreateBucket")
	noVersioning := makeTestIAMUser(t, "rpclocknoversioning", "s3:CreateBucket", "s3:PutBucketObjectLockConfiguration")
	noLock := makeTestIAMUser(t, "rpclocknolock", "s3:CreateBucket", "s3:PutBucketVersioning")
	full := makeTestIAMUser(t, "rpclockfull", "s3:CreateBucket", "s3:PutBucketObjectLockConfiguration", "s3:PutBucketVersioning")

	for _, cred := range []auth.Credentials{createOnly, noVersioning, noLock} {
		if err := create(cred, true); err == nil || !strings.Contains(err.Error(), errAccessDenied.Error()) {
			t.Fatalf("%s: locked MakeBucket by %s = %v, want access denied", instanceType, cred.AccessKey, err)
		}
	}
	if err := create(createOnly, false); err != nil {
		t.Fatalf("%s: plain MakeBucket by %s: %v", instanceType, createOnly.AccessKey, err)
	}
	// FS refuses locked bucket after checks
	err := create(full, true)
	if instanceType == FSTestStr {
		if err == nil || !strings.Contains(err.Error(), "not implemented") {
			t.Fatalf("FS: locked MakeBucket by %s = %v, want not implemented", full.AccessKey, err)
		}
	} else if err != nil {
		t.Fatalf("locked MakeBucket by %s: %v", full.AccessKey, err)
	}
}

func TestPutBucketHandlerLockPermissions(t *testing.T) {
	ExecObjectLayerAPITest(t, testPutBucketHandlerLockPermissions, []string{"PutBucket"})
}

// S3 locked create needs same lock and versioning permissions as RPC
func testPutBucketHandlerLockPermissions(obj ObjectLayer, instanceType, bucketName string, apiRouter http.Handler,
	credentials auth.Credentials, t *testing.T) {
	defer pinBucketGlobals(instanceType)()
	globalIAMSys.InitStore(obj)
	createOnly := makeTestIAMUser(t, "lockcreateonly", "s3:CreateBucket")
	noVersioning := makeTestIAMUser(t, "locknoversioning", "s3:CreateBucket", "s3:PutBucketObjectLockConfiguration")
	noLock := makeTestIAMUser(t, "locknolock", "s3:CreateBucket", "s3:PutBucketVersioning")
	full := makeTestIAMUser(t, "lockfull", "s3:CreateBucket", "s3:PutBucketObjectLockConfiguration", "s3:PutBucketVersioning")

	create := func(cred auth.Credentials, lock bool) *httptest.ResponseRecorder {
		headers := map[string]string{}
		if lock {
			headers["x-amz-bucket-object-lock-enabled"] = "true"
		}
		target := makeTestTargetURL("", getRandomBucketName(), "", url.Values{})
		req, err := newTestSignedRequestV4(http.MethodPut, target, 0, nil, cred.AccessKey, cred.SecretKey, headers)
		if err != nil {
			t.Fatalf("%s: PUT bucket request: %v", instanceType, err)
		}
		rec := httptest.NewRecorder()
		apiRouter.ServeHTTP(rec, req)
		return rec
	}

	for _, cred := range []auth.Credentials{createOnly, noVersioning, noLock} {
		if rec := create(cred, true); rec.Code != http.StatusForbidden {
			t.Fatalf("%s: locked create by %s = %d, want 403", instanceType, cred.AccessKey, rec.Code)
		}
	}
	if rec := create(createOnly, false); rec.Code != http.StatusOK {
		t.Fatalf("%s: plain create by %s = %d %s, want 200", instanceType, createOnly.AccessKey, rec.Code, rec.Body.String())
	}
	// FS refuses locked bucket after checks
	want := http.StatusOK
	if instanceType == FSTestStr {
		want = http.StatusNotImplemented
	}
	if rec := create(full, true); rec.Code != want {
		t.Fatalf("%s: locked create by %s = %d %s, want %d", instanceType, full.AccessKey, rec.Code, rec.Body.String(), want)
	}
	if rec := create(credentials, true); rec.Code != want {
		t.Fatalf("%s: locked create by root = %d %s, want %d", instanceType, rec.Code, rec.Body.String(), want)
	}
}
