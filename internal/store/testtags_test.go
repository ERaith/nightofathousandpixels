//go:build !integration

// The half of the tag contract that a bare `go test ./...` can check.
//
// The other half lives in the Makefile: `make test-integration` refuses to run
// if -tags=integration selects no tests. Between them, neither an empty
// integration suite nor a database test that quietly joined the unit suite can
// stay green.

package store_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// dbTestMarker is how a test file says "I need Postgres". Every such file has
// to carry //go:build integration, or `go test ./...` either skips it (silent
// green, which is nap-gn1) or dials a database nothing started (red for
// everyone without Docker). There is no third option, which is why this is
// checked rather than documented.
const dbTestMarker = "TEST_DATABASE_URL"

// selfPath is this file, which mentions the marker while not being a database
// test -- so it has to exempt itself or it reports its own guard as a defect.
// Taken from the compiler rather than hardcoded, so renaming the file does not
// silently turn the exemption into a hole somewhere else.
func selfPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not name this file")
	}
	return file
}

// moduleRoot walks up from this package to the directory holding go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's working directory")
		}
		dir = parent
	}
}

// sameFile compares two paths by identity rather than by string, so a symlink
// or a relative build path cannot defeat the exemption above.
func sameFile(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(fa, fb)
}

// TestEveryDatabaseTestCarriesTheIntegrationTag is the guard that keeps the
// contract true as the tree grows. It also prints where the database tests
// are, so `go test ./... -v` says out loud what it did not run instead of
// implying it ran everything.
func TestEveryDatabaseTestCarriesTheIntegrationTag(t *testing.T) {
	root := moduleRoot(t)
	self := selfPath(t)

	var tagged, untagged []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "archive", "worktrees", "node_modules", "tmp", "bin", ".beads", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") || sameFile(path, self) {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !strings.Contains(string(src), dbTestMarker) {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		// The constraint has to be in the file's own header, before the
		// package clause; anywhere else Go ignores it.
		header, _, _ := strings.Cut(string(src), "\npackage ")
		if strings.Contains(header, "//go:build integration") {
			tagged = append(tagged, rel)
		} else {
			untagged = append(untagged, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	// A guard that found nothing to guard would pass forever. The three
	// known files are the floor, not the expectation.
	if len(tagged)+len(untagged) == 0 {
		t.Fatalf("found no test file mentioning %s anywhere under %s; "+
			"either the database tests were deleted or this guard stopped looking in the right place",
			dbTestMarker, root)
	}

	for _, f := range untagged {
		t.Errorf("%s reads %s but has no `//go:build integration` header.\n"+
			"Without the tag, `go test ./...` either skips it silently or needs a database it did not start.",
			f, dbTestMarker)
	}

	t.Logf("%d database test file(s) held back by -tags=integration; run them with `make test-integration`:", len(tagged))
	for _, f := range tagged {
		t.Logf("    %s", f)
	}
}

// TestTheDSNCheckIsFatalNotASkip closes the other half of the same hole.
//
// The tag stops `go test ./...` from compiling these files, but a bare
// `go test -tags=integration ./...` with no database is still possible, and if
// the DSN check skips there then the integration suite reports `ok` having run
// nothing - the original nap-gn1 failure, reached by a different route.
// `make test-integration` always supplies the DSN, so this is the case make
// cannot protect and a static check can.
//
// Skips for a genuinely optional dependency are fine and deliberately not
// matched: internal/store/integrity skips TestDumpRestoreRoundTrip when
// pg_dump is not on PATH. Only the DSN lookup itself has to be fatal.
func TestTheDSNCheckIsFatalNotASkip(t *testing.T) {
	root := moduleRoot(t)
	self := selfPath(t)
	lookup := "os.Getenv(\"" + dbTestMarker + "\")"

	checked := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "archive", "worktrees", "node_modules", "tmp", "bin", ".beads", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") || sameFile(path, self) {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines := strings.Split(string(src), "\n")
		rel, _ := filepath.Rel(root, path)
		for i, line := range lines {
			if !strings.Contains(line, lookup) {
				continue
			}
			checked++
			// The handling of an empty DSN is the next few lines. Six is
			// comfortably more than the three the guard actually occupies
			// and well short of the next statement in any of these files.
			end := min(i+7, len(lines))
			for j := i; j < end; j++ {
				if strings.Contains(lines[j], "t.Skip") {
					t.Errorf("%s:%d SKIPS when %s is empty.\n"+
						"Under -tags=integration the database is the point, so an absent DSN is a "+
						"broken invocation, not a reason to report ok. Use t.Fatal.",
						rel, j+1, dbTestMarker)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if checked == 0 {
		t.Fatalf("no file reads %s; this guard is looking for the wrong thing", lookup)
	}
	if !t.Failed() {
		t.Logf("checked %d DSN lookup(s); all fatal on an empty value", checked)
	}
}
