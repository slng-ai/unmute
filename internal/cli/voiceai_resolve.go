package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/target"
)

// templateVaultNames are the {{$NAME}} vault tokens in a piece of text. It goes
// through ir's own template reader rather than a regex here, so a token this
// recognises is exactly a token the emitted body would resolve.
func templateVaultNames(text string) []string {
	var names []string
	for _, ref := range ir.TemplateRefs(text) {
		if name, ok := ir.VaultToken(ref); ok {
			names = append(names, name)
		}
	}
	return names
}

// The reads deployment resolution needs, on the same boundary as voiceai.go and
// under the same rule: this file shells out and decodes, and it decides nothing.
// preflight.go and deploy_report.go decide, and run no subprocess.
//
// It is a separate file because it reads a different kind of thing. voiceai.go
// lists what an account holds, once per resource kind, and the answers are
// names. Everything here is addressed by an id and answers a question about one
// resource: which published version is this, what does that version's contract
// say, what is on the agent right now. A name is not enough for any of them,
// which is the whole reason spec 007 needed an upstream change.
//
// Three rules hold across every reader below.
//
// One: an id, never a name. A name can be reused, and a rename between the
// check and the write would silently move a read onto a different resource. The
// only name-addressed read left is the catalogue listing that turns a name into
// an id, and it is where ambiguity is refused rather than resolved.
//
// Two: verify what came back. A read that answers about something other than
// what was asked for is an unchecked read, not a successful one, and the whole
// promise of this feature is that the version attached is the version checked.
//
// Three: cache within the run, never across runs. Each deployment resolves the
// latest published versions anew. There is no lockfile and nothing on disk
// remembers an account, so the cache is a cost control and never a source of
// truth.

// slngToolIdentity is an ID-addressed metadata read. It carries the two fields
// the immutable version envelope does not, and they are the two that decide
// eligibility: Source separates a tool the organisation owns from a capability
// SLNG curates, and OrganisationID is what proves the record belongs to the
// account this run confirmed.
type slngToolIdentity struct {
	ID             string `json:"id"`
	OrganisationID string `json:"organisation_id"`
	Name           string `json:"name"`
	ToolType       string `json:"tool_type"`
	// Source is "org" for a tool the organisation owns and "curated" for one
	// SLNG publishes to everybody. A `slng:` reference selects the first;
	// `builtin:` selects the second.
	Source string `json:"source"`
	// LatestVersion is nil-safe as a zero: a tool created in the dashboard and
	// never published reports none, which is a refusal rather than version zero.
	LatestVersion int `json:"latest_version"`
}

// slngPublishedVersion is one immutable published version, which is the only
// thing a deployment may validate a binding against or attach.
//
// The mutable record next door is a draft. Its `arg_schema` is whatever
// somebody last saved in the dashboard, which may name parameters no published
// version has, so a binding checked against it is checked against a contract
// nothing serves. That is not a hypothetical: `tool_get_draft_divergent.json`
// in the fixtures is a real shape of it, renaming the one parameter the example
// injects.
type slngPublishedVersion struct {
	ToolID      string                `json:"tool_id"`
	Version     int                   `json:"version_number"`
	ContentHash string                `json:"content_hash"`
	PublishedAt string                `json:"published_at"`
	Snapshot    slngPublishedSnapshot `json:"snapshot_json"`
}

// slngPublishedSnapshot is the published contract: what the model is told, what
// arguments it may send, and which vault entries the tool reads.
//
// ArgumentSchema is `argument_schema`, and the spelling matters. The mutable
// record calls the same idea `arg_schema`. Decoding one into the other silently
// yields an empty schema, and an empty schema accepts every binding, so the
// check would pass and prove nothing.
type slngPublishedSnapshot struct {
	Name        string `json:"name"`
	ToolType    string `json:"tool_type"`
	Description string `json:"description"`
	// DeclaredSecrets are vault entry names the tool reads. Names only: a value
	// never enters this process.
	DeclaredSecrets []string       `json:"declared_secrets"`
	ArgumentSchema  map[string]any `json:"argument_schema"`
	// ArgumentDefaults are the platform's own, applied by the platform. They are
	// read to find vault references inside them and are never applied here: a
	// default filled in locally would be sent as an argument the author did not
	// write.
	ArgumentDefaults map[string]any `json:"argument_defaults"`
	// Config is the tool's own configuration, read only for the vault names in
	// it (an auth secret, a header secret, a token in a URL template). It is
	// never persisted and never printed: it holds a whole request definition.
	Config map[string]any `json:"config"`
}

