/*
 * PGG Obstor, (C) 2026 PGG, Inc.
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

package dockerscripts

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Mock entrypoint to test arguments and environment.
func TestEntrypointFrontend(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the container entrypoint requires a POSIX shell")
	}
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skipf("/bin/sh is unavailable: %v", err)
	}
	source, err := os.ReadFile("docker-entrypoint.sh")
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name         string
		args         []string
		certLocation string
		env          []string
		endpoint     string
		host         string
		inheritedCA  string
		disabled     bool
	}{
		{
			name:     "HTTP",
			args:     []string{"server", "/data"},
			endpoint: "http://127.0.0.1:9000",
			host:     "fixture-host:9000",
		},
		{
			name:         "HTTPS with spaces in certificate path",
			args:         []string{"server", "/data", "--s3-address", ":9443"},
			certLocation: "explicit",
			endpoint:     "https://fixture-host:9443",
			host:         "fixture-host:9443",
		},
		{
			name:         "HTTPS default certificate directory",
			args:         []string{"server", "/data", "--s3-address=:443"},
			certLocation: "default",
			endpoint:     "https://fixture-host:443",
			host:         "fixture-host",
		},
		{
			name: "environment overrides preserve whitespace and glob characters",
			args: []string{"server", "/data"},
			env: []string{
				"OBSTOR_ENDPOINT=https://example.test/a b?key=*",
				"OBSTOR_HOST=*",
				"NODE_EXTRA_CA_CERTS=/inherited certs/root.crt",
			},
			endpoint:    "https://example.test/a b?key=*",
			host:        "*",
			inheritedCA: "/inherited certs/root.crt",
		},
		{
			name:     "browser disabled",
			args:     []string{"server", "/data"},
			env:      []string{"OBSTOR_BROWSER=false"},
			disabled: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			if err := os.Mkdir(bin, 0o755); err != nil {
				t.Fatal(err)
			}
			write := func(path, content string, mode os.FileMode) {
				t.Helper()
				if err := os.WriteFile(path, []byte(content), mode); err != nil {
					t.Fatal(err)
				}
			}
			write(filepath.Join(bin, "node"), `#!/bin/sh
printf '%s\000' "$PORT" "$HOSTNAME" "$OBSTOR_ENDPOINT" "$OBSTOR_HOST" "${NODE_EXTRA_CA_CERTS-}" "$@" > "$ENTRYPOINT_TEST_NODE"
`, 0o755)
			write(filepath.Join(bin, "obstor"), `#!/bin/sh
printf '%s\000' "$@" > "$ENTRYPOINT_TEST_BACKEND"
`, 0o755)
			write(filepath.Join(bin, "hostname"), "#!/bin/sh\nprintf '%s\\n' fixture-host\n", 0o755)
			write(filepath.Join(dir, "OBSTOR_HOST=expanded"), "", 0o644)
			frontend := filepath.Join(dir, "server.js")
			write(frontend, "", 0o644)
			defaultCert := filepath.Join(dir, "default.crt")
			args := append([]string(nil), tc.args...)
			wantCA := tc.inheritedCA
			switch tc.certLocation {
			case "explicit":
				certDir := filepath.Join(dir, "custom certs")
				if err := os.Mkdir(certDir, 0o755); err != nil {
					t.Fatal(err)
				}
				wantCA = filepath.Join(certDir, "public.crt")
				write(wantCA, "fixture certificate", 0o644)
				args = append(args, "--certs-dir", certDir)
			case "default":
				wantCA = defaultCert
				write(defaultCert, "fixture certificate", 0o644)
			}

			// Certificate detection
			script := strings.NewReplacer(
				"/opt/frontend/server.js", `"$ENTRYPOINT_TEST_FRONTEND"`,
				"-f /etc/obstor/certs/public.crt", `-f "$ENTRYPOINT_TEST_DEFAULT_CERT"`,
				`CA_CERT="/etc/obstor/certs/public.crt"`, `CA_CERT="$ENTRYPOINT_TEST_DEFAULT_CERT"`,
			).Replace(string(source))
			entrypoint := filepath.Join(dir, "entrypoint.sh")
			write(entrypoint, script, 0o755)
			nodeOutput := filepath.Join(dir, "node.env")
			backendOutput := filepath.Join(dir, "backend.args")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "/bin/sh", append([]string{entrypoint}, args...)...)
			cmd.Dir = dir
			cmd.Env = append([]string{
				"PATH=" + bin + ":/usr/bin:/bin",
				"ENTRYPOINT_TEST_FRONTEND=" + frontend,
				"ENTRYPOINT_TEST_DEFAULT_CERT=" + defaultCert,
				"ENTRYPOINT_TEST_NODE=" + nodeOutput,
				"ENTRYPOINT_TEST_BACKEND=" + backendOutput,
			}, tc.env...)
			cmd.WaitDelay = 5 * time.Second
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("entrypoint failed: %v\n%s", err, output)
			}
			readFields := func(path string) []string {
				t.Helper()
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("missing process output %s: %v\nentrypoint stderr: %s", path, err, output)
				}
				return strings.Split(string(bytes.TrimSuffix(data, []byte{0})), "\x00")
			}
			if got := readFields(backendOutput); !reflect.DeepEqual(got, args) {
				t.Errorf("backend arguments = %q, want %q", got, args)
			}
			if tc.disabled {
				if _, err := os.Stat(nodeOutput); !os.IsNotExist(err) {
					t.Fatalf("frontend ran while browser was disabled: %v", err)
				}
				return
			}
			want := []string{"3000", "127.0.0.1", tc.endpoint, tc.host, wantCA, frontend}
			if got := readFields(nodeOutput); !reflect.DeepEqual(got, want) {
				t.Errorf("frontend environment and arguments = %q, want %q", got, want)
			}
		})
	}
}
