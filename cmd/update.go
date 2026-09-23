/*
 * MinIO Cloud Storage, (C) 2015-2021 MinIO, Inc.
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
	"bufio"
	"crypto"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	xhttp "github.com/obstor/obstor/cmd/http"
	"github.com/obstor/obstor/cmd/logger"
	"github.com/obstor/obstor/pkg/env"
	xnet "github.com/obstor/obstor/pkg/net"
	"github.com/obstor/selfupdate"
)

const (
	obstorReleaseTagTimeLayout = "2006-01-02T15-04-05Z"
	obstorOSARCH               = runtime.GOOS + "-" + runtime.GOARCH
	obstorReleaseURL           = "https://dl.pgg.net/packages/obstor/release/" + obstorOSARCH + SlashSeparator

	envMinisignPubKey = "OBSTOR_UPDATE_MINISIGN_PUBKEY"
	updateTimeout     = 10 * time.Second
)

var (
	// For windows our files have .exe additionally.
	obstorReleaseWindowsInfoURL = obstorReleaseURL + "obstor.exe.sha256sum"
)

// obstorVersionToReleaseTime - parses a standard official release
// Obstor version string.
//
// An official binary's version string is the release time formatted
// with RFC3339 (in UTC) - e.g. `2017-09-29T19:16:56Z`
func obstorVersionToReleaseTime(version string) (releaseTime time.Time, err error) {
	return time.Parse(time.RFC3339, version)
}

// releaseTimeToReleaseTag - converts a time to a string formatted as
// an official Obstor release tag.
//
// An official obstor release tag looks like:
// `RELEASE.2017-09-29T19-16-56Z`
func releaseTimeToReleaseTag(releaseTime time.Time) string {
	return "RELEASE." + releaseTime.Format(obstorReleaseTagTimeLayout)
}

// releaseTagToReleaseTime - reverse of `releaseTimeToReleaseTag()`
func releaseTagToReleaseTime(releaseTag string) (releaseTime time.Time, err error) {
	fields := strings.Split(releaseTag, ".")
	if len(fields) < 2 || len(fields) > 3 {
		return releaseTime, fmt.Errorf("%s is not a valid release tag", releaseTag)
	}
	if fields[0] != "RELEASE" {
		return releaseTime, fmt.Errorf("%s is not a valid release tag", releaseTag)
	}
	return time.Parse(obstorReleaseTagTimeLayout, fields[1])
}

// getModTime - get the file modification time of `path`
func getModTime(path string) (t time.Time, err error) {
	// Convert to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return t, fmt.Errorf("unable to get absolute path of %s. %w", path, err)
	}

	// Version is obstor non-standard, we will use obstor binary's
	// ModTime as release time.
	fi, err := os.Stat(absPath)
	if err != nil {
		return t, fmt.Errorf("unable to get ModTime of %s. %w", absPath, err)
	}

	// Return the ModTime
	return fi.ModTime().UTC(), nil
}

// GetCurrentReleaseTime - returns this process's release time.  If it
// is official obstor version, parsed version is returned else obstor
// binary's mod time is returned.
func GetCurrentReleaseTime() (releaseTime time.Time, err error) {
	if releaseTime, err = obstorVersionToReleaseTime(Version); err == nil {
		return releaseTime, err
	}

	// Looks like version is obstor non-standard, we use obstor
	// binary's ModTime as release time:
	return getModTime(os.Args[0])
}

// IsDocker - returns if the environment obstor is running in docker or
// not. The check is a simple file existence check.
//
// https://github.com/moby/moby/blob/master/daemon/initlayer/setup_unix.go#L25
//
//	"/.dockerenv":      "file",
func IsDocker() bool {
	if env.Get("OBSTOR_CI_CD", "") == "" {
		_, err := os.Stat("/.dockerenv")
		if osIsNotExist(err) {
			return false
		}

		// Log error, as we will not propagate it to caller
		logger.LogIf(GlobalContext, err)

		return err == nil
	}
	return false
}

// IsDCOS returns true if obstor is running in DCOS.
func IsDCOS() bool {
	if env.Get("OBSTOR_CI_CD", "") == "" {
		// http://mesos.apache.org/documentation/latest/docker-containerizer/
		// Mesos docker containerizer sets this value
		return env.Get("MESOS_CONTAINER_NAME", "") != ""
	}
	return false
}

// IsKubernetesReplicaSet returns true if obstor is running in kubernetes replica set.
func IsKubernetesReplicaSet() bool {
	return IsKubernetes() && (env.Get("KUBERNETES_REPLICA_SET", "") != "")
}

// IsKubernetes returns true if obstor is running in kubernetes.
func IsKubernetes() bool {
	if env.Get("OBSTOR_CI_CD", "") == "" {
		// Kubernetes env used to validate if we are
		// indeed running inside a kubernetes pod
		// is KUBERNETES_SERVICE_HOST
		// https://github.com/kubernetes/kubernetes/blob/master/pkg/kubelet/kubelet_pods.go#L541
		return env.Get("KUBERNETES_SERVICE_HOST", "") != ""
	}
	return false
}

// IsBOSH returns true if obstor is deployed from a bosh package
func IsBOSH() bool {
	// "/var/vcap/bosh" exists in BOSH deployed instance.
	_, err := os.Stat("/var/vcap/bosh")
	if osIsNotExist(err) {
		return false
	}

	// Log error, as we will not propagate it to caller
	logger.LogIf(GlobalContext, err)

	return err == nil
}

// Obstor Helm chart uses DownwardAPIFile to write pod label info to /podinfo/labels
// More info: https://kubernetes.io/docs/tasks/inject-data-application/downward-api-volume-expose-pod-information/#store-pod-fields
// Check if this is Helm package installation and report helm chart version
func getHelmVersion(helmInfoFilePath string) string {
	// Read the file exists.
	helmInfoFile, err := os.Open(helmInfoFilePath)
	if err != nil {
		// Log errors and return "" as Obstor can be deployed
		// without Helm charts as well.
		if !osIsNotExist(err) {
			reqInfo := (&logger.ReqInfo{}).AppendTags("helmInfoFilePath", helmInfoFilePath)
			ctx := logger.SetReqInfo(GlobalContext, reqInfo)
			logger.LogIf(ctx, err)
		}
		return ""
	}
	defer helmInfoFile.Close()

	scanner := bufio.NewScanner(helmInfoFile)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "chart=") {
			helmChartVersion := strings.TrimPrefix(scanner.Text(), "chart=")
			// Remove quotes from the chart version
			return strings.Trim(helmChartVersion, `"`)
		}
	}

	return ""
}

// IsSourceBuild - returns if this binary is a non-official build from
// source code.
func IsSourceBuild() bool {
	_, err := obstorVersionToReleaseTime(Version)
	return err != nil
}

// IsPCFTile returns if server is running in PCF
func IsPCFTile() bool {
	return env.Get("OBSTOR_PCF_TILE_VERSION", "") != ""
}

// DO NOT CHANGE USER AGENT STYLE.
// The style should be
//
//	Obstor (<OS>; <ARCH>[; <MODE>][; dcos][; kubernetes][; docker][; source]) Obstor/<VERSION> Obstor/<RELEASE-TAG> Obstor/<COMMIT-ID> [Obstor/universe-<PACKAGE-NAME>] [Obstor/helm-<HELM-VERSION>]
//
// Any change here should be discussed by opening an issue at
// https://github.com/obstor/obstor/issues.
func getUserAgent(mode string) string {

	userAgentParts := []string{}
	// Helper function to concisely append a pair of strings to a
	// the user-agent slice.
	uaAppend := func(p, q string) {
		userAgentParts = append(userAgentParts, p, q)
	}

	uaAppend("Obstor (", runtime.GOOS)
	uaAppend("; ", runtime.GOARCH)
	if mode != "" {
		uaAppend("; ", mode)
	}
	if IsDCOS() {
		uaAppend("; ", "dcos")
	}
	if IsKubernetes() {
		uaAppend("; ", "kubernetes")
	}
	if IsDocker() {
		uaAppend("; ", "docker")
	}
	if IsBOSH() {
		uaAppend("; ", "bosh")
	}
	if IsSourceBuild() {
		uaAppend("; ", "source")
	}

	uaAppend(") Obstor/", Version)
	uaAppend(" Obstor/", ReleaseTag)
	uaAppend(" Obstor/", CommitID)
	if IsDCOS() {
		universePkgVersion := env.Get("MARATHON_APP_LABEL_DCOS_PACKAGE_VERSION", "")
		// On DC/OS environment try to the get universe package version.
		if universePkgVersion != "" {
			uaAppend(" Obstor/universe-", universePkgVersion)
		}
	}

	if IsKubernetes() {
		// In Kubernetes environment, try to fetch the helm package version
		helmChartVersion := getHelmVersion("/podinfo/labels")
		if helmChartVersion != "" {
			uaAppend(" Obstor/helm-", helmChartVersion)
		}
		// In Kubernetes environment, try to fetch the Operator, VSPHERE plugin version
		opVersion := env.Get("OBSTOR_OPERATOR_VERSION", "")
		if opVersion != "" {
			uaAppend(" Obstor/operator-", opVersion)
		}
		vsphereVersion := env.Get("OBSTOR_VSPHERE_PLUGIN_VERSION", "")
		if vsphereVersion != "" {
			uaAppend(" Obstor/vsphere-plugin-", vsphereVersion)
		}
	}

	if IsPCFTile() {
		pcfTileVersion := env.Get("OBSTOR_PCF_TILE_VERSION", "")
		if pcfTileVersion != "" {
			uaAppend(" Obstor/pcf-tile-", pcfTileVersion)
		}
	}

	return strings.Join(userAgentParts, "")
}

func downloadReleaseURL(u *url.URL, timeout time.Duration, mode string) (content string, err error) {
	var reader io.ReadCloser
	if u.Scheme == "https" || u.Scheme == "http" {
		req, err := http.NewRequest(http.MethodGet, u.String(), nil)
		if err != nil {
			return content, AdminError{
				Code:       AdminUpdateUnexpectedFailure,
				Message:    err.Error(),
				StatusCode: http.StatusInternalServerError,
			}
		}
		req.Header.Set("User-Agent", getUserAgent(mode))

		client := &http.Client{Transport: getUpdateTransport(timeout)}
		resp, err := client.Do(req)
		if err != nil {
			if xnet.IsNetworkOrHostDown(err, false) {
				return content, AdminError{
					Code:       AdminUpdateURLNotReachable,
					Message:    err.Error(),
					StatusCode: http.StatusServiceUnavailable,
				}
			}
			return content, AdminError{
				Code:       AdminUpdateUnexpectedFailure,
				Message:    err.Error(),
				StatusCode: http.StatusInternalServerError,
			}
		}
		if resp == nil {
			return content, AdminError{
				Code:       AdminUpdateUnexpectedFailure,
				Message:    fmt.Sprintf("No response from server to download URL %s", u),
				StatusCode: http.StatusInternalServerError,
			}
		}
		reader = resp.Body
		defer xhttp.DrainBody(resp.Body)

		if resp.StatusCode != http.StatusOK {
			return content, AdminError{
				Code:       AdminUpdateUnexpectedFailure,
				Message:    fmt.Sprintf("error downloading URL %s. Response: %v", u, resp.Status),
				StatusCode: resp.StatusCode,
			}
		}
	} else {
		return content, AdminError{
			Code:       AdminUpdateUnexpectedFailure,
			Message:    fmt.Sprintf("unsupported update URL scheme: %s", u.Scheme),
			StatusCode: http.StatusBadRequest,
		}
	}

	contentBytes, err := io.ReadAll(reader)
	if err != nil {
		return content, AdminError{
			Code:       AdminUpdateUnexpectedFailure,
			Message:    fmt.Sprintf("Error reading response. %s", err),
			StatusCode: http.StatusInternalServerError,
		}
	}

	return string(contentBytes), nil
}

// parseReleaseData - parses release info file content fetched from
// official obstor download server.
//
// The expected format is a single line with two words like:
//
// fbe246edbd382902db9a4035df7dce8cb441357d obstor.RELEASE.2016-10-07T01-16-39Z.<hotfix_optional>
//
// The second word must be `obstor.` appended to a standard release tag.
func parseReleaseData(data string) (sha256Sum []byte, releaseTime time.Time, releaseInfo string, err error) {
	defer func() {
		if err != nil {
			err = AdminError{
				Code:       AdminUpdateUnexpectedFailure,
				Message:    err.Error(),
				StatusCode: http.StatusInternalServerError,
			}
		}
	}()

	fields := strings.Fields(data)
	if len(fields) != 2 {
		err = fmt.Errorf("unknown release data `%s`", data)
		return sha256Sum, releaseTime, releaseInfo, err
	}

	sha256Sum, err = hex.DecodeString(fields[0])
	if err != nil {
		return sha256Sum, releaseTime, releaseInfo, err
	}

	releaseInfo = fields[1]

	// Split release of style obstor.RELEASE.2019-08-21T19-40-07Z.<hotfix>
	nfields := strings.SplitN(releaseInfo, ".", 2)
	if len(nfields) != 2 {
		err = fmt.Errorf("unknown release information `%s`", releaseInfo)
		return sha256Sum, releaseTime, releaseInfo, err
	}
	if nfields[0] != "obstor" {
		err = fmt.Errorf("unknown release `%s`", releaseInfo)
		return sha256Sum, releaseTime, releaseInfo, err
	}

	releaseTime, err = releaseTagToReleaseTime(nfields[1])
	if err != nil {
		err = fmt.Errorf("unknown release tag format. %w", err)
	}

	return sha256Sum, releaseTime, releaseInfo, err
}

func getUpdateTransport(timeout time.Duration) http.RoundTripper {
	var updateTransport http.RoundTripper = &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           xhttp.NewCustomDialContext(timeout),
		IdleConnTimeout:       timeout,
		TLSHandshakeTimeout:   timeout,
		ExpectContinueTimeout: timeout,
		TLSClientConfig: &tls.Config{
			RootCAs: globalRootCAs,
		},
		DisableCompression: true,
	}
	return updateTransport
}

func getLatestReleaseTime(u *url.URL, timeout time.Duration, mode string) (sha256Sum []byte, releaseTime time.Time, err error) {
	data, err := downloadReleaseURL(u, timeout, mode)
	if err != nil {
		return sha256Sum, releaseTime, err
	}

	sha256Sum, releaseTime, _, err = parseReleaseData(data)
	return
}

const (
	// Kubernetes deployment doc link.
	kubernetesDeploymentDoc = "https://obstor.net/docs/deploy-obstor-on-kubernetes"

	// Mesos deployment doc link.
	mesosDeploymentDoc = "https://obstor.net/docs/deploy-obstor-on-dc-os"
)

func getDownloadURL(releaseTag string) (downloadURL string) {
	// Check if we are in DCOS environment, return
	// deployment guide for update procedures.
	if IsDCOS() {
		return mesosDeploymentDoc
	}

	// Check if we are in kubernetes environment, return
	// deployment guide for update procedures.
	if IsKubernetes() {
		return kubernetesDeploymentDoc
	}

	// Check if we are docker environment, return docker update command
	if IsDocker() {
		// Construct release tag name.
		return fmt.Sprintf("docker pull obstor/obstor:%s", releaseTag)
	}

	// For binary only installations, we return link to the latest binary.
	if runtime.GOOS == "windows" {
		return obstorReleaseURL + "obstor.exe"
	}

	return obstorReleaseURL + "obstor"
}

func getUpdateReaderFromURL(u *url.URL, transport http.RoundTripper, mode string) (io.ReadCloser, error) {
	clnt := &http.Client{
		Transport: transport,
	}
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, AdminError{
			Code:       AdminUpdateUnexpectedFailure,
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		}
	}

	req.Header.Set("User-Agent", getUserAgent(mode))

	resp, err := clnt.Do(req)
	if err != nil {
		if xnet.IsNetworkOrHostDown(err, false) {
			return nil, AdminError{
				Code:       AdminUpdateURLNotReachable,
				Message:    err.Error(),
				StatusCode: http.StatusServiceUnavailable,
			}
		}
		return nil, AdminError{
			Code:       AdminUpdateUnexpectedFailure,
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		}
	}
	return resp.Body, nil
}

func doUpdate(u *url.URL, _ time.Time, sha256Sum []byte, releaseInfo string, mode string) (err error) {
	transport := getUpdateTransport(30 * time.Second)
	var reader io.ReadCloser
	// CVE-2022-35919: Only allow HTTP(S) update URLs to prevent arbitrary file reads.
	if u.Scheme == "https" || u.Scheme == "http" {
		reader, err = getUpdateReaderFromURL(u, transport, mode)
		if err != nil {
			return err
		}
	} else {
		return AdminError{
			Code:       AdminUpdateUnexpectedFailure,
			Message:    fmt.Sprintf("unsupported update URL scheme: %s", u.Scheme),
			StatusCode: http.StatusBadRequest,
		}
	}

	opts := selfupdate.Options{
		Hash:     crypto.SHA256,
		Checksum: sha256Sum,
	}

	minisignPubkey := env.Get(envMinisignPubKey, "")
	if minisignPubkey != "" {
		v := selfupdate.NewVerifier()
		u.Path = path.Dir(u.Path) + slashSeparator + releaseInfo + ".minisig"
		if err = v.LoadFromURL(u.String(), minisignPubkey, transport); err != nil {
			return AdminError{
				Code:       AdminUpdateApplyFailure,
				Message:    fmt.Sprintf("signature loading failed for %v with %v", u, err),
				StatusCode: http.StatusInternalServerError,
			}
		}
		opts.Verifier = v
	}

	if err = selfupdate.Apply(reader, opts); err != nil {
		if rerr := selfupdate.RollbackError(err); rerr != nil {
			return AdminError{
				Code:       AdminUpdateApplyFailure,
				Message:    fmt.Sprintf("Failed to rollback from bad update: %v", rerr),
				StatusCode: http.StatusInternalServerError,
			}
		}
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			return AdminError{
				Code: AdminUpdateApplyFailure,
				Message: fmt.Sprintf("Unable to update the binary at %s: %v",
					filepath.Dir(pathErr.Path), pathErr.Err),
				StatusCode: http.StatusForbidden,
			}
		}
		return AdminError{
			Code:       AdminUpdateApplyFailure,
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		}
	}

	return nil
}
