package logger

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuditLogActivityHook(t *testing.T) {
	savedHook, savedTargets := ActivityHook, AuditTargets
	defer func() { ActivityHook, AuditTargets = savedHook, savedTargets }()
	AuditTargets = nil

	calls, gotStatus := 0, -1
	var gotInfo *ReqInfo
	ActivityHook = func(_ context.Context, reqInfo *ReqInfo, statusCode int) {
		calls++
		gotInfo, gotStatus = reqInfo, statusCode
	}

	info := &ReqInfo{API: "PutObject", BucketName: "alpha", ObjectName: "a.txt", AccessKey: "obstoradmin"}
	ctx := SetReqInfo(context.Background(), info)
	r := httptest.NewRequest(http.MethodPut, "/alpha/a.txt", nil)
	w := NewResponseWriter(httptest.NewRecorder())
	w.WriteHeader(http.StatusCreated)

	AuditLog(ctx, w, r, nil)
	if calls != 1 || gotInfo != info || gotStatus != http.StatusCreated {
		t.Fatalf("wrapped writer: calls %d, info %p (want %p), status %d; want 1 call with status 201", calls, gotInfo, info, gotStatus)
	}

	AuditLog(ctx, nil, nil, nil)
	AuditLog(ctx, w, nil, nil)
	AuditLog(ctx, nil, r, nil)
	if calls != 1 {
		t.Fatalf("nil writer or request: calls %d, want the hook skipped", calls)
	}

	AuditLog(ctx, httptest.NewRecorder(), r, nil)
	if calls != 2 || gotStatus != 0 {
		t.Fatalf("plain writer: calls %d, status %d; want 2 calls and status 0", calls, gotStatus)
	}

	ActivityHook = nil
	AuditLog(ctx, w, r, nil)
	if calls != 2 {
		t.Fatalf("nil hook: calls %d, want 2", calls)
	}
}
