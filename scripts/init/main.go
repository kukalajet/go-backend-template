// Command init turns a fresh copy of the template into a project. It runs once
// through `just init`, after gonew has rewritten go.mod and Go imports, and then
// deletes itself.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	templateModule = "github.com/kukalajet/go-backend-template"
	templateName   = "go-backend-template"
	templateDB     = "go_template"
	markerBegin    = "template-only:begin"
	markerEnd      = "template-only:end"
)

var (
	majorVersion = regexp.MustCompile(`^v[0-9]+$`)
	notDBSafe    = regexp.MustCompile(`[^a-z0-9_]`)

	// templateOnlyFiles are deleted rather than rewritten.
	templateOnlyFiles = []string{"LICENSE", "SPEC.md", "scripts/init", "scripts/test-init.sh"}
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "init:", err)
		os.Exit(1)
	}
}

func run() error {
	module, err := currentModule()
	if err != nil {
		return err
	}
	if module == templateModule {
		return errors.New("go.mod still has the template's module path; create the project with gonew first")
	}
	if !hasMarkers("justfile") {
		return errors.New("this project is already initialized")
	}

	name := projectName(module)
	replacer := strings.NewReplacer(
		templateModule, module,
		templateName, name,
		templateDB, dbIdentifier(name),
	)
	if err := rewriteFiles(replacer); err != nil {
		return err
	}
	for _, file := range templateOnlyFiles {
		if err := os.RemoveAll(filepath.FromSlash(file)); err != nil {
			return fmt.Errorf("delete %s: %w", file, err)
		}
	}

	fmt.Printf("Initialized %s as %s\n", module, name)

	return nil
}

func currentModule() (string, error) {
	out, err := exec.CommandContext(context.Background(), "go", "list", "-m").Output()
	if err != nil {
		return "", fmt.Errorf("read module path: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

func hasMarkers(file string) bool {
	content, err := os.ReadFile(file) //nolint:gosec // file is a fixed name in this project

	return err == nil && bytes.Contains(content, []byte(markerBegin))
}

// projectName returns the module path's last element, ignoring a major-version
// suffix such as /v2.
func projectName(module string) string {
	base := path.Base(module)
	if majorVersion.MatchString(base) {
		base = path.Base(path.Dir(module))
	}

	return strings.ToLower(base)
}

// dbIdentifier converts a project name to a safe, unquoted Postgres identifier.
func dbIdentifier(name string) string {
	return notDBSafe.ReplaceAllString(strings.ToLower(name), "_")
}

func rewriteFiles(replacer *strings.Replacer) error {
	return filepath.WalkDir(".", func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skipDir(filepath.ToSlash(p)) {
				return filepath.SkipDir
			}

			return nil
		}
		if isTemplateOnly(filepath.ToSlash(p)) {
			return nil
		}

		return rewriteFile(p, entry, replacer)
	})
}

func skipDir(p string) bool {
	return p == ".git" || p == "bin" || p == "scripts/init"
}

func isTemplateOnly(p string) bool {
	for _, file := range templateOnlyFiles {
		if p == file {
			return true
		}
	}

	return false
}

func rewriteFile(p string, entry fs.DirEntry, replacer *strings.Replacer) error {
	original, err := os.ReadFile(p) //nolint:gosec // p comes from walking this project's own tree
	if err != nil {
		return fmt.Errorf("read %s: %w", p, err)
	}
	updated := replacer.Replace(removeTemplateOnlyBlocks(string(original)))
	if updated == string(original) {
		return nil
	}
	info, err := entry.Info()
	if err != nil {
		return fmt.Errorf("stat %s: %w", p, err)
	}
	//nolint:gosec // p comes from walking this project's own tree
	if err := os.WriteFile(p, []byte(updated), info.Mode().Perm()); err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}

	return nil
}

// removeTemplateOnlyBlocks drops every line from a begin marker through its end
// marker.
func removeTemplateOnlyBlocks(content string) string {
	if !strings.Contains(content, markerBegin) {
		return content
	}

	var kept strings.Builder
	inBlock := false
	for line := range strings.SplitAfterSeq(content, "\n") {
		switch {
		case strings.Contains(line, markerBegin):
			inBlock = true
		case strings.Contains(line, markerEnd):
			inBlock = false
		case !inBlock:
			kept.WriteString(line)
		}
	}

	return kept.String()
}
