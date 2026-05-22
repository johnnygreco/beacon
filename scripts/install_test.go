package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallSkipsChecksumSidecarDownloadsWhenDisabled(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skipf("sh unavailable: %v", err)
	}
	scriptPath, err := filepath.Abs("../install.sh")
	if err != nil {
		t.Fatalf("resolve install script: %v", err)
	}
	tempDir := t.TempDir()
	fakeBin := filepath.Join(tempDir, "bin")
	curlLog := filepath.Join(tempDir, "curl.log")
	installDir := filepath.Join(tempDir, "install")

	writeExecutable(t, filepath.Join(fakeBin, "uname"), `#!/bin/sh
case "$1" in
  -s) echo Linux ;;
  -m) echo x86_64 ;;
  *) exit 2 ;;
esac
`)
	writeExecutable(t, filepath.Join(fakeBin, "curl"), `#!/bin/sh
url=""
dest=""
previous=""
for arg in "$@"; do
  if [ "$previous" = "-o" ]; then
    dest="$arg"
  fi
  case "$arg" in
    http*) url="$arg" ;;
  esac
  previous="$arg"
done
printf '%s\n' "$url" >> "$CURL_LOG"
case "$url" in
  *checksums.txt|*.sha512)
    echo "unexpected checksum sidecar download: $url" >&2
    exit 42
    ;;
esac
if [ -z "$dest" ]; then
  echo "missing curl destination" >&2
  exit 2
fi
printf 'archive fixture\n' > "$dest"
`)
	writeExecutable(t, filepath.Join(fakeBin, "tar"), `#!/bin/sh
out=""
previous=""
for arg in "$@"; do
  if [ "$previous" = "-C" ]; then
    out="$arg"
  fi
  previous="$arg"
done
if [ -z "$out" ]; then
  echo "missing tar output directory" >&2
  exit 2
fi
mkdir -p "$out/pkg/usr/bin"
printf '#!/bin/sh\n' > "$out/beacon"
chmod 755 "$out/beacon"
printf '#!/bin/sh\n' > "$out/pkg/usr/bin/clickhouse"
chmod 755 "$out/pkg/usr/bin/clickhouse"
`)

	cmd := exec.Command("sh", scriptPath)
	cmd.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin",
		"HOME="+filepath.Join(tempDir, "home"),
		"INSTALL_DIR="+installDir,
		"CLICKHOUSE_INSTALL_DIR="+filepath.Join(tempDir, "clickhouse-bin"),
		"VERSION=1.2.3",
		"VERIFY_CHECKSUMS=0",
		"INSTALL_CLICKHOUSE=1",
		"CURL_LOG="+curlLog,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install script failed: %v\n%s", err, out)
	}
	gotOutput := string(out)
	for _, want := range []string{
		"Warning: checksum verification disabled for beacon_linux_amd64.tar.gz",
		"Warning: checksum verification disabled for clickhouse-common-static-24.12.6.70-amd64.tgz",
	} {
		if !strings.Contains(gotOutput, want) {
			t.Fatalf("install output missing %q:\n%s", want, gotOutput)
		}
	}
	logData, err := os.ReadFile(curlLog)
	if err != nil {
		t.Fatalf("read curl log: %v", err)
	}
	for _, forbidden := range []string{"checksums.txt", ".sha512"} {
		if strings.Contains(string(logData), forbidden) {
			t.Fatalf("checksum sidecar was downloaded despite VERIFY_CHECKSUMS=0:\n%s", logData)
		}
	}
	if _, err := os.Stat(filepath.Join(installDir, "beacon")); err != nil {
		t.Fatalf("beacon binary was not installed: %v", err)
	}
}