// read reports whether this snapshot came from a version read, as opposed to
// being the zero value of a reference that has no published contract to read.
//
// A published version always carries a name, so that is the field asked. It
// exists because a comparison against an unread snapshot reports every field as
// changed, which is a preview that names removals nobody is making.
func (s slngPublishedSnapshot) read() bool { return s.Name != "" }

// slngMCPRecord is a server's whole stored record, addressed by id.
//
// Every field here is read together, because no one of them establishes that a
// selected tool can be attached. A healthy status with a week-old observation is
// stale. A fresh observation on a failed probe found nothing. A truncated record
// listing two tools does not say the third is absent, it says the probe stopped.
type slngMCPRecord struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Revision  int    `json:"revision"`
	// Status is the platform's word for the last probe's outcome: "healthy", or
	// an error state.
	Status        string `json:"capability_status"`
	StatusError   string `json:"capability_error"`
	ObservedAt    string `json:"capability_observed_at"`
	NextRefreshAt string `json:"next_refresh_at"`
	ToolCount     int    `json:"capability_tool_count"`
	// Auth and Headers name vault entries the hosted connection uses. SLNG holds
	// the connection, so these are where an MCP server's credential requirements
	// come from, not from anything the package declares.
	Auth         *slngMCPAuth   `json:"auth"`
	Headers      []slngMCPAuth  `json:"headers"`
	URLTemplate  string         `json:"url_template"`
	Capabilities slngMCPRecords `json:"capabilities"`
}

type slngMCPAuth struct {
	Type       string `json:"type"`
	Name       string `json:"name"`
	SecretName string `json:"secret_name"`
}

type slngMCPRecords struct {
	Truncated    bool           `json:"truncated"`
	PagesFetched int            `json:"pages_fetched"`
	Tools        []slngMCPEntry `json:"tools"`
}

type slngMCPEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// SchemaHash is copied, never computed: it is the platform's own hash of the
	// tool's input schema as its probe saw it, and it is the value a resolved
	// push attaches. Computing one here would be a different number and would
	// refuse every attachment.
	SchemaHash string `json:"schema_hash"`
}

// Tool finds one entry by name. The bool is "the record contains it", which is
// only a statement about absence when the record is complete: usable() is what
// decides whether that distinction can be drawn.
func (r slngMCPRecord) Tool(name string) (slngMCPEntry, bool) {
	for _, entry := range r.Capabilities.Tools {
		if entry.Name == name {
			return entry, true
		}
	}
	return slngMCPEntry{}, false
}

// healthy reports whether the last probe succeeded, in the platform's own word.
//
// Exactly "healthy", which is the platform's own test: its attachment
// validation refuses any other status outright. This accepted "ok" as well,
// which is not a status the platform has, and a permissive alias here is a
// second opinion that can only ever pass something the push refuses.
//
// Deliberately NOT also comparing capability_revision against revision, which
// is the platform's next condition, and the reason is the database rather than
// this CLI's types: a check constraint requires every record whose status is
// not "unknown" to have the two equal (ck_mcp_capability_revision_state), so a
// record this function calls healthy has them equal by construction and the
// comparison can carry no information. The field is present in the response and
// typed as a plain int on the platform's read model, so this is a redundancy
// left out rather than a field this CLI cannot see.
func (r slngMCPRecord) healthy() bool { return r.Status == "healthy" }

// fresh reports whether the last probe is still inside the window the platform
// keeps it for.
//
// The platform expires a snapshot on its OBSERVATION age: it refuses one whose
// `capability_observed_at` is older than its own capability TTL, which is a
// server setting with a default of five minutes and a configurable range. That
// number is not knowable from here and is not copied here, because a constant of
// ours would be a second opinion that goes out of step the first time theirs
// changes.
//
// NextRefreshAt is used as a conservative proxy instead, and it is worth being
// precise about why it is sound rather than convenient. It is not the expiry: it
// is when the platform's own background discovery becomes due, scheduled at
// 80-90% of the TTL after the observation. So it always falls BEFORE the expiry,
// and `now` being before it means the observation is inside the window with
// room to spare. The failure direction is a refresh slightly earlier than
// strictly needed, which costs one connection.
//
// A record with no NextRefreshAt used to be treated as fresh, and that was the
// defect: with no schedule and no TTL, nothing on the record says how old the
// observation may be, so a six-year-old snapshot read as current. An unknown
// window is not a fresh one. Nor is a missing observation time, which the
// platform refuses outright.
func (r slngMCPRecord) fresh(now time.Time) bool {
	// No observation, no freshness. The platform raises on a null one, and its
	// own schema forbids a healthy record from having one, so this is a record
	// that cannot be reasoned about rather than a young one.
	if r.ObservedAt == "" {
		return false
	}
	if _, err := time.Parse(time.RFC3339, r.ObservedAt); err != nil {
		return false
	}
	// No schedule, no window. Refreshing is the safe direction: it costs one
	// connection, and the alternative is attaching a snapshot the push refuses
	// after this run reported it current.
	if r.NextRefreshAt == "" {
		return false
	}
	deadline, err := time.Parse(time.RFC3339, r.NextRefreshAt)
	if err != nil {
		return false
	}
	return now.Before(deadline)
}

