package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishRollbackPlan(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skipf("bash unavailable: %v", err)
	}

	cmd := exec.Command("bash", "./publish.sh", "--rollback-plan", "1.2.3")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rollback plan failed: %v\n%s", err, out)
	}
	got := string(out)
	for _, want := range []string{
		"Rollback steps for v1.2.3:",
		"gh release delete v1.2.3 --cleanup-tag --yes",
		"git push origin :refs/tags/v1.2.3",
		"git tag -d v1.2.3",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("rollback plan missing %q:\n%s", want, got)
		}
	}
}

func TestPublishReleaseFailureRollsBackPushedTag(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skipf("bash unavailable: %v", err)
	}
	scriptPath, err := filepath.Abs("publish.sh")
	if err != nil {
		t.Fatalf("resolve publish script: %v", err)
	}
	tempDir := t.TempDir()
	originDir := filepath.Join(tempDir, "origin.git")
	workDir := filepath.Join(tempDir, "work")
	fakeBin := filepath.Join(tempDir, "bin")
	ghLog := filepath.Join(tempDir, "gh.log")

	runCommand(t, "", "git", "init", "--bare", originDir)
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("create work dir: %v", err)
	}
	runGit(t, workDir, "init")
	runGit(t, workDir, "checkout", "-b", "main")
	runGit(t, workDir, "config", "user.email", "release-test@example.com")
	runGit(t, workDir, "config", "user.name", "Release Test")
	if err := os.WriteFile(filepath.Join(workDir, "README.md"), []byte("release test\n"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	runGit(t, workDir, "add", "README.md")
	runGit(t, workDir, "commit", "-m", "initial")
	runGit(t, workDir, "remote", "add", "origin", originDir)
	runGit(t, workDir, "push", "-u", "origin", "main")

	writeExecutable(t, filepath.Join(fakeBin, "zig"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(fakeBin, "goreleaser"), `#!/bin/sh
case "$1" in
  build) exit 0 ;;
  release) exit 42 ;;
  *) exit 2 ;;
esac
`)
	writeExecutable(t, filepath.Join(fakeBin, "gh"), `#!/bin/sh
printf '%s\n' "$*" >> "$GH_LOG"
if [ "$1" = "release" ] && [ "$2" = "delete" ]; then
  git push origin ":refs/tags/$3" >/dev/null 2>&1
  exit 0
fi
exit 1
`)

	cmd := exec.Command("bash", scriptPath, "9.9.9")
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GITHUB_TOKEN=test-token",
		"GH_LOG="+ghLog,
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("publish unexpectedly succeeded:\n%s", out)
	}
	got := string(out)
	for _, want := range []string{
		"Error: GoReleaser failed after v9.9.9 was pushed.",
		"Attempting rollback for v9.9.9...",
		"Rolled back v9.9.9.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("publish output missing %q:\n%s", want, got)
		}
	}

	ghOutput, err := os.ReadFile(ghLog)
	if err != nil {
		t.Fatalf("read gh log: %v", err)
	}
	if !strings.Contains(string(ghOutput), "release delete v9.9.9 --cleanup-tag --yes") {
		t.Fatalf("gh rollback command not recorded:\n%s", ghOutput)
	}
	if tags := strings.TrimSpace(string(runGitOutput(t, workDir, "tag", "--list", "v9.9.9"))); tags != "" {
		t.Fatalf("local rollback tag still exists: %s", tags)
	}
	remoteCheck := exec.Command("git", "ls-remote", "--exit-code", "--tags", "origin", "refs/tags/v9.9.9")
	remoteCheck.Dir = workDir
	if out, err := remoteCheck.CombinedOutput(); err == nil {
		t.Fatalf("remote rollback tag still exists:\n%s", out)
	}
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create executable dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	runCommand(t, dir, "git", args...)
}

func runGitOutput(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	return runCommand(t, dir, "git", args...)
}

func runCommand(t *testing.T, dir, name string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return out
}
