package skill

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/slng-ai/unmute/internal/scaffold"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// The bundle restates facts that Go code already owns: the tool execution
// blocks, the catalogue's vendors, the provider set, the command tree, and the
// documentation pages. Constitution III says a fact stated twice gets an
// agreement test, so each of those lists is held here. Every failure names the
// bundle file that has to change.
//
// The command agreement test lives in internal/cli, because the cobra tree is
// unexported and internal/cli already imports this package.

// bundleFile reads one file from the shipped bundle.
func bundleFile(t *testing.T, name string) string {
	t.Helper()
	files, err := New("test").Files(Canonical)
	if err != nil {
		t.Fatal(err)
	}
	content, ok := files[name]
	if !ok {
		t.Fatalf("the bundle has no %s", name)
	}
	return string(content)
}

// referenceNames lists every file under references/ in the shipped bundle.
func referenceNames(t *testing.T) []string {
	t.Helper()
	files, err := New("test").Files(Canonical)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for name := range files {
		if strings.HasPrefix(name, "references/") {
			out = append(out, name)
		}
	}
	return out
}

// TestToolsReferenceMatchesExecutionBlocks holds references/tools.md against the
// Tool struct. A block added or removed in internal/spec fails here until the
// reference is updated, and a block the reference invents fails too.
func TestToolsReferenceMatchesExecutionBlocks(t *testing.T) {
	raw := bundleFile(t, "references/tools.md")

	row := regexp.MustCompile("^\\| `([a-z_]+):` \\|")
	documented := map[string]bool{}
	for _, line := range strings.Split(raw, "\n") {
		if m := row.FindStringSubmatch(line); m != nil {
			documented[m[1]] = true
		}
	}
	if len(documented) == 0 {
		t.Fatal("parsed no execution-block rows from references/tools.md: table format changed? update this parser")
	}

	blocks := map[string]bool{}
	tool := reflect.TypeOf(spec.Tool{})
	for i := range tool.NumField() {
		field := tool.Field(i)
		if field.Type.Kind() != reflect.Pointer {
			continue // the contract fields, which every block shares
		}
		name := strings.Split(field.Tag.Get("yaml"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		blocks[name] = true
	}
	if len(blocks) == 0 {
		t.Fatal("no execution blocks found on spec.Tool: the struct shape changed, so this test needs rewriting")
	}

	for name := range documented {
		if !blocks[name] {
			t.Errorf("references/tools.md documents execution block %q, which spec.Tool does not have", name)
		}
	}
	for name := range blocks {
		if !documented[name] {
			t.Errorf("spec.Tool has execution block %q, which references/tools.md does not document", name)
		}
	}
}

// TestModelsReferenceMatchesCatalog holds references/models.md against the
// provider catalogue, per target per role, and holds the one editorial rule the
// documentation site is written under: SLNG leads every list it appears in.
func TestModelsReferenceMatchesCatalog(t *testing.T) {
	raw := bundleFile(t, "references/models.md")

	// The reference names the roles the way an author writes them; the
	// catalogue keeps the internal name "reason" for the thinking kind.
	roles := map[string]target.Role{"listen": target.Listen, "speak": target.Speak, "think": target.Reason}
	providers := map[string]target.Provider{"pipecat": target.Pipecat, "livekit": target.LiveKit}

	row := regexp.MustCompile(`^\| (pipecat|livekit) \| (listen|speak|think) \| (.*) \|$`)
	vendor := regexp.MustCompile("`([a-z0-9_]+)`")

	documented := map[string][]string{}
	for _, line := range strings.Split(raw, "\n") {
		m := row.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		var vendors []string
		for _, hit := range vendor.FindAllStringSubmatch(m[3], -1) {
			vendors = append(vendors, hit[1])
		}
		documented[m[1]+" "+m[2]] = vendors
	}
	if len(documented) != 6 {
		t.Fatalf("parsed %d vendor rows from references/models.md, want 6 (two targets, three roles) — table format changed? update this parser", len(documented))
	}

	cat := target.DefaultCatalog()
	for key, vendors := range documented {
		parts := strings.Fields(key)
		fw, role := providers[parts[0]], roles[parts[1]]
		catalogued := cat.Vendors(fw, role)

		for _, name := range vendors {
			if !containsString(catalogued, name) {
				t.Errorf("references/models.md lists %s %s %q, which the catalogue does not have", parts[0], parts[1], name)
			}
		}
		for _, name := range catalogued {
			if !containsString(vendors, name) {
				t.Errorf("catalogue entry %s/%s/%s is missing from references/models.md", parts[0], parts[1], name)
			}
		}
		if containsString(catalogued, "slng") && len(vendors) > 0 && vendors[0] != "slng" {
			t.Errorf("references/models.md %s %s lists %q first; slng leads every list it appears in", parts[0], parts[1], vendors[0])
		}
	}

	// The turn role has no catalogue entries on either target, which is exactly
	// why the reference explains a mechanism instead of listing vendors. If that
	// ever changes, the reference has to grow a row.
	for _, fw := range []target.Provider{target.Pipecat, target.LiveKit} {
		if vendors := cat.Vendors(fw, target.Turn); len(vendors) != 0 {
			t.Errorf("the catalogue now has %s turn vendors %v: references/models.md must list them", fw, vendors)
		}
	}
}

// TestProvidersReferenceMatchesTargetSet holds the provider table in
// references/package.md: the set comes from internal/target, and every row says
// whether support means validation or generation.
func TestProvidersReferenceMatchesTargetSet(t *testing.T) {
	raw := bundleFile(t, "references/package.md")

	row := regexp.MustCompile("^\\| `([a-z]+)` \\| (yes|no) \\| (yes|no) \\|$")
	documented := map[target.Provider][2]string{}
	for _, line := range strings.Split(raw, "\n") {
		if m := row.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			documented[target.Provider(m[1])] = [2]string{m[2], m[3]}
		}
	}
	if len(documented) == 0 {
		t.Fatal("parsed no provider rows from references/package.md: table format changed? update this parser")
	}

	for provider := range documented {
		if !containsProvider(target.Providers, provider) {
			t.Errorf("references/package.md names provider %q, which internal/target does not have", provider)
		}
	}
	for _, provider := range target.Providers {
		cells, ok := documented[provider]
		if !ok {
			t.Errorf("provider %q is missing from the table in references/package.md", provider)
			continue
		}
		if cells[0] != "yes" {
			t.Errorf("references/package.md says %q does not validate; every provider validates", provider)
		}
		// Which providers have a shipped driver is target.EmitsProject, which is
		// the one list; ir.Validate reads the same one.
		want := "no"
		if target.EmitsProject(provider) {
			want = "yes"
		}
		if cells[1] != want {
			t.Errorf("references/package.md says %q generates %q, want %q", provider, cells[1], want)
		}
	}
}

// sitePages lists every page path the documentation site carries, as the site
// addresses them: the path under docs-site/ with the .mdx dropped.
func sitePages(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..", "docs-site")
	var pages []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".mdx" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		pages = append(pages, strings.TrimSuffix(filepath.ToSlash(rel), ".mdx"))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) == 0 {
		t.Fatal("found no pages under docs-site/, so this test would pass for the wrong reason")
	}
	return pages
}

