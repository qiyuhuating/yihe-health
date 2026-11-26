package runtimeaudit

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The backend must compile and ship without a JVM or Java build tooling.
// Run this check on the checked-out preview tree as part of go test ./....
func TestGoBackendContainsNoJavaRuntimeArtifacts(t *testing.T) {
	backend := filepath.Clean(filepath.Join("..", ".."))
	forbidden := map[string]bool{
		"pom.xml": true, "mvnw": true, "mvnw.cmd": true,
		"gradlew": true, "gradlew.bat": true,
		"build.gradle": true, "build.gradle.kts": true,
		"settings.gradle": true, "settings.gradle.kts": true,
		"gradle.properties": true,
	}
	extensions := map[string]bool{
		".java": true, ".class": true, ".jar": true, ".war": true,
		".jsp": true, ".groovy": true, ".kt": true, ".kts": true,
	}
	err := filepath.WalkDir(backend, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := strings.ToLower(entry.Name())
		if (entry.IsDir() && (name == ".mvn" || name == ".gradle")) ||
			(!entry.IsDir() && (forbidden[name] || extensions[strings.ToLower(filepath.Ext(name))])) {
			t.Errorf("Java/JVM artifact reintroduced into Go backend: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDeploymentOnlyRunsGoBackend(t *testing.T) {
	// This path is checked only in the full repository checkout. A backend-only
	// source package can still run its Go tests without the deployment folder.
	path := filepath.Join("..", "..", "..", "deploy", "Dockerfile")
	dockerfile, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		t.Skip("deployment tree is not present in this source package")
	}
	if err != nil {
		t.Fatal(err)
	}
	source := string(dockerfile)
	for _, required := range []string{
		"FROM golang:", "CGO_ENABLED=0 go build", "ENTRYPOINT [\"/app/yihe\"]",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("Go-only deployment requirement absent: %q", required)
		}
	}
	// Exclude Java runtime/base images and Java/Maven command invocations,
	// while allowing written historical references in documentation.
	pattern := regexp.MustCompile(`(?im)\b(?:FROM\s+[^\n]*(?:openjdk|eclipse-temurin|maven|gradle)|(?:RUN|ENTRYPOINT|CMD)\s+[^\n]*(?:java\s+-jar|javac\s|mvn\s|gradle\s))`)
	if pattern.MatchString(source) {
		t.Fatal("container build is trying to invoke a Java toolchain")
	}
}