// usable reports whether this record can settle a question about a selected
// tool, either way.
//
// Three things together, and no one of them alone. A record that is unhealthy or
// truncated can confirm nothing and deny nothing: it is evidence that discovery
// did not finish, which is a different answer from "the tool is not there". And
// a healthy, complete record whose window has passed is one the platform will
// refuse at push time, so calling it usable would produce a deployment that
// checked a snapshot and then failed to attach it.
func (r slngMCPRecord) usable() bool { return r.usableAt(time.Now()) }

// usableAt is usable with the clock supplied, so a test can hold a fixture to a
// fixed moment rather than to whenever it runs.
func (r slngMCPRecord) usableAt(now time.Time) bool {
	return r.healthy() && !r.Capabilities.Truncated && r.fresh(now)
}

// vaultNames are the entries this server's hosted connection reads, in the
// order they were found and deduplicated. Names only.
func (r slngMCPRecord) vaultNames() []string {
	var names []string
	add := func(name string) {
		if name == "" {
			return
		}
		for _, seen := range names {
			if seen == name {
				return
			}
		}
		names = append(names, name)
	}
	if r.Auth != nil {
		add(r.Auth.SecretName)
	}
	for _, header := range r.Headers {
		add(header.SecretName)
	}
	// A URL template may carry a {{$VAULT_NAME}} token, which is a vault
	// variable rather than a secret. It is still an entry the account has to
	// hold for the connection to resolve.
	for _, ref := range templateVaultNames(r.URLTemplate) {
		add(ref)
	}
	return names
}

// slngLiveAgent is the agent as it stands now, which is what a push replaces.
//
// It is read for the preview and for nothing else. Unmute writes none of these
// fields back: the push owns the write, and reusing an attachment id is its job.
type slngLiveAgent struct {
	ID             string           `json:"id"`
	OrganisationID string           `json:"organisation_id"`
	Name           string           `json:"name"`
	ToolRefs       []slngLiveTool   `json:"tool_refs"`
	MCPRefs        []slngLiveMCPRef `json:"mcp_refs"`
}

// slngLiveTool is one attachment, in the platform's own field names.
//
// Every setting an author can edit in the dashboard is read, because a
// replacement destroys all of them and a preview that does not name one lets it
// disappear silently. That is not a general principle stated in advance: this
// read had a top-level `trigger` field, the platform keeps a trigger under
// `system.triggers`, and the consequence was a real preview reporting an
// invocation change to `system` while saying nothing about the `call_start`
// trigger that made it one.
//
// The shape mirrors the platform's ToolAttachment: attachment_id, tool_id,
// version, description, invocation, system, execution_policy,
// argument_overrides and config_overrides. Nothing here is written back; the
// push owns the write, and reusing an attachment id is its job.
type slngLiveTool struct {
	AttachmentID string         `json:"attachment_id"`
	ToolID       string         `json:"tool_id"`
	Version      int            `json:"version"`
	Description  string         `json:"description"`
	Invocation   string         `json:"invocation"`
	Arguments    map[string]any `json:"argument_overrides"`
	// System carries the triggers and the arguments a system-invoked
	// attachment runs with. Present only when Invocation is "system", which
	// the platform enforces both ways.
	System *slngLiveSystem `json:"system"`
	// ExecutionPolicy is the pre-action message: what the agent says before the
	// tool runs, and whether it waits. Dashboard-only, so a replacement removes
	// it.
	ExecutionPolicy map[string]any `json:"execution_policy"`
	// ConfigOverrides is the per-attachment configuration of a curated
	// capability, such as an end_call prompt or a transfer destination. A
	// tagged union on the platform, read here as a document because the
	// preview names that it exists and never interprets it.
	ConfigOverrides map[string]any `json:"config_overrides"`
}

// slngLiveSystem is a system-invoked attachment's triggers and arguments.
type slngLiveSystem struct {
	Triggers  []slngLiveTrigger `json:"triggers"`
	Arguments []slngLiveArg     `json:"arguments"`
}

