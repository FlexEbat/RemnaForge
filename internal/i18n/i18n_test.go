package i18n

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var verbRE = regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z]`)

func TestLocalesHaveTheSameKeys(t *testing.T) {
	var missingRU, missingEN []string
	for k := range english {
		if _, ok := russian[k]; !ok {
			missingRU = append(missingRU, k)
		}
	}
	for k := range russian {
		if _, ok := english[k]; !ok {
			missingEN = append(missingEN, k)
		}
	}
	sort.Strings(missingRU)
	sort.Strings(missingEN)
	if len(missingRU) > 0 {
		t.Errorf("keys missing in the Russian locale: %v", missingRU)
	}
	if len(missingEN) > 0 {
		t.Errorf("keys missing in the English locale: %v", missingEN)
	}
}

func TestLocaleValuesAreUsable(t *testing.T) {
	for name, m := range map[string]map[string]string{"en": english, "ru": russian} {
		for k, v := range m {
			if strings.TrimSpace(v) == "" {
				t.Errorf("%s: key %s is empty", name, k)
			}
		}
	}
	// A format verb present in one language and absent in the other
	// prints %!s(MISSING) or drops an argument.
	for k, en := range english {
		ru, ok := russian[k]
		if !ok {
			continue
		}
		if a, b := strings.Join(verbRE.FindAllString(en, -1), ","), strings.Join(verbRE.FindAllString(ru, -1), ","); a != b {
			t.Errorf("key %s: format verbs differ, en=%q ru=%q", k, a, b)
		}
	}
}

// A literal i18n.T("KEY") with no entry returns "" and prints an empty
// line to the operator, so every such key must exist.
func TestEveryReferencedKeyExists(t *testing.T) {
	callRE := regexp.MustCompile(`i18n\.T\("([A-Za-z0-9_]+)"\)`)
	root := filepath.Join("..", "..")
	var missing []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, m := range callRE.FindAllStringSubmatch(string(data), -1) {
			if _, ok := english[m[1]]; !ok {
				missing = append(missing, path+": "+m[1])
			}
			if _, ok := russian[m[1]]; !ok {
				missing = append(missing, path+": "+m[1]+" (ru)")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) > 0 {
		t.Errorf("keys used in code but not defined:\n%s", strings.Join(missing, "\n"))
	}
}

// A key that no code refers to is dead text that still has to be
// translated and kept in sync, so each key must appear as a quoted
// literal somewhere outside the locale tables.
func TestNoUnusedKeys(t *testing.T) {
	root := filepath.Join("..", "..")
	var code strings.Builder
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if filepath.Base(path) == "i18n.go" && filepath.Base(filepath.Dir(path)) == "i18n" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		code.Write(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	src := code.String()
	var unused []string
	for k := range english {
		if !strings.Contains(src, `"`+k+`"`) {
			unused = append(unused, k)
		}
	}
	sort.Strings(unused)
	if len(unused) > 0 {
		t.Errorf("keys defined but never used in code:\n%s", strings.Join(unused, "\n"))
	}
}
