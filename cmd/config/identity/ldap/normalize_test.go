/*
 * PGG Obstor, (C) 2021-2026 PGG, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

package ldap

import "testing"

func TestNormalizeDN(t *testing.T) {
	cases := []struct {
		a, b  string
		equal bool
	}{
		// Case-insensitive: same identity, different case -> same key.
		{"uid=Alice,OU=People,DC=Example,DC=com", "uid=alice,ou=people,dc=example,dc=com", true},
		// Insignificant whitespace around RDN separators -> same key.
		{"uid=alice, ou=people,  dc=example, dc=com", "uid=alice,ou=people,dc=example,dc=com", true},
		// Multi-valued RDN attribute order is insignificant -> same key.
		{"ou=eng+cn=alice,dc=corp", "cn=alice+ou=eng,dc=corp", true},
		// Distinct identities must stay distinct.
		{"cn=bob,dc=corp", "cn=carol,dc=corp", false},
		// Different RDN path order is significant -> distinct.
		{"cn=alice,ou=people,dc=corp", "ou=people,cn=alice,dc=corp", false},
	}
	for _, c := range cases {
		na, nb := NormalizeDN(c.a), NormalizeDN(c.b)
		if (na == nb) != c.equal {
			t.Errorf("NormalizeDN(%q)=%q vs NormalizeDN(%q)=%q: got equal=%v want %v",
				c.a, na, c.b, nb, na == nb, c.equal)
		}
	}
}
