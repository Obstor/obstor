/*
 * PGG Obstor, (C) 2021-2026 PGG, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

package cmd

import (
	"fmt"
	"testing"
)

// TestLDAPSTSThrottle verifies the per-IP/per-username limiter
func TestLDAPSTSThrottle(t *testing.T) {
	tr := newLDAPSTSThrottle()

	// First ldapThrottleBurst attempts from one IP/user are allowed.
	allowed := 0
	for i := 0; i < ldapThrottleBurst+5; i++ {
		if tr.allow("10.0.0.1", "alice") {
			allowed++
		}
	}
	if allowed != ldapThrottleBurst {
		t.Fatalf("expected exactly %d allowed in a burst, got %d", ldapThrottleBurst, allowed)
	}

	// Throttled even for a different username, because the IP bucket is exhausted.
	if tr.allow("10.0.0.1", "bob") {
		t.Fatal("expected same IP to be throttled regardless of username")
	}

	// A fresh IP with a fresh user is independent and allowed.
	if !tr.allow("10.0.0.2", "carol") {
		t.Fatal("expected a fresh IP/user to be allowed")
	}

	// Boune the user bucket for "matt" across many IPs.
	userAllowed := 0
	for i := 0; i < ldapThrottleBurst+5; i++ {
		ip := "192.168.1." + itoaByte(i)
		if tr.allow(ip, "matt") {
			userAllowed++
		}
	}
	if userAllowed != ldapThrottleBurst {
		t.Fatalf("expected per-username cap of %d across distinct IPs, got %d", ldapThrottleBurst, userAllowed)
	}

	// Empty IP and user share one throttled bucket
	emptyDenied := false
	for i := 0; i < ldapThrottleBurst+5; i++ {
		if !tr.allow("", "") {
			emptyDenied = true
			break
		}
	}
	if !emptyDenied {
		t.Fatal("empty ip/user must be throttled, not a free pass")
	}
}

// Fresh never-idle keys cant grow the tracking map unbounded.
func TestLDAPThrottleHardCapBoundsMap(t *testing.T) {
	tr := newLDAPSTSThrottle()
	now := UTCNow()
	for i := 0; i < ldapThrottleHardMax+2000; i++ {
		tr.allowKeyLocked(tr.perUser, fmt.Sprintf("user-%d", i), now)
	}
	if len(tr.perUser) > ldapThrottleHardMax {
		t.Fatalf("perUser map grew unbounded: %d entries, hard cap %d", len(tr.perUser), ldapThrottleHardMax)
	}
}

func itoaByte(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
