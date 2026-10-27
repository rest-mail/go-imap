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

type coreResponseIndex struct {
	Sources []coreResponseSource `json:"sources"`
}
type coreResponseSource struct {
	RFC      int                   `json:"rfc"`
	URL      string                `json:"url"`
	SHA256   string                `json:"sha256"`
	Sections []coreResponseSection `json:"sections"`
	Codes    []coreResponseCode    `json:"codes"`
}
type coreResponseCode struct {
	Code         string   `json:"code"`
	Requirements []string `json:"requirements"`
	Note         string   `json:"note"`
}
type coreResponseSection struct {
	Section      string   `json:"section"`
	Title        string   `json:"title"`
	SHA256       string   `json:"source_section_sha256"`
	Requirements []string `json:"requirements"`
	Note         string   `json:"note"`
}

// Independent source/section pins prevent removing reviewed response prose or
// changing its provenance in both the index and rendered crosswalk unnoticed.
// They do not establish semantic completeness of individual sentence mappings.
var coreResponsePins = map[int]struct{ sha, sections string }{
	3501: {"4437974dde85859f2391f32215295e7b20ce157c3bd2adbe7e5b443e990f15cf", "7 7.1 7.1.1 7.1.2 7.1.3 7.1.4 7.1.5 7.2 7.2.1 7.2.2 7.2.3 7.2.4 7.2.5 7.2.6 7.3 7.3.1 7.3.2 7.4 7.4.1 7.4.2 7.5"},
	9051: {"acf52a50eb748ea21b5ffdcb31c761d3fbba6242d53d4f53c2555de039c5c3cf", "7 7.1 7.1.1 7.1.2 7.1.3 7.1.4 7.1.5 7.2 7.2.1 7.2.2 7.3 7.3.1 7.3.2 7.3.3 7.3.4 7.3.5 7.4 7.4.1 7.5 7.5.1 7.5.2 7.6"},
}

var coreResponseSectionPins = map[string]string{
	"3501/7":     "72dc8c2f363b2f8de306c9135e30ad4fd89dfea312ea51c41ac186c97e76fbd8",
	"3501/7.1":   "20a9f6775c71556aff2eb8212f4e38a23ea11e2d59a25de0640d86c865e0b160",
	"3501/7.1.1": "3e95c07a9b3e94b154ea22595dae1a3f30cb0771e46c328927fdf948d1c5f696",
	"3501/7.1.2": "04ee6473d45977045383bb88a2e6a4db487275d659c8d5d83f5f47b5e9db7165",
	"3501/7.1.3": "922ac3ed1fd7860891a694efbe79f05001d896f54591343394360dc5468cd4f3",
	"3501/7.1.4": "cae97ef6e3e272099f284b8a3e467b2875d3cb6851ad00ba3fda9c269e2542d4",
	"3501/7.1.5": "15501659125564b3666d74f4b38da467045ce70fa580a9dbb75799702ddbf8ec",
	"3501/7.2":   "11bed1c45f9e133027fee57c9f1a1c35fcc552ee952879b8d81dc3852e6c1156",
	"3501/7.2.1": "7e0890db23920f1b486b24aeead51974fd891c581d3a8d7817bf82be7621fb23",
	"3501/7.2.2": "8ae477a8615e7679189ef66c1c9694e5df869973bef338f5e89c457cd23ad0cb",
	"3501/7.2.3": "929aaa87a95cc627036967b0ce55347a4d878e40552653997dbc335e994a47d1",
	"3501/7.2.4": "b563d2e0fd245b8d9cb207dcd3decf0906ae6f4ab35ba25e8d46523fbb0153d8",
	"3501/7.2.5": "0ffc8bd690467484ff6efdf6dea8fdd2dc49780d42593ea9878b1cbe44335507",
	"3501/7.2.6": "e533f43702d9bbb207e9aae0e50a6a95c72ca1550dccec9d258bb75be9d86830",
	"3501/7.3":   "d5016c6d0ce353926227745a8292ff456ac4e7dfeca33a2e1815ef881fd6afb7",
	"3501/7.3.1": "d88d0449145ce07714c4f7451a3d963b1ad4c7f3e77889351c6b195f26160c93",
	"3501/7.3.2": "3b436953bf560784bb97be62fe1d9e3b5ffa069afbc05642be6962ef2a34b08c",
	"3501/7.4":   "0ed4d2bff537b166fc43ffd38f11ca96667bf43b214d569a73d9827c987a02c9",
	"3501/7.4.1": "a9b677e13209fc8557700d3e453f0328ea99aaad90c725a33fc3ca9ee41d290f",
	"3501/7.4.2": "f679d25e36d1460dcf58adbb43687853acb92c77366c652de73ab9128e96e234",
	"3501/7.5":   "2c27d91fd629b4d80d403fa84bf8b2bebf2c25a3b7f04d550cb038e56c147a2c",
	"9051/7":     "94c88e293e03a214b122e75846e4a77e2e6f2ee185ceb2dbc53ea708ee5c82c9",
	"9051/7.1":   "f842cd0bae8f47da261cf116e15ba1f4c4df4582df0a142365bf63adf062ebe4",
	"9051/7.1.1": "a61e25984a8e87f2dd6e5f3f873d5b07065f770cd5edc1ef272bccd58b27bfbb",
	"9051/7.1.2": "be53c6ff8d1f63f1f8206a1a1ee51a3e63f23ce7b19f8bc716f5542210cbcbcd",
	"9051/7.1.3": "c486caff3f96e5255dc559f398c1e395867cf4b2c5c04e3d3a75a0c3ade7aa15",
	"9051/7.1.4": "5f6ea9dd5d5723bd6372919f4981b01906152539f660da3aacb337d237e67449",
	"9051/7.1.5": "79c1273a7e8828920172b8a7113c7df2b8b1c4a78c30b0fa721559fda1e40295",
	"9051/7.2":   "0d92c997ba4a170532181a95156cc0878f38463849c372329a0c2a0a572be355",
	"9051/7.2.1": "a55ca17175c06d6af7c4eabb1e4f721be5b852eb262dca37a57fc65faa0dd2ba",
	"9051/7.2.2": "de02c75822d3e3f43ffee5962117c620a6f235df7ead7527e304d8f5bc76eb22",
	"9051/7.3":   "c82b9f8f50c490793487960cf3444b7e746ac55cbdd4e8ffef65e6415d59bf11",
	"9051/7.3.1": "ad09ae36520ca4f2e14ea87286f65b3dc01cac6a0b57d1820daa53cfc7b331c2",
	"9051/7.3.2": "1b9c93f9ba8e8d9e6925930e7140abe437ba12f147cdec2767b702795c00c462",
	"9051/7.3.3": "cab7246ff634aba20626c57c8b993fcfce1170214c27584e8ddad01052d81201",
	"9051/7.3.4": "cd38f2e3d981a5cc48e4ee48b4ab1cc158ef687e80e13eabec8e6fa293959e4b",
	"9051/7.3.5": "87ed4e5f1589f8b50d8f9ce0b4dc936a09bd91c158e142a2d5b019878b571ebb",
	"9051/7.4":   "d6f91e01e9d74cfc31a5e8537084be90e14c8f78a9a7fdc415925b498419971e",
	"9051/7.4.1": "14249bf141b161b2f52e4a6d428b0a54ec7f7816d8a0df9b6c46c9d531a2f440",
	"9051/7.5":   "286b7b761de6a1257f728b5cb80de2410cf2633f75b14e2ca18ad62701c15b8a",
	"9051/7.5.1": "5485c5e00008bc97dce404471073eaf28ba8c16b1aaa0ba2f1ec2718fa8b49fc",
	"9051/7.5.2": "5108afcab26ef6b22e9f040eaab2721b8e90ab30cccd1cb1162d1708e916f698",
	"9051/7.6":   "dc5d53dfb04c4eb46a0aa02b8b15df77e49595d6951707f793e644589b9e8382",
}

