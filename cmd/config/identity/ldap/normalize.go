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

package ldap

import (
	"sort"
	"strings"

	ldap "github.com/go-ldap/ldap/v3"
)

// LDAP normalization as two byte-distinct spellings
func NormalizeDN(dn string) string {
	parsed, err := ldap.ParseDN(dn)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(dn))
	}
	rdns := make([]string, 0, len(parsed.RDNs))
	for _, rdn := range parsed.RDNs {
		attrs := make([]string, 0, len(rdn.Attributes))
		for _, ava := range rdn.Attributes {
			attrs = append(attrs, strings.ToLower(strings.TrimSpace(ava.Type))+"="+strings.ToLower(strings.TrimSpace(ava.Value)))
		}
		// Order within a multi-valued RDN is insignificant
		sort.Strings(attrs)
		rdns = append(rdns, strings.Join(attrs, "+"))
	}
	// RDN order is a directory path
	return strings.Join(rdns, ",")
}