// TestBundleNamesNoSitePage holds the inverse of what this test used to hold.
//
// The references once ended with a "Documentation:" line naming the site page
// that owned their facts, and SKILL.md told the reader that page won any
// disagreement. Verified 2026-08-15: the site is not published, so every one of
// those paths resolved to nothing for a reader outside this repository, and an
// assistant that hit a dead pointer could not tell a missing page from its own
// mistake. An instruction nobody can follow is worse than no instruction, so
// the pointers came out and `unmute validate` became the authority instead.
//
// This test keeps them out. When the site is public, the honest move is to put
// the pointers back as absolute URLs and turn this test around again.
func TestBundleNamesNoSitePage(t *testing.T) {
	pages := sitePages(t)

	for _, name := range referenceNames(t) {
		content := bundleFile(t, name)

		if strings.Contains(content, "\nDocumentation:") {
			t.Errorf("%s carries a Documentation line; the site is not published, so its paths resolve to nothing for a reader", name)
		}
		for _, page := range pages {
			if strings.Contains(content, "`"+page+"`") {
				t.Errorf("%s names the site page %q, which a reader outside this repository cannot open; say the fact or point at another file in this bundle", name, page)
			}
		}
	}
}

// TestEntryDocumentBudget holds the layering. SKILL.md is read on every task, so
// it is a decision layer that routes to a reference, not a summary of all of
// them. 500 lines is the documented guidance for an Agent Skills entry file.
func TestEntryDocumentBudget(t *testing.T) {
	lines := strings.Count(bundleFile(t, "SKILL.md"), "\n")
	if lines >= 500 {
		t.Errorf("SKILL.md is %d lines; the budget is under 500. Move detail into a reference rather than raising this number", lines)
	}
}

