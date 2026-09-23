/*
 * MinIO Cloud Storage, (C) 2018-2020 MinIO, Inc.
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
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/obstor/obstor/cmd/config/identity/openid"
	xhttp "github.com/obstor/obstor/cmd/http"
	"github.com/obstor/obstor/cmd/logger"
	"github.com/obstor/obstor/pkg/auth"
	"github.com/obstor/obstor/pkg/handlers"
	iampolicy "github.com/obstor/obstor/pkg/iam/policy"
	"github.com/obstor/obstor/pkg/wildcard"
)

const (
	// STS API version.
	stsAPIVersion       = "2011-06-15"
	stsVersion          = "Version"
	stsAction           = "Action"
	stsPolicy           = "Policy"
	stsToken            = "Token"
	stsWebIdentityToken = "WebIdentityToken"
	stsDurationSeconds  = "DurationSeconds"
	stsLDAPUsername     = "LDAPUsername"
	stsLDAPPassword     = "LDAPPassword"

	// STS API action constants
	clientGrants = "AssumeRoleWithClientGrants"
	webIdentity  = "AssumeRoleWithWebIdentity"
	ldapIdentity = "AssumeRoleWithLDAPIdentity"
	assumeRole   = "AssumeRole"

	stsRequestBodyLimit = 10 * (1 << 20) // 10 MiB

	// JWT claim keys
	expClaim = "exp"
	subClaim = "sub"

	// JWT claim to check the parent user
	parentClaim = "parent"

	// LDAP claim keys
	ldapUser = "ldapUser"
)

// stsAPIHandlers implements and provides http handlers for AWS STS API.
type stsAPIHandlers struct{}

func filterReservedClaims(m map[string]interface{}) {
	delete(m, ldapUser)
	delete(m, parentClaim)
	delete(m, iampolicy.SessionPolicyName)
	delete(m, iampolicy.DecodedSessionPolicyKey)
	delete(m, iamPolicyClaimNameSA())
}

// registerSTSRouter - registers AWS STS compatible APIs.
func registerSTSRouter(router *mux.Router) {
	// Initialize STS.
	sts := &stsAPIHandlers{}

	// STS Router
	stsRouter := router.NewRoute().PathPrefix(SlashSeparator).Subrouter()

	// Assume roles with no JWT, handles AssumeRole.
	stsRouter.Methods(http.MethodPost).MatcherFunc(func(r *http.Request, rm *mux.RouteMatch) bool {
		ctypeOk := wildcard.MatchSimple("application/x-www-form-urlencoded*", r.Header.Get(xhttp.ContentType))
		authOk := wildcard.MatchSimple(signV4Algorithm+"*", r.Header.Get(xhttp.Authorization))
		noQueries := len(r.URL.Query()) == 0
		return ctypeOk && authOk && noQueries
	}).HandlerFunc(httpTraceAll(sts.AssumeRole))

	// Assume roles with JWT handler, handles both ClientGrants and WebIdentity.
	stsRouter.Methods(http.MethodPost).MatcherFunc(func(r *http.Request, rm *mux.RouteMatch) bool {
		ctypeOk := wildcard.MatchSimple("application/x-www-form-urlencoded*", r.Header.Get(xhttp.ContentType))
		noQueries := len(r.URL.Query()) == 0
		return ctypeOk && noQueries
	}).HandlerFunc(httpTraceAll(sts.AssumeRoleWithSSO))

	// AssumeRoleWithClientGrants
	stsRouter.Methods(http.MethodPost).HandlerFunc(httpTraceAll(sts.AssumeRoleWithClientGrants)).
		Queries(stsAction, clientGrants).
		Queries(stsVersion, stsAPIVersion).
		Queries(stsToken, "{Token:.*}")

	// AssumeRoleWithWebIdentity
	stsRouter.Methods(http.MethodPost).HandlerFunc(httpTraceAll(sts.AssumeRoleWithWebIdentity)).
		Queries(stsAction, webIdentity).
		Queries(stsVersion, stsAPIVersion).
		Queries(stsWebIdentityToken, "{Token:.*}")

	// AssumeRoleWithLDAPIdentity
	stsRouter.Methods(http.MethodPost).HandlerFunc(httpTraceAll(sts.AssumeRoleWithLDAPIdentity)).
		Queries(stsAction, ldapIdentity).
		Queries(stsVersion, stsAPIVersion).
		Queries(stsLDAPUsername, "{LDAPUsername:.*}").
		Queries(stsLDAPPassword, "{LDAPPassword:.*}")
}

func checkAssumeRoleAuth(ctx context.Context, r *http.Request) (user auth.Credentials, isErrCodeSTS bool, stsErr STSErrorCode) {
	switch getRequestAuthType(r) {
	default:
		return user, true, ErrSTSAccessDenied
	case authTypeSigned:
		s3Err := isReqAuthenticated(ctx, r, globalServerRegion, serviceSTS)
		if APIErrorCode(s3Err) != ErrNone {
			return user, false, STSErrorCode(s3Err)
		}
		var owner bool
		user, owner, s3Err = getReqAccessKeyV4(r, globalServerRegion, serviceSTS)
		if APIErrorCode(s3Err) != ErrNone {
			return user, false, STSErrorCode(s3Err)
		}
		// Root credentials are not allowed to use STS API
		if owner {
			return user, true, ErrSTSAccessDenied
		}
	}

	// Session tokens are not allowed in STS AssumeRole requests.
	if getSessionToken(r) != "" {
		return user, true, ErrSTSAccessDenied
	}

	// Temporary credentials or Service accounts cannot generate further temporary credentials.
	if user.IsTemp() || user.IsServiceAccount() {
		return user, true, ErrSTSAccessDenied
	}

	return user, true, ErrSTSNone
}

// AssumeRole - implementation of AWS STS API AssumeRole to get temporary
// credentials for regular users on Obstor.
// https://docs.aws.amazon.com/STS/latest/APIReference/API_AssumeRole.html
func (sts *stsAPIHandlers) AssumeRole(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "AssumeRole")

	// Authenticate r.ParseForm before r.Form
	user, isErrCodeSTS, stsErr := checkAssumeRoleAuth(ctx, r)
	if err := r.ParseForm(); err != nil {
		writeSTSErrorResponse(ctx, w, true, ErrSTSInvalidParameterValue, err)
		return
	}

	// Verify the API version
	if r.Form.Get(stsVersion) != stsAPIVersion {
		writeSTSErrorResponse(ctx, w, true, ErrSTSMissingParameter, fmt.Errorf("invalid STS API version %s, expecting %s", r.Form.Get(stsVersion), stsAPIVersion))
		return
	}

	action := r.Form.Get(stsAction)
	if action != assumeRole {
		writeSTSErrorResponse(ctx, w, true, ErrSTSInvalidParameterValue, fmt.Errorf("unsupported action %s", action))
		return
	}

	ctx = newContext(r, w, action)
	defer logger.AuditLog(ctx, w, r, nil)

	// After audit setup, check for earlier authentication failure
	if stsErr != ErrSTSNone {
		writeSTSErrorResponse(ctx, w, isErrCodeSTS, stsErr, nil)
		return
	}

	// Enforce the 2048-character cap on inline/managed session policies.
	sessionPolicyStr := r.Form.Get(stsPolicy)
	if len(sessionPolicyStr) > 2048 {
		writeSTSErrorResponse(ctx, w, true, ErrSTSInvalidParameterValue, fmt.Errorf("session policy exceeds 2048-character limit"))
		return
	}

	if len(sessionPolicyStr) > 0 {
		sessionPolicy, err := iampolicy.ParseConfig(bytes.NewReader([]byte(sessionPolicyStr)))
		if err != nil {
			writeSTSErrorResponse(ctx, w, true, ErrSTSInvalidParameterValue, err)
			return
		}
		if sessionPolicy.Version == "" {
			writeSTSErrorResponse(ctx, w, true, ErrSTSInvalidParameterValue, fmt.Errorf("policy version is required (expected '2012-10-17')"))
			return
		}
	}

	var err error
	claims := make(map[string]interface{})
	claims[expClaim], err = openid.GetDefaultExpiration(r.Form.Get(stsDurationSeconds))
	if err != nil {
		writeSTSErrorResponse(ctx, w, true, ErrSTSInvalidParameterValue, err)
		return
	}

	// Look up the policies associated to the user so the temp
	// credentials inherit the same permissions
	policies, err := globalIAMSys.PolicyDBGet(user.AccessKey, false)
	if err != nil {
		writeSTSErrorResponse(ctx, w, true, ErrSTSInvalidParameterValue, err)
		return
	}

	policyName := strings.Join(policies, ",")
	claims[iamPolicyClaimNameOpenID()] = policyName

	if len(sessionPolicyStr) > 0 {
		claims[iampolicy.SessionPolicyName] = base64.StdEncoding.EncodeToString([]byte(sessionPolicyStr))
	}

	cred, err := auth.GetNewCredentialsWithMetadata(claims, globalActiveCred.SecretKey)
	if err != nil {
		writeSTSErrorResponse(ctx, w, true, ErrSTSInternalError, err)
		return
	}

	// Set the parent of the temporary access key, this is useful
	// in obtaining service accounts by this cred.
	cred.ParentUser = user.AccessKey

	// Set the newly generated credentials.
	if err = globalIAMSys.SetTempUser(cred.AccessKey, cred, policyName); err != nil {
		writeSTSErrorResponse(ctx, w, true, ErrSTSInternalError, err)
		return
	}

	// Notify all other Obstor peers to reload temp users
	for _, nerr := range globalNotificationSys.LoadUser(cred.AccessKey, true) {
		if nerr.Err != nil {
			logger.GetReqInfo(ctx).SetTags("peerAddress", nerr.Host.String())
			logger.LogIf(ctx, nerr.Err)
		}
	}

	resp := &AssumeRoleResponse{
		Result: AssumeRoleResult{
			Credentials: cred,
		},
	}
	resp.ResponseMetadata.RequestID = w.Header().Get(xhttp.AmzRequestID)
	writeSuccessResponseXML(w, encodeResponse(resp))
}

func (sts *stsAPIHandlers) AssumeRoleWithSSO(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "AssumeRoleSSOCommon")

	// Parse the incoming form data.
	if err := r.ParseForm(); err != nil {
		writeSTSErrorResponse(ctx, w, true, ErrSTSInvalidParameterValue, err)
		return
	}

	if r.Form.Get(stsVersion) != stsAPIVersion {
		writeSTSErrorResponse(ctx, w, true, ErrSTSMissingParameter, fmt.Errorf("invalid STS API version %s, expecting %s", r.Form.Get("Version"), stsAPIVersion))
		return
	}

	action := r.Form.Get(stsAction)
	switch action {
	case ldapIdentity:
		sts.AssumeRoleWithLDAPIdentity(w, r)
		return
	case clientGrants, webIdentity:
	default:
		writeSTSErrorResponse(ctx, w, true, ErrSTSInvalidParameterValue, fmt.Errorf("unsupported action %s", action))
		return
	}

	ctx = newContext(r, w, action)
	defer logger.AuditLog(ctx, w, r, nil)

	if globalOpenIDValidators == nil {
		writeSTSErrorResponse(ctx, w, true, ErrSTSNotInitialized, errServerNotInitialized)
		return
	}

	v, err := globalOpenIDValidators.Get("jwt")
	if err != nil {
		writeSTSErrorResponse(ctx, w, true, ErrSTSInvalidParameterValue, err)
		return
	}

	token := r.Form.Get(stsToken)
	if token == "" {
		token = r.Form.Get(stsWebIdentityToken)
	}

	m, err := v.Validate(token, r.Form.Get(stsDurationSeconds))
	if err != nil {
		switch err {
		case openid.ErrTokenExpired:
			switch action {
			case clientGrants:
				writeSTSErrorResponse(ctx, w, true, ErrSTSClientGrantsExpiredToken, err)
			case webIdentity:
				writeSTSErrorResponse(ctx, w, true, ErrSTSWebIdentityExpiredToken, err)
			}
			return
		case auth.ErrInvalidDuration:
			writeSTSErrorResponse(ctx, w, true, ErrSTSInvalidParameterValue, err)
			return
		}
		writeSTSErrorResponse(ctx, w, true, ErrSTSInvalidParameterValue, err)
		return
	}

	filterReservedClaims(m)

	// JWT has requested a custom claim with policy value set.
	// This is a Obstor STS API specific value, this value should
	// be set and configured on your identity provider as part of
	// JWT custom claims.
	var policyName string
	policySet, ok := iampolicy.GetPoliciesFromClaims(m, iamPolicyClaimNameOpenID())
	if ok {
		policyName = globalIAMSys.CurrentPolicies(strings.Join(policySet.ToSlice(), ","))
	}

	if policyName == "" && globalPolicyOPA == nil {
		writeSTSErrorResponse(ctx, w, true, ErrSTSInvalidParameterValue,
			fmt.Errorf("%s claim missing from the JWT token, credentials will not be generated", iamPolicyClaimNameOpenID()))
		return
	}
	m[iamPolicyClaimNameOpenID()] = policyName

	sessionPolicyStr := r.Form.Get(stsPolicy)
	// https://docs.aws.amazon.com/STS/latest/APIReference/API_AssumeRoleWithWebIdentity.html
	// The plain text that you use for both inline and managed session
	// policies shouldn't exceed 2048 characters.
	if len(sessionPolicyStr) > 2048 {
		writeSTSErrorResponse(ctx, w, true, ErrSTSInvalidParameterValue, fmt.Errorf("session policy should not exceed 2048 characters"))
		return
	}

	if len(sessionPolicyStr) > 0 {
		sessionPolicy, err := iampolicy.ParseConfig(bytes.NewReader([]byte(sessionPolicyStr)))
		if err != nil {
			writeSTSErrorResponse(ctx, w, true, ErrSTSInvalidParameterValue, err)
			return
		}

		// Version in policy must not be empty
		if sessionPolicy.Version == "" {
			writeSTSErrorResponse(ctx, w, true, ErrSTSInvalidParameterValue, fmt.Errorf("invalid session policy version"))
			return
		}

		m[iampolicy.SessionPolicyName] = base64.StdEncoding.EncodeToString([]byte(sessionPolicyStr))
	}

	secret := globalActiveCred.SecretKey
	cred, err := auth.GetNewCredentialsWithMetadata(m, secret)
	if err != nil {
		writeSTSErrorResponse(ctx, w, true, ErrSTSInternalError, err)
		return
	}

	var subFromToken string
	if v, ok := m[subClaim]; ok {
		subFromToken, _ = v.(string)
	}

	// Set the newly generated credentials.
	if err = globalIAMSys.SetTempUser(cred.AccessKey, cred, policyName); err != nil {
		writeSTSErrorResponse(ctx, w, true, ErrSTSInternalError, err)
		return
	}

	// Notify all other Obstor peers to reload temp users
	for _, nerr := range globalNotificationSys.LoadUser(cred.AccessKey, true) {
		if nerr.Err != nil {
			logger.GetReqInfo(ctx).SetTags("peerAddress", nerr.Host.String())
			logger.LogIf(ctx, nerr.Err)
		}
	}

	var encodedSuccessResponse []byte
	switch action {
	case clientGrants:
		clientGrantsResponse := &AssumeRoleWithClientGrantsResponse{
			Result: ClientGrantsResult{
				Credentials:      cred,
				SubjectFromToken: subFromToken,
			},
		}
		clientGrantsResponse.ResponseMetadata.RequestID = w.Header().Get(xhttp.AmzRequestID)
		encodedSuccessResponse = encodeResponse(clientGrantsResponse)
	case webIdentity:
		webIdentityResponse := &AssumeRoleWithWebIdentityResponse{
			Result: WebIdentityResult{
				Credentials:                 cred,
				SubjectFromWebIdentityToken: subFromToken,
			},
		}
		webIdentityResponse.ResponseMetadata.RequestID = w.Header().Get(xhttp.AmzRequestID)
		encodedSuccessResponse = encodeResponse(webIdentityResponse)
	}

	writeSuccessResponseXML(w, encodedSuccessResponse)
}

// AssumeRoleWithWebIdentity - implementation of AWS STS API supporting OAuth2.0
// users from web identity provider such as Facebook, Google, or any OpenID
// Connect-compatible identity provider.
//
// Eg:-
//
//	$ curl https://obstor:9000/?Action=AssumeRoleWithWebIdentity&WebIdentityToken=<jwt>
func (sts *stsAPIHandlers) AssumeRoleWithWebIdentity(w http.ResponseWriter, r *http.Request) {
	sts.AssumeRoleWithSSO(w, r)
}

// AssumeRoleWithClientGrants - implementation of AWS STS extension API supporting
// OAuth2.0 client credential grants.
//
// Eg:-
//
//	$ curl https://obstor:9000/?Action=AssumeRoleWithClientGrants&Token=<jwt>
func (sts *stsAPIHandlers) AssumeRoleWithClientGrants(w http.ResponseWriter, r *http.Request) {
	sts.AssumeRoleWithSSO(w, r)
}

// AssumeRoleWithLDAPIdentity - implements user auth against LDAP server
func (sts *stsAPIHandlers) AssumeRoleWithLDAPIdentity(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "AssumeRoleWithLDAPIdentity")

	defer logger.AuditLog(ctx, w, r, nil, stsLDAPPassword)

	// Parse the incoming form data.
	if err := r.ParseForm(); err != nil {
		writeSTSErrorResponse(ctx, w, true, ErrSTSInvalidParameterValue, err)
		return
	}

	if r.Form.Get(stsVersion) != stsAPIVersion {
		writeSTSErrorResponse(ctx, w, true, ErrSTSMissingParameter,
			fmt.Errorf("invalid STS API version %s, expecting %s", r.Form.Get("Version"), stsAPIVersion))
		return
	}

	ldapUsername := r.Form.Get(stsLDAPUsername)
	ldapPassword := r.Form.Get(stsLDAPPassword)

	if ldapUsername == "" || ldapPassword == "" {
		writeSTSErrorResponse(ctx, w, true, ErrSTSMissingParameter, fmt.Errorf("LDAPUsername and LDAPPassword cannot be empty"))
		return
	}

	clientIP := handlers.GetSourceIP(r)
	if !globalLDAPSTSThrottle.allow(clientIP, ldapUsername) {
		logger.LogIf(ctx, fmt.Errorf("LDAP STS auth throttled for ip=%s", clientIP))
		writeSTSErrorResponse(ctx, w, true, ErrSTSInvalidParameterValue, errLDAPAuthFailed)
		return
	}

	action := r.Form.Get(stsAction)
	switch action {
	case ldapIdentity:
	default:
		writeSTSErrorResponse(ctx, w, true, ErrSTSInvalidParameterValue, fmt.Errorf("unsupported action %s", action))
		return
	}

	sessionPolicyStr := r.Form.Get(stsPolicy)
	// https://docs.aws.amazon.com/STS/latest/APIReference/API_AssumeRole.html
	// The plain text that you use for both inline and managed session
	// policies shouldn't exceed 2048 characters.
	if len(sessionPolicyStr) > 2048 {
		writeSTSErrorResponse(ctx, w, true, ErrSTSInvalidParameterValue, fmt.Errorf("session policy should not exceed 2048 characters"))
		return
	}

	if len(sessionPolicyStr) > 0 {
		sessionPolicy, err := iampolicy.ParseConfig(bytes.NewReader([]byte(sessionPolicyStr)))
		if err != nil {
			writeSTSErrorResponse(ctx, w, true, ErrSTSInvalidParameterValue, err)
			return
		}

		// Version in policy must not be empty
		if sessionPolicy.Version == "" {
			writeSTSErrorResponse(ctx, w, true, ErrSTSInvalidParameterValue, fmt.Errorf("version needs to be specified in session policy"))
			return
		}
	}

	ldapUserDN, groupDistNames, err := globalLDAPConfig.Bind(ldapUsername, ldapPassword)
	if err != nil {
		logger.LogIf(ctx, fmt.Errorf("LDAP bind failed for STS request: %w", err))
		writeSTSErrorResponse(ctx, w, true, ErrSTSInvalidParameterValue, errLDAPAuthFailed)
		return
	}

	// Check if this user or their groups have a policy applied.
	ldapPolicies, _ := globalIAMSys.PolicyDBGet(ldapUserDN, false, groupDistNames...)
	if len(ldapPolicies) == 0 {
		logger.LogIf(ctx, fmt.Errorf("no policy set for LDAP user %q or groups %v - rejecting STS request", ldapUserDN, groupDistNames))
		writeSTSErrorResponse(ctx, w, true, ErrSTSInvalidParameterValue, errLDAPAuthFailed)
		return
	}

	expiryDur := globalLDAPConfig.GetExpiryDuration()
	m := map[string]interface{}{
		expClaim: UTCNow().Add(expiryDur).Unix(),
		ldapUser: ldapUserDN,
	}

	if len(sessionPolicyStr) > 0 {
		m[iampolicy.SessionPolicyName] = base64.StdEncoding.EncodeToString([]byte(sessionPolicyStr))
	}

	secret := globalActiveCred.SecretKey
	cred, err := auth.GetNewCredentialsWithMetadata(m, secret)
	if err != nil {
		writeSTSErrorResponse(ctx, w, true, ErrSTSInternalError, err)
		return
	}

	// Set the parent of the temporary access key, this is useful
	// in obtaining service accounts by this cred.
	cred.ParentUser = ldapUserDN

	// Set this value to LDAP groups, LDAP user can be part
	// of large number of groups
	cred.Groups = groupDistNames

	// Set the newly generated credentials, policyName is empty on purpose
	// LDAP policies are applied automatically using their ldapUser, ldapGroups
	// mapping.
	if err = globalIAMSys.SetTempUser(cred.AccessKey, cred, ""); err != nil {
		writeSTSErrorResponse(ctx, w, true, ErrSTSInternalError, err)
		return
	}

	// Notify all other Obstor peers to reload temp users
	for _, nerr := range globalNotificationSys.LoadUser(cred.AccessKey, true) {
		if nerr.Err != nil {
			logger.GetReqInfo(ctx).SetTags("peerAddress", nerr.Host.String())
			logger.LogIf(ctx, nerr.Err)
		}
	}

	ldapIdentityResponse := &AssumeRoleWithLDAPResponse{
		Result: LDAPIdentityResult{
			Credentials: cred,
		},
	}
	ldapIdentityResponse.ResponseMetadata.RequestID = w.Header().Get(xhttp.AmzRequestID)
	encodedSuccessResponse := encodeResponse(ldapIdentityResponse)

	writeSuccessResponseXML(w, encodedSuccessResponse)
}