// slngLiveTrigger is one event that runs the tool. SourceAttachmentID is set
// only for the two tool-outcome events.
type slngLiveTrigger struct {
	Event              string `json:"event"`
	SourceAttachmentID string `json:"source_attachment_id"`
}

// slngLiveArg is one argument a system-invoked attachment is called with. Its
// source is a tagged union; only the name is named in a preview, because the
// point is that the argument exists and would be lost.
type slngLiveArg struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// trigger names every event that runs this attachment, or nothing.
func (t slngLiveTool) triggers() []string {
	if t.System == nil {
		return nil
	}
	var events []string
	for _, entry := range t.System.Triggers {
		if entry.Event != "" {
			events = append(events, entry.Event)
		}
	}
	return events
}

// preAction reads the sentence the agent speaks before this tool runs, whether
// it waits for it, and whether the message is switched on.
//
// Read out of the raw document rather than typed, so a field the platform adds
// to its execution policy later is still visible to unreadPolicySettings below
// instead of being dropped by a struct that does not name it. ToolExecutionPolicy
// carries exactly one field today, and reading only what we can name is what
// made a top-level `trigger` field the platform does not have look correct.
//
// A message can interleave literal text with call-context values, so an
// expression segment is rendered as its own template rather than skipped: a
// dashboard message built from a call value would otherwise read as an empty
// sentence and be reported as no announcement at all.
func (t slngLiveTool) preAction() (spoken string, wait, enabled bool) {
	message, _ := t.ExecutionPolicy["pre_action_message"].(map[string]any)
	if message == nil {
		return "", false, false
	}
	enabled, _ = message["enabled"].(bool)
	wait, _ = message["wait"].(bool)
	text, _ := message["text"].(map[string]any)
	segments, _ := text["segments"].([]any)
	var parts []string
	for _, entry := range segments {
		segment, _ := entry.(map[string]any)
		if value, ok := segment["value"].(string); ok {
			parts = append(parts, value)
			continue
		}
		if expression, ok := segment["expression"].(string); ok {
			parts = append(parts, "{{"+expression+"}}")
		}
	}
	return strings.Join(parts, ""), wait, enabled
}

// unreadPolicySettings names anything in the live execution policy this preview
// does not read, so a field the platform adds is reported as a setting a
// replacement removes rather than silently dropped.
func (t slngLiveTool) unreadPolicySettings() []string {
	var out []string
	for _, key := range sortedMapKeys(t.ExecutionPolicy) {
		if key != "pre_action_message" {
			out = append(out, "execution_policy."+key+", which the agent has now and this package cannot declare, so a replacement removes it")
		}
	}
	return out
}

// systemArguments names the arguments a system-invoked attachment carries.
func (t slngLiveTool) systemArguments() []string {
	if t.System == nil {
		return nil
	}
	var names []string
	for _, entry := range t.System.Arguments {
		if entry.Name != "" {
			names = append(names, entry.Name)
		}
	}
	return names
}

// slngLiveMCPRef is one MCP attachment.
//
// Description, Arguments and ExecutionPolicy are read for the same reason they
// are on a tool attachment: a replacement destroys them. Comparing the schema
// hash alone reported that a server's tool had changed shape and stayed silent
// about a description and an argument override being removed in the same push.
type slngLiveMCPRef struct {
	AttachmentID    string         `json:"attachment_id"`
	ServerID        string         `json:"server_id"`
	ToolName        string         `json:"tool_name"`
	SchemaHash      string         `json:"observed_schema_hash"`
	Description     string         `json:"description"`
	Arguments       map[string]any `json:"argument_overrides"`
	ExecutionPolicy map[string]any `json:"execution_policy"`
}

// resolveCache holds one deployment's reads. Keyed by id, and by id and version
// for a snapshot, so resolving eight references to the same tool costs one read
// and previewing an unchanged attachment costs none.
//
// It lives for one run. Nothing writes it to disk, and a second deploy resolves
// everything again, because "the latest published version" is a question with a
// different answer every time it is asked.
type resolveCache struct {
	identities map[string]slngToolIdentity
	versions   map[string]slngPublishedVersion
	servers    map[string]slngMCPRecord
	agents     map[string]slngLiveAgent
}

func newResolveCache() *resolveCache {
	return &resolveCache{
		identities: map[string]slngToolIdentity{},
		versions:   map[string]slngPublishedVersion{},
		servers:    map[string]slngMCPRecord{},
		agents:     map[string]slngLiveAgent{},
	}
}