// TestNoOrphanReferences holds both halves of the routing table: every reference
// on disk is reachable from SKILL.md, and every reference SKILL.md names exists.
func TestNoOrphanReferences(t *testing.T) {
	entry := bundleFile(t, "SKILL.md")

	for _, name := range referenceNames(t) {
		if !strings.Contains(entry, name) {
			t.Errorf("%s is in the bundle but SKILL.md never names it: an assistant will never open it", name)
		}
	}

	named := regexp.MustCompile("`(references/[a-z0-9-]+\\.md)`")
	files, err := New("test").Files(Canonical)
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range named.FindAllStringSubmatch(entry, -1) {
		if _, ok := files[hit[1]]; !ok {
			t.Errorf("SKILL.md routes to %s, which the bundle does not carry", hit[1])
		}
	}
}

// frontmatterKeys returns the top-level YAML keys of a file's frontmatter.
func frontmatterKeys(t *testing.T, content string) []string {
	t.Helper()
	if !strings.HasPrefix(content, "---\n") {
		t.Fatal("the file does not open with YAML frontmatter")
	}
	end := strings.Index(content[4:], "\n---\n")
	if end < 0 {
		t.Fatal("the frontmatter is not closed")
	}
	key := regexp.MustCompile("^([a-z_]+):")
	var out []string
	for _, line := range strings.Split(content[4:4+end], "\n") {
		if m := key.FindStringSubmatch(line); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

func frontmatterValue(content, field string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, field+":") {
			return strings.TrimSpace(strings.TrimPrefix(line, field+":"))
		}
		if line == "---" && strings.Contains(content[:strings.Index(content, line)+1], field+":") {
			break
		}
	}
	return ""
}

// TestFrontmatterIsThePortableSet holds the one thing that decides whether a
// skill is seen at all. name, description, and metadata are the intersection
// every supported assistant accepts; anything outside that set errors on at
// least one of them.
func TestFrontmatterIsThePortableSet(t *testing.T) {
	canonical := bundleFile(t, "SKILL.md")

	pointerFiles, err := New("test").Files(Pointer)
	if err != nil {
		t.Fatal(err)
	}
	pointer := string(pointerFiles["SKILL.md"])

	want := []string{"name", "description", "metadata"}
	for _, file := range []struct {
		label   string
		content string
	}{{"SKILL.md", canonical}, {"pointer/SKILL.md", pointer}} {
		got := frontmatterKeys(t, file.content)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s frontmatter is %v, want exactly %v: anything else errors on at least one assistant", file.label, got, want)
		}
	}

	// The description is the activation trigger, so the pointer has to carry the
	// same one. A pointer that never activates is a pointer nobody follows.
	for _, field := range []string{"name", "description"} {
		if a, b := frontmatterValue(canonical, field), frontmatterValue(pointer, field); a != b {
			t.Errorf("the pointer's %s does not match the canonical one:\n  canonical: %s\n  pointer:   %s", field, a, b)
		}
	}
}

// TestNoSecretsInTheBundle holds the repository's hardest rule. The bundle
// teaches environment variable names and nothing else, so a value that looks
// like a credential is a defect wherever it appears.
func TestNoSecretsInTheBundle(t *testing.T) {
	credential := []struct {
		name    string
		pattern *regexp.Regexp
	}{
		{"an OpenAI-style key", regexp.MustCompile(`sk-[A-Za-z0-9_-]{16,}`)},
		{"an AWS access key id", regexp.MustCompile(`AKIA[0-9A-Z]{12,}`)},
		{"a GitHub token", regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{16,}`)},
		{"a Slack token", regexp.MustCompile(`xox[abprs]-[A-Za-z0-9-]{10,}`)},
		{"a bearer token literal", regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._-]{20,}`)},
		{"a long hex string", regexp.MustCompile(`\b[0-9a-f]{40,}\b`)},
	}
	// An E.164 number is a secret here too, and the only place one may appear is
	// a quoted refusal showing it being rejected.
	phone := regexp.MustCompile(`\+[1-9][0-9]{9,14}`)

	files, err := New("test").Files(Canonical)
	if err != nil {
		t.Fatal(err)
	}
	pointerFiles, err := New("test").Files(Pointer)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range pointerFiles {
		files["pointer/"+name] = content
	}

	for name, raw := range files {
		content := string(raw)
		for _, check := range credential {
			if hit := check.pattern.FindString(content); hit != "" {
				t.Errorf("%s contains %s (%q); the bundle carries environment variable names only", name, check.name, hit)
			}
		}
		for _, line := range strings.Split(content, "\n") {
			hit := phone.FindString(line)
			if hit == "" {
				continue
			}
			if strings.Contains(line, "literal") {
				continue // the documented refusal, which is the point of showing it
			}
			t.Errorf("%s carries the phone number %q outside a refusal example; a destination names an environment variable", name, hit)
		}
	}
}