var coreResponseCodePins = map[int]string{
	3501: "ALERT BADCHARSET CAPABILITY PARSE PERMANENTFLAGS READ-ONLY READ-WRITE TRYCREATE UIDNEXT UIDVALIDITY UNSEEN",
	9051: "ALERT ALREADYEXISTS APPENDUID AUTHENTICATIONFAILED AUTHORIZATIONFAILED BADCHARSET CANNOT CAPABILITY CLIENTBUG CLOSED CONTACTADMIN COPYUID CORRUPTION EXPIRED EXPUNGEISSUED HASCHILDREN INUSE LIMIT NONEXISTENT NOPERM OVERQUOTA PARSE PERMANENTFLAGS PRIVACYREQUIRED READ-ONLY READ-WRITE SERVERBUG TRYCREATE UIDNEXT UIDNOTSTICKY UIDVALIDITY UNAVAILABLE UNKNOWN-CTE",
}

func validateCoreResponseIndex(index coreResponseIndex, items map[string]*item, doc string) error {
	if len(index.Sources) != len(coreResponsePins) {
		return fmt.Errorf("source count: got%d want%d", len(index.Sources), len(coreResponsePins))
	}
	seenRFC := map[int]bool{}
	hex := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for _, source := range index.Sources {
		pin, ok := coreResponsePins[source.RFC]
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
			if section.Title == "" || strings.TrimSpace(section.Note) == "" || !hex.MatchString(section.SHA256) || section.SHA256 != coreResponseSectionPins[fmt.Sprintf("%d/%s", source.RFC, section.Section)] {
				return fmt.Errorf("incomplete disposition RFC%d/%s", source.RFC, section.Section)
			}
			if len(section.Requirements) == 0 {
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
		wantCodes := strings.Fields(coreResponseCodePins[source.RFC])
		if len(source.Codes) != len(wantCodes) {
			return fmt.Errorf("code count RFC%d", source.RFC)
		}
		for i, code := range source.Codes {
			if code.Code != wantCodes[i] || strings.TrimSpace(code.Note) == "" || len(code.Requirements) == 0 {
				return fmt.Errorf("code disposition RFC%d/%s", source.RFC, code.Code)
			}
			refs := []string{}
			seen := map[string]bool{}
			for _, id := range code.Requirements {
				if items[id] == nil || seen[id] {
					return fmt.Errorf("code requirement %s", id)
				}
				seen[id] = true
				refs = append(refs, "`"+id+"`")
			}
			row := fmt.Sprintf("| RFC%d %s | %s | %s |", source.RFC, code.Code, strings.Join(refs, ", "), code.Note)
			if strings.Count(doc, row) != 1 {
				return fmt.Errorf("code crosswalk drift RFC%d/%s", source.RFC, code.Code)
			}
		}
	}
	return nil
}

func loadCoreResponseAudit(t *testing.T) (coreResponseIndex, map[string]*item, string) {
	t.Helper()
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join("testdata", "core-responses.json"))
	if err != nil {
		t.Fatal(err)
	}
	var index coreResponseIndex
	if err := json.Unmarshal(data, &index); err != nil {
		t.Fatal(err)
	}
	items, err := parseChecklist(filepath.Join(root, "CHECKLIST.md"), func(format string, args ...any) { t.Errorf(format, args...) })
	if err != nil {
		t.Fatal(err)
	}
	return index, items, readFile(t, root, "RFC-RESPONSES.md")
}

