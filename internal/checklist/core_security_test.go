package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type coreSecurityIndex struct {
	Sources []coreSecuritySource `json:"sources"`
}
type coreSecuritySource struct {
	RFC      int                   `json:"rfc"`
	URL      string                `json:"url"`
	SHA256   string                `json:"sha256"`
	Sections []coreSecuritySection `json:"sections"`
}
type coreSecuritySection struct {
	Section      string   `json:"section"`
	Title        string   `json:"title"`
	SHA256       string   `json:"source_section_sha256"`
	Requirements []string `json:"requirements"`
	Note         string   `json:"note"`
}

// Independent source/section pins prevent removing reviewed security prose or
// changing its provenance in both the index and rendered crosswalk unnoticed.
// They do not establish semantic completeness of individual sentence mappings.
var coreSecurityPins = map[int]struct{ sha, sections string }{
	3501: {"4437974dde85859f2391f32215295e7b20ce157c3bd2adbe7e5b443e990f15cf", "8 10 11 11.1 11.2 12"},
	9051: {"acf52a50eb748ea21b5ffdcb31c761d3fbba6242d53d4f53c2555de039c5c3cf", "8 10 11 11.1 11.2 11.3 11.4 11.5 11.6 11.7 12 12.1 12.2 12.3 12.4"},
}

var coreSecuritySectionPins = map[string]string{
	"3501/8":    "b6cd0b31d85ef727b7da7c5b1a2372b1a8a1946e4ca6a151e92525b5bb9228c1",
	"3501/10":   "6026eb0faee6bce3bd1d4ae2a7f84ee8822563b6890d060ff00f4cc92704808c",
	"3501/11":   "170a435c09910db1f34b54b088ed450d856d4714815bdce4c0614a25668a6b5c",
	"3501/11.1": "ed0afa92153aa258ee6bdf763e02a7d356f8329b9cc39158b0c450b1d7c1dd82",
	"3501/11.2": "c68d0a43b61cabf0011531463529a7191af7b30c0c142934de72c414cdf71c93",
	"3501/12":   "5ed9641979fa672dbc4c4ec339f6ad3e5da0bc104c8e1f69e93beda9ff21fe59",
	"9051/8":    "c35c9d875c79b4808f34135fb24c8bfc9c4cb44c3d8fccfbf6bb2b7b7883f424",
	"9051/10":   "087077477514f0596dadf700b518118a1d11935089e4d68b1d08d308d83f57a4",
	"9051/11":   "0fc0adf6d8a2dd68ae4aa150a0d7b408ca99fcf39e03d714d25fc20abb4487ac",
	"9051/11.1": "bc050c01638b3b3910560b8ea4f5e4db6f01a9e8cd95dcc493adb172b9dc9b09",
	"9051/11.2": "09b6651ac8b4ba83898937d24c3c9aaf68e9c583021a415a8c6b71b2fdc82cbd",
	"9051/11.3": "ab41b5880d9808dc0cdcc01fa7fe0b928c443cde996c59a31504a0b0eef319af",
	"9051/11.4": "f06dceb38f6a5ae2ffec3a7ab52237421de7a0a4b4c8c496d10cdaaf56ea75b5",
	"9051/11.5": "5b5d903f843b5a63b67c8aac5ece98b4a15d2d2d8d7fc32233733ae34c7752f2",
	"9051/11.6": "1db6c12d9178c27e10c214ecc9a95ccd1b6faa53dba03d41c85bbf6a612d305a",
	"9051/11.7": "4e3d0a2652f55de1a45894509cac78f8d22cebd14289f5c5045c7346c1294e04",
	"9051/12":   "3794c3848cd34eb00832ae9ffdf1e179c7baee01ead32142e5034551dcbcca9a",
	"9051/12.1": "49be10fa83971cbad2a37a12634588e113a57864ad2ee246cf121be79158e4b4",
	"9051/12.2": "7fb907e8303093860d0d18dff133eeb3a5be166b04d6147ba58c3343180351ba",
	"9051/12.3": "1177e960693c1ee6085174892e6dbb3fe0efdf57a15747ad18e293de4492cbf6",
	"9051/12.4": "42ce5a8f0f4828e82d65488a0d1fa65fdf4128bc35a732cbc1aef93bbe95c194",
}