// beginnerPath is every surface a first-time author meets before they have
// decided anything: the site's front door, the two sections that get them to a
// running agent, the repository README, everything `unmute init` writes, and the
// whole bundle a coding assistant reads. Modelled on TestNoSecretsInTheBundle,
// which is the same shape of prohibition over a whole tree.
func beginnerPath(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	site := filepath.Join("..", "..", "docs-site")
	for _, root := range []string{filepath.Join(site, "index.mdx"), filepath.Join(site, "start"), filepath.Join(site, "build")} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			out[filepath.ToSlash(path)] = string(content)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	out["README.md"] = string(readme)

	dir := filepath.Join(t.TempDir(), "hello-agent")
	created, err := scaffold.Write(dir, scaffold.Data{Name: "hello-agent", Tools: scaffold.DefaultTools()})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range created {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			t.Fatal(err)
		}
		out["unmute init/"+filepath.ToSlash(rel)] = string(content)
	}

	for _, form := range []Destination{Canonical, Pointer} {
		files, err := New("test").Files(form)
		if err != nil {
			t.Fatal(err)
		}
		for name, content := range files {
			out["bundle/"+name] = string(content)
		}
	}
	if len(out) < 10 {
		t.Fatalf("collected %d beginner-path files, so this test would pass for the wrong reason", len(out))
	}
	return out
}

// TestNoUnmuteEnvOnTheBeginnerPath locks a state that already held when it was
// written, and that is the point: an audit found zero hits across all of these
// surfaces, and nothing was holding them there. A generated project must carry
// no Unmute dependency (Principle I), and a variable named UNMUTE_ inside a
// project Unmute is not part of is exactly that shape.
func TestNoUnmuteEnvOnTheBeginnerPath(t *testing.T) {
	unmuteEnv := regexp.MustCompile(`\bUNMUTE_[A-Z0-9_]+\b`)
	for name, content := range beginnerPath(t) {
		if hit := unmuteEnv.FindString(content); hit != "" {
			t.Errorf("%s names %s; nothing a beginner reads may ask them for an Unmute-branded variable", name, hit)
		}
	}
}

// TestOneModelIdEverywhere holds every author-facing surface to the single model
// identifier internal/scaffold owns. It fails on three things, not one: a stale
// identifier, a combined provider/model form, and a
// temperature on a think model, which OpenAI's reference does not state this
// model family accepts (research D10).
func TestOneModelIdEverywhere(t *testing.T) {
	want := scaffold.DefaultReasonModel
	if want == "" {
		t.Fatal("internal/scaffold owns the identifier; an empty constant makes every check below vacuous")
	}
	// Every OpenAI chat identifier shape, so a stale one is caught by shape and
	// not by a list somebody has to remember to extend.
	identifier := regexp.MustCompile(`\bgpt-[0-9][A-Za-z0-9.-]*\b`)
	combined := regexp.MustCompile(`\b(?:openai|slng)/gpt-[A-Za-z0-9.-]+\b`)

	for name, content := range authorFacingModelSurfaces(t) {
		for _, hit := range identifier.FindAllString(content, -1) {
			if hit != want {
				t.Errorf("%s names the model %q; the one identifier is %q", name, hit, want)
			}
		}
		if hit := combined.FindString(content); hit != "" {
			t.Errorf("%s writes %q; provider and model are two fields, and a folded string reaches the SDK verbatim (SCHEMA N15)", name, hit)
		}
		for _, block := range thinkBlocks(content) {
			if strings.Contains(block, "temperature:") {
				t.Errorf("%s sets temperature on a think model; OpenAI does not state this family accepts it, so it stays off until it is verified", name)
			}
		}
	}
}

