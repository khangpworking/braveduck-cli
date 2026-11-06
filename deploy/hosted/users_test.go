//go:build !windows

package hosted_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const existingUser = "users:\n  alex1doe:\n    disabled: false\n    displayname: 'Alex One'\n    email: 'one@example.test'\n    password: '$argon2id$dummy-existing'\n"
const dottedUser = "  alex.doe:\n    disabled: false\n    displayname: 'Alex Dot'\n    email: 'dot@example.test'\n    password: '$argon2id$dummy-test-only'\n"

func userScriptFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"add-user.sh", "remove-user.sh"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{
		"users.yml": existingUser,
		"authelia":  "#!/bin/sh\nprintf '%s\\n' 'Digest: $argon2id$dummy-test-only'\n",
		"compose":   "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$COMPOSE_LOG\"\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func runUserScript(dir, script string, args ...string) ([]byte, error) {
	cmd := exec.Command("sh", append([]string{filepath.Join(dir, script)}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME="+dir, "TYPESAFE_API_KEY=",
		"USERS_DB="+filepath.Join(dir, "users.yml"), "AUTHELIA="+filepath.Join(dir, "authelia"),
		"COMPOSE="+filepath.Join(dir, "compose"), "COMPOSE_LOG="+filepath.Join(dir, "compose.log"),
		"BRAVEDUCK_NEW_PASSWORD=dummy-password-for-tests")
	return cmd.CombinedOutput()
}

func checkUsers(t *testing.T, dir, want string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(dir, "users.yml"))
	if err != nil || string(got) != want {
		t.Fatalf("users database = %q, %v; want %q", got, err, want)
	}
}

func TestAddUserMatchesLiteralName(t *testing.T) {
	dir := userScriptFixture(t)
	if out, err := runUserScript(dir, "add-user.sh", "alex.doe", "Alex Dot", "dot@example.test"); err != nil {
		t.Fatalf("add distinct dotted user: %v\n%s", err, out)
	}
	checkUsers(t, dir, existingUser+dottedUser)
	if out, err := runUserScript(dir, "add-user.sh", "alex.doe", "Alex Dot", "dot@example.test"); err == nil {
		t.Fatalf("duplicate user accepted: %s", out)
	}
	checkUsers(t, dir, existingUser+dottedUser)
}

func TestRemoveUserMatchesLiteralName(t *testing.T) {
	dir := userScriptFixture(t)
	if out, err := runUserScript(dir, "remove-user.sh", "alex.doe"); err == nil {
		t.Errorf("absent dotted user reported removed: %s", out)
	}
	checkUsers(t, dir, existingUser)
	if _, err := os.Stat(filepath.Join(dir, "compose.log")); !os.IsNotExist(err) {
		t.Fatalf("removing an absent user invoked compose: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "users.yml"), []byte(existingUser+dottedUser), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := runUserScript(dir, "remove-user.sh", "alex.doe"); err != nil {
		t.Fatalf("remove existing dotted user: %v\n%s", err, out)
	}
	checkUsers(t, dir, existingUser)
	if _, err := os.Stat(filepath.Join(dir, "compose.log")); err != nil {
		t.Fatalf("removing an existing user did not invoke compose: %v", err)
	}
}