func validateCoreSecurityIndex(index coreSecurityIndex, items map[string]*item, doc string) error {
	if len(index.Sources) != len(coreSecurityPins) {
		return fmt.Errorf("source count: got%d want%d", len(index.Sources), len(coreSecurityPins))
	}
	seenRFC := map[int]bool{}
	hex := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for _, source := range index.Sources {
		pin, ok := coreSecurityPins[source.RFC]
		if !ok || seenRFC[source.RFC] {
			return fmt.Errorf("unknown or duplicate RFC%d", source.RFC)
		}
		seenRFC[source.RFC] = true
		if source.SHA256 != pin.sha || source.URL != fmt.Sprintf("https://www.rfc-editor.org/rfc/rfc%d.txt", source.RFC) {
			return fmt.Errorf("source pin mismatch RFC%d", source.RFC)
		}
		want := strings.Fields(pin.sections)
		if len(source.Sections) != len(want) {
			return fmt.Errorf("section count RFC%d", source.RFC)
		}
		for i, section := range source.Sections {
			if section.Section != want[i] {
				return fmt.Errorf("missing, reordered or duplicate section RFC%d/%s", source.RFC, want[i])
			}
			if section.Title == "" || strings.TrimSpace(section.Note) == "" || !hex.MatchString(section.SHA256) || section.SHA256 != coreSecuritySectionPins[fmt.Sprintf("%d/%s", source.RFC, section.Section)] {
				return fmt.Errorf("incomplete disposition RFC%d/%s", source.RFC, section.Section)
			}
			if len(section.Requirements) == 0 && section.Section != "10" {
				return fmt.Errorf("missing requirement disposition RFC%d/%s", source.RFC, section.Section)
			}
			refs := make([]string, 0, len(section.Requirements))
			seenID := map[string]bool{}
			for _, id := range section.Requirements {
				if items[id] == nil || seenID[id] {
					return fmt.Errorf("unknown or duplicate requirement %s", id)
				}
				seenID[id] = true
				refs = append(refs, "`"+id+"`")
			}
			rendered := strings.Join(refs, ", ")
			if len(refs) == 0 {
				rendered = "—"
			}
			row := fmt.Sprintf("| RFC%d §%s | %s | %s |", source.RFC, section.Section, rendered, section.Note)
			if strings.Count(doc, row) != 1 {
				return fmt.Errorf("crosswalk drift RFC%d/%s", source.RFC, section.Section)
			}
		}
	}
	return nil
}

func loadCoreSecurityAudit(t *testing.T) (coreSecurityIndex, map[string]*item, string) {
	t.Helper()
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join("testdata", "core-security.json"))
	if err != nil {
		t.Fatal(err)
	}
	var index coreSecurityIndex
	if err := json.Unmarshal(data, &index); err != nil {
		t.Fatal(err)
	}
	items, err := parseChecklist(filepath.Join(root, "CHECKLIST.md"), func(format string, args ...any) { t.Errorf(format, args...) })
	if err != nil {
		t.Fatal(err)
	}
	return index, items, readFile(t, root, "RFC-SECURITY.md")
}

func TestCoreSecurityInventory(t *testing.T) {
	index, items, doc := loadCoreSecurityAudit(t)
	if err := validateCoreSecurityIndex(index, items, doc); err != nil {
		t.Fatal(err)
	}
}

// A lost section, duplicated source, stale source pin, invented requirement or
// drift between the machine index and review document must fail closed.
func TestCoreSecurityInventoryMutations(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*coreSecurityIndex, *string)
		want   string
	}{
		{"lost RFC", func(x *coreSecurityIndex, _ *string) { x.Sources = x.Sources[1:] }, "source count"},
		{"duplicate RFC", func(x *coreSecurityIndex, _ *string) { x.Sources[1] = x.Sources[0] }, "duplicate RFC"},
		{"lost section", func(x *coreSecurityIndex, _ *string) { x.Sources[0].Sections = x.Sources[0].Sections[1:] }, "section count"},
		{"duplicate section", func(x *coreSecurityIndex, _ *string) { x.Sources[0].Sections[1] = x.Sources[0].Sections[0] }, "duplicate section"},
		{"source drift", func(x *coreSecurityIndex, _ *string) { x.Sources[0].SHA256 = strings.Repeat("0", 64) }, "source pin mismatch"},
		{"unknown requirement", func(x *coreSecurityIndex, _ *string) {
			x.Sources[0].Sections[0].Requirements = []string{"IMAP-INVENTED-999"}
		}, "unknown or duplicate requirement"},
		{"empty disposition", func(x *coreSecurityIndex, _ *string) { x.Sources[0].Sections[0].Note = "" }, "incomplete disposition"},

		{"section source drift", func(x *coreSecurityIndex, _ *string) { x.Sources[0].Sections[0].SHA256 = strings.Repeat("0", 64) }, "incomplete disposition"},
		{"lost requirement disposition", func(x *coreSecurityIndex, _ *string) { x.Sources[0].Sections[0].Requirements = nil }, "missing requirement disposition"},
		{"duplicate requirement", func(x *coreSecurityIndex, _ *string) {
			x.Sources[0].Sections[0].Requirements = []string{"IMAP-SECURITY-12", "IMAP-SECURITY-12"}
		}, "unknown or duplicate requirement"},
		{"source URL drift", func(x *coreSecurityIndex, _ *string) { x.Sources[0].URL = "https://example.invalid/rfc3501.txt" }, "source pin mismatch"},
		{"document drift", func(_ *coreSecurityIndex, doc *string) {
			*doc = strings.Replace(*doc, "| RFC3501 §8 |", "| RFC3501 §999 |", 1)
		}, "crosswalk drift"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			index, items, doc := loadCoreSecurityAudit(t)
			tc.mutate(&index, &doc)
			err := validateCoreSecurityIndex(index, items, doc)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want %q", err, tc.want)
			}
		})
	}
}