// authorFacingModelSurfaces is the 24-file set research D10 measured: everything
// an author reads or copies. Test fixtures, goldens, and the specs that record
// the drift as history are deliberately absent.
func authorFacingModelSurfaces(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	read := func(path string) {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		out[filepath.ToSlash(path)] = string(content)
	}
	repo := func(parts ...string) string {
		return filepath.Join(append([]string{"..", ".."}, parts...)...)
	}
	walk := func(root string, ext ...string) {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if entry.IsDir() && root == repo("examples") && entry.Name() == "build" {
				return fs.SkipDir
			}
			if err != nil || entry.IsDir() || !slices.Contains(ext, filepath.Ext(path)) {
				return err
			}
			read(path)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	walk(repo("examples"), ".yaml", ".md")
	walk(repo("docs-site"), ".mdx")
	walk(repo("docs"), ".md")
	read(repo("README.md"))
	read(repo("internal", "scaffold", "scaffold.go"))
	for _, name := range []string{"references/models.md", "references/package.md"} {
		out["bundle/"+name] = bundleFile(t, name)
	}
	return out
}

// thinkBlocks slices out each `think:` section of a YAML or fenced document, so
// a temperature on a speak entry is not mistaken for one on a reasoning model.
func thinkBlocks(content string) []string {
	var blocks []string
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "think:" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		var block []string
		for _, next := range lines[i+1:] {
			if strings.TrimSpace(next) == "" {
				block = append(block, next)
				continue
			}
			if len(next)-len(strings.TrimLeft(next, " ")) <= indent {
				break
			}
			block = append(block, next)
		}
		blocks = append(blocks, strings.Join(block, "\n"))
	}
	return blocks
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func containsProvider(list []target.Provider, want target.Provider) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// TestOneFrameworkVersionEverywhere is the version half of the same rule as
// TestOneModelIdEverywhere: what a release supports has one recorded home
// (target.Window), and no page an author reads may contradict it.
//
// The repository has been burned by this exact drift. Before the support window
// existed, the framework version lived in five unsynchronised places, and the
// examples disagreed with the docs, which disagreed with the tests: examples
// declared 1.6.4 while comments claimed verification against 1.6.9, because the
// emitted LiveKit pin floated instead of honouring what an author declared.
//
// Matching is by shape, not by a list of files someone has to remember to
// extend. Deliberately absent: goldens, internal/testdata fixtures, and specs/,
// which record older versions as history and must not fight this test.
func TestOneFrameworkVersionEverywhere(t *testing.T) {
	windows := target.Windows()
	if len(windows) == 0 {
		t.Fatal("internal/target owns the support window; an empty table makes every check below vacuous")
	}
	ceilings := map[string]target.Provider{}
	for provider, win := range windows {
		if win.Ceiling == "" {
			t.Fatalf("%s has no recorded ceiling", provider)
		}
		ceilings[win.Ceiling] = provider
	}

	// A dependency line naming a framework: the operator makes it a constraint
	// rather than prose, so dated verification notes ("verified against
	// livekit-agents 1.6.9") stay out of scope.
	constraint := regexp.MustCompile(`(livekit-agents|pipecat-ai)(\[[^\]]*\])?(==|>=|<=|~=)([0-9][0-9.]*)`)
	// A target's declared version in an authoring sample. Quoted and three-part,
	// so `version: 1` (the package schema version) and a project's own
	// `version = "0.1.0"` are not swept up.
	declared := regexp.MustCompile(`version:\s*"(\d+\.\d+\.\d+)"`)

	byPackage := map[string]target.Provider{}
	for provider := range windows {
		byPackage[target.FrameworkPackage(provider)] = provider
	}

	for name, content := range authorFacingVersionSurfaces(t) {
		for _, hit := range constraint.FindAllStringSubmatch(content, -1) {
			pkg, operator, version := hit[1], hit[3], hit[4]
			provider, ok := byPackage[pkg]
			if !ok {
				continue
			}
			win := windows[provider]
			if operator != "==" {
				t.Errorf("%s writes %q; a target installs exactly the version it declares, so an author-facing sample pins with == (%s==%s)",
					name, hit[0], pkg, win.Ceiling)
				continue
			}
			if version != win.Ceiling {
				t.Errorf("%s pins %s==%s; this release's %s ceiling is %s", name, pkg, version, pkg, win.Ceiling)
			}
		}
		for _, hit := range declared.FindAllStringSubmatch(content, -1) {
			if _, ok := ceilings[hit[1]]; !ok {
				t.Errorf("%s declares version %q, which is no framework's ceiling; the supported ceilings are %s",
					name, hit[1], strings.Join(sortedCeilings(windows), " and "))
			}
		}
	}
}

func sortedCeilings(windows map[target.Provider]target.SupportWindow) []string {
	var out []string
	for _, provider := range slices.Sorted(maps.Keys(windows)) {
		out = append(out, target.FrameworkPackage(provider)+" "+windows[provider].Ceiling)
	}
	return out
}

// authorFacingVersionSurfaces is everything an author reads or copies that could
// name a framework version: the model-id surfaces plus the rest of the skill
// bundle, which is what a coding assistant reads before it writes a package.
func authorFacingVersionSurfaces(t *testing.T) map[string]string {
	t.Helper()
	out := authorFacingModelSurfaces(t)
	for _, name := range []string{"references/telephony.md", "references/workflow.md", "references/conversation.md"} {
		out["bundle/"+name] = bundleFile(t, name)
	}
	return out
}