// readToolIdentity reads one tool's metadata by id, and checks that the record
// that came back is the one asked for.
func readToolIdentity(runner *voiceaiRunner, cache *resolveCache, id string) (slngToolIdentity, error) {
	if found, ok := cache.identities[id]; ok {
		return found, nil
	}
	command := target.SlngToolGet.With(id, target.SlngIDFlag)
	var identity slngToolIdentity
	if err := runner.read(command, &identity); err != nil {
		return slngToolIdentity{}, err
	}
	if identity.ID != id {
		return slngToolIdentity{}, &unchecked{Command: command.String(), Reason: fmt.Sprintf(
			"it answered about tool %q, and %q was asked for", identity.ID, id)}
	}
	cache.identities[id] = identity
	return identity, nil
}

// readPublishedVersion reads one immutable published version.
//
// It refuses a version of zero or less before running anything, because the
// only way to reach that is a tool with no publication at all, and asking for
// version zero would be a request the platform is entitled to answer however it
// likes.
func readPublishedVersion(runner *voiceaiRunner, cache *resolveCache, id string, version int) (slngPublishedVersion, error) {
	if version < 1 {
		return slngPublishedVersion{}, fmt.Errorf("tool %s has no published version to read", id)
	}
	key := id + "@" + strconv.Itoa(version)
	if found, ok := cache.versions[key]; ok {
		return found, nil
	}
	command := target.SlngToolGet.With(id, target.SlngVersionFlag, strconv.Itoa(version))
	var snapshot slngPublishedVersion
	if err := runner.read(command, &snapshot); err != nil {
		return slngPublishedVersion{}, err
	}
	// Both halves, because either one being wrong means the contract validated
	// is not the contract attached.
	if snapshot.ToolID != id || snapshot.Version != version {
		return slngPublishedVersion{}, &unchecked{Command: command.String(), Reason: fmt.Sprintf(
			"it answered with tool %q version %d, and %q version %d was asked for",
			snapshot.ToolID, snapshot.Version, id, version)}
	}
	cache.versions[key] = snapshot
	return snapshot, nil
}

// readMCPRecord reads one server's stored record by id.
func readMCPRecord(runner *voiceaiRunner, cache *resolveCache, id string) (slngMCPRecord, error) {
	if found, ok := cache.servers[id]; ok {
		return found, nil
	}
	command := target.SlngMCPGet.With(id, target.SlngIDFlag)
	var record slngMCPRecord
	if err := runner.read(command, &record); err != nil {
		return slngMCPRecord{}, err
	}
	if record.ID != id {
		return slngMCPRecord{}, &unchecked{Command: command.String(), Reason: fmt.Sprintf(
			"it answered about server %q, and %q was asked for", record.ID, id)}
	}
	cache.servers[id] = record
	return record, nil
}

// refreshMCPRecord connects to one server, which refreshes the snapshot SLNG
// stores for it, then reads that same id back and returns the new record.
//
// It is the only read on this path that changes remote state, so: a real deploy
// only, never a dry run, at most once per server per run. It performs the
// server's own discovery handshake and runs no selected tool, which is the
// distinction FR-012 turns on.
//
// The cached record is dropped first, so the reread cannot be served from the
// stale entry the refresh was called to replace.
func refreshMCPRecord(runner *voiceaiRunner, cache *resolveCache, id string) (slngMCPRecord, error) {
	command := target.SlngMCPRun.With(id, target.SlngIDFlag)
	var connected struct {
		Status string `json:"status"`
	}
	if err := runner.read(command, &connected); err != nil {
		return slngMCPRecord{}, err
	}
	// A 200 that says anything but connected is still a server that did not
	// work, and the platform's own CLI treats it that way.
	if connected.Status != "connected" && connected.Status != "healthy" {
		return slngMCPRecord{}, &unchecked{Command: command.String(), Reason: fmt.Sprintf(
			"the server answered %q, so its capabilities were not refreshed", connected.Status)}
	}
	delete(cache.servers, id)
	return readMCPRecord(runner, cache, id)
}

// readLiveAgent reads the agent a push would replace.
//
// A failed read is unchecked and not an empty agent. Treating it as empty would
// turn "I could not see what is there" into "there is nothing there", and the
// preview would report no removals for an agent full of them.
func readLiveAgent(runner *voiceaiRunner, cache *resolveCache, id string) (slngLiveAgent, error) {
	if found, ok := cache.agents[id]; ok {
		return found, nil
	}
	command := target.SlngAgentGet.With(id)
	var live slngLiveAgent
	if err := runner.read(command, &live); err != nil {
		return slngLiveAgent{}, err
	}
	if live.ID != id {
		return slngLiveAgent{}, &unchecked{Command: command.String(), Reason: fmt.Sprintf(
			"it answered about agent %q, and %q was asked for", live.ID, id)}
	}
	cache.agents[id] = live
	return live, nil
}
