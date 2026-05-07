package ops_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeployScriptsEnableCGO(t *testing.T) {
	t.Parallel()

	for _, script := range []string{
		"deploy_man_v2.sh",
		filepath.Join("ops", "deploy.sh"),
	} {
		content, err := os.ReadFile(filepath.Join("..", script))
		if err != nil {
			t.Fatalf("read %s: %v", script, err)
		}

		text := string(content)
		if strings.Contains(text, "CGO_ENABLED=0") {
			t.Fatalf("%s still disables cgo", script)
		}
		if !strings.Contains(text, "CGO_ENABLED=1") {
			t.Fatalf("%s does not enable cgo", script)
		}
	}
}

func TestDeployScriptsVerifyZmqSupport(t *testing.T) {
	t.Parallel()

	for _, script := range []string{
		"deploy_man_v2.sh",
		filepath.Join("ops", "deploy.sh"),
	} {
		content, err := os.ReadFile(filepath.Join("..", script))
		if err != nil {
			t.Fatalf("read %s: %v", script, err)
		}

		text := string(content)
		if !strings.Contains(text, "zmq_stub.go") {
			t.Fatalf("%s does not reject stub-only binaries", script)
		}
		if !strings.Contains(text, "github.com/pebbe/zmq4") {
			t.Fatalf("%s does not verify zmq support in the built binary", script)
		}
	}
}

func TestDeployScriptsBuildPackageEntrypoint(t *testing.T) {
	t.Parallel()

	for _, script := range []string{
		"deploy_man_v2.sh",
		filepath.Join("ops", "deploy.sh"),
	} {
		content, err := os.ReadFile(filepath.Join("..", script))
		if err != nil {
			t.Fatalf("read %s: %v", script, err)
		}

		if strings.Contains(string(content), " app.go") {
			t.Fatalf("%s builds app.go directly; deploy must build the whole main package", script)
		}
	}
}
