package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIMAPChecklist checks the real checklist against internal/rfctest, then
// proves that breaking any row's mapping from either side fails the check.
func TestIMAPChecklist(t *testing.T) {
	root := filepath.Join("..", "..")
	rfctest := filepath.Join(root, "internal", "rfctest")
	var out bytes.Buffer
	if code := run([]string{"-file", filepath.Join(root, "CHECKLIST.md"), rfctest}, &out, &out); code != 0 {
		t.Fatalf("real checklist: exit %d:\n%s", code, &out)
	}

	data, err := os.ReadFile(filepath.Join(root, "CHECKLIST.md"))
	if err != nil {
		t.Fatal(err)
	}
	checklist := string(data)
	files, err := filepath.Glob(filepath.Join(rfctest, "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	original := map[string]string{}
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		original[filepath.Base(path)] = string(src)
	}
	items, err := parseChecklist(filepath.Join(root, "CHECKLIST.md"), func(string, ...any) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("real checklist has no items")
	}

	for id, it := range items {
		if it.na {
			continue
		}
		row := ""
		for _, line := range strings.Split(checklist, "\n") {
			if strings.HasPrefix(line, "| "+id+" |") {
				row = line
			}
		}
		test := it.tests[0]
		file := ""
		for name, src := range original {
			if strings.Contains(src, "func "+test+"(") {
				file = name
			}
		}
		if row == "" || file == "" {
			t.Fatalf("%s: row %q, test %s in %q", id, row, test, file)
		}
		source := original[file]
		emptied := strings.Replace(row, "| "+strings.Join(it.tests, ", ")+" |", "| |", 1)
		for _, mutation := range []struct{ name, checklist, source, want string }{
			{"missing-test", checklist, strings.Replace(source, "func "+test+"(", "func missingProof(", 1), id + " lists " + test + ", which is not a test"},
			{"missing-row", strings.Replace(checklist, row+"\n", "", 1), source, test + " names " + id + ", which is not in"},
			{"missing-annotation", checklist, unname(source, test, id), test + " does not name " + id},
			{"empty-mapping", strings.Replace(checklist, row, emptied, 1), source, id + " names no test"},
		} {
			t.Run(id+"/"+mutation.name, func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, "CHECKLIST.md")
				if err := os.WriteFile(path, []byte(mutation.checklist), 0o644); err != nil {
					t.Fatal(err)
				}
				for name, content := range original {
					if name == file {
						content = mutation.source
					}
					if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				var out bytes.Buffer
				if code := run([]string{"-file", path, dir}, &out, &out); code != 1 || !strings.Contains(out.String(), mutation.want) {
					t.Fatalf("exit %d, want 1 and %q:\n%s", code, mutation.want, &out)
				}
			})
		}
	}
}

// unname removes id from the checklist line of test's doc comment in source.
func unname(source, test, id string) string {
	fn := strings.Index(source, "func "+test+"(")
	line := strings.LastIndex(source[:fn], "// checklist:")
	end := line + strings.Index(source[line:], "\n")
	return source[:line] + strings.Replace(source[line:end], " "+id, "", 1) + source[end:]
}