func TestCoreResponseInventory(t *testing.T) {
	index, items, doc := loadCoreResponseAudit(t)
	if err := validateCoreResponseIndex(index, items, doc); err != nil {
		t.Fatal(err)
	}
}

// A lost section, duplicated source, stale source pin, invented requirement or
// drift between the machine index and review document must fail closed.
func TestCoreResponseInventoryMutations(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*coreResponseIndex, *string)
		want   string
	}{
		{"lost RFC", func(x *coreResponseIndex, _ *string) { x.Sources = x.Sources[1:] }, "source count"},
		{"duplicate RFC", func(x *coreResponseIndex, _ *string) { x.Sources[1] = x.Sources[0] }, "duplicate RFC"},
		{"lost section", func(x *coreResponseIndex, _ *string) { x.Sources[0].Sections = x.Sources[0].Sections[1:] }, "section count"},
		{"duplicate section", func(x *coreResponseIndex, _ *string) { x.Sources[0].Sections[1] = x.Sources[0].Sections[0] }, "duplicate section"},
		{"source drift", func(x *coreResponseIndex, _ *string) { x.Sources[0].SHA256 = strings.Repeat("0", 64) }, "source pin mismatch"},
		{"unknown requirement", func(x *coreResponseIndex, _ *string) {
			x.Sources[0].Sections[0].Requirements = []string{"IMAP-INVENTED-999"}
		}, "unknown or duplicate requirement"},
		{"empty disposition", func(x *coreResponseIndex, _ *string) { x.Sources[0].Sections[0].Note = "" }, "incomplete disposition"},

		{"section source drift", func(x *coreResponseIndex, _ *string) { x.Sources[0].Sections[0].SHA256 = strings.Repeat("0", 64) }, "incomplete disposition"},
		{"lost requirement disposition", func(x *coreResponseIndex, _ *string) { x.Sources[0].Sections[0].Requirements = nil }, "missing requirement disposition"},
		{"duplicate requirement", func(x *coreResponseIndex, _ *string) {
			x.Sources[0].Sections[0].Requirements = []string{"IMAP-RESP-01", "IMAP-RESP-01"}
		}, "unknown or duplicate requirement"},
		{"source URL drift", func(x *coreResponseIndex, _ *string) { x.Sources[0].URL = "https://example.invalid/rfc3501.txt" }, "source pin mismatch"},
		{"lost code", func(x *coreResponseIndex, _ *string) { x.Sources[0].Codes = x.Sources[0].Codes[1:] }, "code count"},
		{"unknown code", func(x *coreResponseIndex, _ *string) { x.Sources[0].Codes[0].Code = "INVENTED" }, "code disposition"},
		{"unknown code requirement", func(x *coreResponseIndex, _ *string) {
			x.Sources[0].Codes[0].Requirements = []string{"IMAP-INVENTED-999"}
		}, "code requirement"},
		{"code document drift", func(_ *coreResponseIndex, doc *string) {
			*doc = strings.Replace(*doc, "| RFC3501 ALERT |", "| RFC3501 INVENTED |", 1)
		}, "code crosswalk drift"},
		{"document drift", func(_ *coreResponseIndex, doc *string) {
			*doc = strings.Replace(*doc, "| RFC3501 §7 |", "| RFC3501 §999 |", 1)
		}, "crosswalk drift"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			index, items, doc := loadCoreResponseAudit(t)
			tc.mutate(&index, &doc)
			err := validateCoreResponseIndex(index, items, doc)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want %q", err, tc.want)
			}
		})
	}
}
