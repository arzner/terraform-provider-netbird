package provider

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Test_dataSourceExamples checks that every data source example, which the
// docs copy, uses the data source it documents and declares no resource.
func Test_dataSourceExamples(t *testing.T) {
	dirs, err := filepath.Glob(filepath.Join("..", "..", "examples", "data-sources", "*"))
	if err != nil || len(dirs) == 0 {
		t.Fatalf("no data source examples found: %v", err)
	}
	resourceBlock := regexp.MustCompile(`(?m)^\s*resource\s+"`)
	for _, dir := range dirs {
		name := filepath.Base(dir)
		raw, err := os.ReadFile(filepath.Join(dir, "data-source.tf"))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		example := string(raw)
		if resourceBlock.MatchString(example) {
			t.Errorf("%s: the data source example declares a resource; use data %q", name, name)
		}
		if !strings.Contains(example, `data "`+name+`"`) {
			t.Errorf("%s: the data source example does not use data %q", name, name)
		}
	}
}
