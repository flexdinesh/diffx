package goclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/packages/api"
	"github.com/flexdinesh/diffx/packages/api/goclient"
	"github.com/google/jsonschema-go/jsonschema"
	"go.yaml.in/yaml/v3"
)

func canonicalSchema(t *testing.T, name string) *jsonschema.Resolved {
	t.Helper()
	var document struct {
		Components struct {
			Schemas map[string]interface{} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(api.OpenAPI, &document); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(document.Components.Schemas)
	if err != nil {
		t.Fatal(err)
	}
	// JSON Schema validators use $defs rather than OpenAPI's components container.
	// Only relocate references for validation; retain every schema keyword.
	data = []byte(strings.ReplaceAll(string(data), "#/components/schemas/", "#/$defs/"))
	var definitions map[string]*jsonschema.Schema
	if err := json.Unmarshal(data, &definitions); err != nil {
		t.Fatal(err)
	}
	schema := &jsonschema.Schema{Ref: "#/$defs/" + name, Defs: definitions}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func jsonValue(t *testing.T, data []byte) interface{} {
	t.Helper()
	var value interface{}
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestCapabilitiesExtractionPreservesSchema(t *testing.T) {
	original, err := os.ReadFile("testdata/capabilities.before.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var before interface{}
	if err := yaml.Unmarshal(original, &before); err != nil {
		t.Fatal(err)
	}
	var document struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]interface{} `yaml:"properties"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(api.OpenAPI, &document); err != nil {
		t.Fatal(err)
	}
	var canonical struct {
		Components struct {
			Schemas map[string]interface{} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(api.OpenAPI, &canonical); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, canonical.Components.Schemas["SessionCapabilities"]) {
		t.Fatal("extraction changed capabilities schema keywords or constraints")
	}
	for _, owner := range []string{"Context", "Session"} {
		want := map[string]interface{}{"$ref": "#/components/schemas/SessionCapabilities"}
		if !reflect.DeepEqual(document.Components.Schemas[owner].Properties["capabilities"], want) {
			t.Errorf("%s.capabilities does not reference the extracted schema", owner)
		}
	}
}

func TestGeneratorOverlayChangesOnlyGoNames(t *testing.T) {
	data, err := os.ReadFile("names.overlay.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var overlay struct {
		Actions []struct {
			Target string                 `yaml:"target"`
			Update map[string]interface{} `yaml:"update"`
			Remove bool                   `yaml:"remove"`
		} `yaml:"actions"`
	}
	if err := yaml.Unmarshal(data, &overlay); err != nil {
		t.Fatal(err)
	}
	if len(overlay.Actions) != 1 {
		t.Fatalf("unexpected overlay actions: %d", len(overlay.Actions))
	}
	action := overlay.Actions[0]
	want := map[string]interface{}{"x-go-type-name": "ComparisonPolicyKind"}
	if action.Target != "$.components.schemas.Comparison.properties.kind" || action.Remove || !reflect.DeepEqual(action.Update, want) {
		t.Fatalf("overlay must only rename the Go comparison policy enum: %#v", action)
	}
	roundTrip[goclient.Comparison](t, "Comparison", `{"kind":"branch","baseRef":"refs/heads/main"}`)
	roundTrip[goclient.ComparisonKind](t, "ComparisonKind", `"combined"`)
}

func roundTrip[T interface{}](t *testing.T, schemaName, input string) T {
	t.Helper()
	schema := canonicalSchema(t, schemaName)
	want := jsonValue(t, []byte(input))
	if err := schema.Validate(want); err != nil {
		t.Fatalf("invalid fixture: %v", err)
	}
	var model T
	if err := json.Unmarshal([]byte(input), &model); err != nil {
		t.Fatal(err)
	}
	output, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	got := jsonValue(t, output)
	if err := schema.Validate(got); err != nil {
		t.Fatalf("generated model violates canonical schema: %v\n%s", err, output)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("wire data changed\ninput: %s\noutput: %s", input, output)
	}
	return model
}

const localBootstrap = `{"mode":"local","googleEnabled":false,"user":null,"csrfToken":null,"collect":true,"input":"checkout"}`

const capabilities = `{"diff":{"scopes":{"state":"enabled","values":["all","committed"]},"refresh":{"state":"disabled"},"stagingMetadata":{"state":"unavailable"}},"files":{"contents":{"state":"enabled"}},"review":{"comments":{"state":"enabled"}}}`

func TestSessionCapabilitiesRoundTrip(t *testing.T) {
	roundTrip[goclient.SessionCapabilities](t, "SessionCapabilities", capabilities)
	session := roundTrip[goclient.Session](t, "Session", `{"id":"diff","user":{"id":"owner","name":"Local"},"locationId":null,"repositoryId":null,"source":"stdin","name":"Capture","root":"","capabilities":`+capabilities+`}`)
	if !session.LocationId.IsNull() || !session.RepositoryId.IsNull() {
		t.Fatal("capture session lost explicit null identities")
	}
}

func TestBootstrapRoundTrips(t *testing.T) {
	for _, input := range []string{"checkout", "checkouts", "capture"} {
		t.Run(input, func(t *testing.T) {
			fixture := strings.ReplaceAll(localBootstrap, "checkout", input)
			local := roundTrip[goclient.Bootstrap0](t, "Bootstrap", fixture)
			if !local.User.IsNull() || !local.CsrfToken.IsNull() {
				t.Fatal("local null-only fields must remain explicit nulls")
			}
			var union goclient.Bootstrap
			if err := union.FromBootstrap0(local); err != nil {
				t.Fatal(err)
			}
			decoded, err := union.AsBootstrap0()
			if err != nil || !reflect.DeepEqual(decoded, local) {
				t.Fatalf("local union lost data: %v", err)
			}
			roundTrip[goclient.Bootstrap](t, "Bootstrap", fixture)
		})
	}
	for _, user := range []string{"null", `{"id":"owner","email":"owner@example.com","method":"password"}`} {
		t.Run(user, func(t *testing.T) {
			token := "null"
			if user != "null" {
				token = `"csrf-token"`
			}
			fixture := `{"mode":"remote","googleEnabled":true,"signupEnabled":false,"passwordSignupEnabled":true,"publicPullRequests":false,"user":` + user + `,"csrfToken":` + token + `}`
			remote := roundTrip[goclient.Bootstrap1](t, "Bootstrap", fixture)
			if remote.User.IsNull() != (user == "null") || remote.CsrfToken.IsNull() != (token == "null") {
				t.Fatal("remote nullable fields lost null/value distinction")
			}
			var union goclient.Bootstrap
			if err := union.FromBootstrap1(remote); err != nil {
				t.Fatal(err)
			}
			decoded, err := union.AsBootstrap1()
			if err != nil || !reflect.DeepEqual(decoded, remote) {
				t.Fatalf("remote union lost data: %v", err)
			}
			roundTrip[goclient.Bootstrap](t, "Bootstrap", fixture)
		})
	}
}

const commit = `"oid":"0123456789012345678901234567890123456789","parents":[],"subject":"Capture commit","body":"Details\n","authorName":"Author","authoredAt":1793836456789,"committedAt":1793836456790`

func TestCommitAllOfRoundTrips(t *testing.T) {
	roundTrip[goclient.CommitSummary](t, "CommitSummary", `{`+commit+`,"merge":false,"fileCount":1,"additions":2,"deletions":0,"patchId":"stable"}`)
	roundTrip[goclient.IngestionCommit](t, "IngestionCommit", `{`+commit+`}`)
	roundTrip[goclient.IngestionCommit](t, "IngestionCommit", `{`+commit+`,"snapshot":{"id":"diff","versionId":"version","locationId":null,"repositoryId":null,"source":"local","root":"/checkout","name":"repo","branch":"main","head":null,"mode":"commit","comparisonKind":"commit","files":[],"revision":"revision"},"patches":{"file":{"patch":"patch","message":null,"contents":{"before":"old","after":"new"}}}}`)
}

const comment = `"id":"comment","diffId":"diff","versionId":"version","path":"file.go","scope":"all","fingerprint":"fingerprint","body":"Review","status":"open","createdAt":1793836456789`
const carriedFrom = `"carriedFrom":{"versionId":"earlier","side":"deletions","start":3,"end":4}`
const createComment = `"diffId":"diff","versionId":"version","fileId":"file","scope":"all","fileVersion":"fingerprint","body":"Review"`

func TestConditionalCommentRoundTrips(t *testing.T) {
	for _, selection := range []string{
		`"side":"deletions","start":3,"end":4,"code":"old\ncode"`,
		`"target":"lines","side":"additions","start":1,"end":1,"code":"new"`,
		`"target":"file","side":"additions","start":0,"end":0,"code":""`,
		`"target":"file","side":"additions","start":0,"end":0,"code":"old\ncode",` + carriedFrom,
	} {
		t.Run(selection, func(t *testing.T) {
			roundTrip[goclient.ReviewComment](t, "ReviewComment", `{`+comment+`,`+selection+`}`)
			roundTrip[goclient.AgentReviewComment](t, "AgentReviewComment", `{`+comment+`,`+selection+`,"applicability":"anchored","actionable":true}`)
		})
	}
	for _, selection := range []string{
		`"side":"deletions","start":3,"end":4`,
		`"target":"lines","side":"additions","start":1,"end":1`,
		`"target":"file","side":"additions","start":0,"end":0`,
	} {
		roundTrip[goclient.CreateComment](t, "CreateComment", `{`+createComment+`,`+selection+`}`)
	}
}

func TestCanonicalConstraintsRejectInvalidWireValues(t *testing.T) {
	for _, tc := range []struct{ name, schema, input string }{
		{"local user", "Bootstrap", strings.Replace(localBootstrap, `"user":null`, `"user":{}`, 1)},
		{"missing required null", "Bootstrap", strings.Replace(localBootstrap, `"user":null,`, "", 1)},
		{"mixed bootstrap modes", "Bootstrap", strings.Replace(localBootstrap, `"collect":true`, `"collect":true,"signupEnabled":true`, 1)},
		{"invalid commit OID", "CommitSummary", `{` + strings.Replace(commit, "0123456789012345678901234567890123456789", "invalid", 1) + `,"merge":false,"fileCount":1,"additions":0,"deletions":0}`},
		{"missing allOf property", "CommitSummary", `{` + commit + `,"fileCount":1,"additions":0,"deletions":0}`},
		{"negative allOf count", "CommitSummary", `{` + commit + `,"merge":false,"fileCount":-1,"additions":0,"deletions":0}`},
		{"create zero line anchor", "CreateComment", `{` + createComment + `,"side":"additions","start":0,"end":0}`},
		{"create file on deletion side", "CreateComment", `{` + createComment + `,"target":"file","side":"deletions","start":0,"end":0}`},
		{"create file with lines", "CreateComment", `{` + createComment + `,"target":"file","side":"additions","start":1,"end":1}`},
		{"zero line anchor", "ReviewComment", `{` + comment + `,"side":"additions","start":0,"end":0,"code":""}`},
		{"file on deletion side", "ReviewComment", `{` + comment + `,"target":"file","side":"deletions","start":0,"end":0,"code":""}`},
		{"file with lines", "ReviewComment", `{` + comment + `,"target":"file","side":"additions","start":1,"end":1,"code":""}`},
		{"file with uncarried code", "ReviewComment", `{` + comment + `,"target":"file","side":"additions","start":0,"end":0,"code":"old"}`},
		{"carried line", "ReviewComment", `{` + comment + `,"side":"additions","start":1,"end":1,"code":"old",` + carriedFrom + `}`},
		{"unknown comment property", "ReviewComment", `{` + comment + `,"side":"additions","start":1,"end":1,"code":"old","unknown":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := canonicalSchema(t, tc.schema).Validate(jsonValue(t, []byte(tc.input))); err == nil {
				t.Fatal("canonical constraint accepted invalid wire value")
			}
		})
	}
}

func TestAllCanonicalOperationsHaveGeneratedClientMethods(t *testing.T) {
	var document struct {
		Paths map[string]map[string]yaml.Node `yaml:"paths"`
	}
	if err := yaml.Unmarshal(api.OpenAPI, &document); err != nil {
		t.Fatal(err)
	}
	client := reflect.TypeFor[goclient.ClientInterface]()
	for path, operations := range document.Paths {
		for verb, node := range operations {
			if node.Kind != yaml.MappingNode {
				continue // Path-level parameters are a sequence, not an operation.
			}
			var operation struct {
				OperationID string `yaml:"operationId"`
			}
			if err := node.Decode(&operation); err != nil {
				t.Fatal(err)
			}
			if operation.OperationID == "" {
				continue
			}
			method := strings.ToUpper(operation.OperationID[:1]) + operation.OperationID[1:]
			if _, ok := client.MethodByName(method); !ok {
				t.Errorf("%s %s: generated client missing %s", verb, path, method)
			}
		}
	}
}

func TestGeneratedClientUsesInjectedUnixHTTPClient(t *testing.T) {
	directory, err := os.MkdirTemp("", "dx-api-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(directory) })
	socket := filepath.Join(directory, "api.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	cancelled := make(chan struct{})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/bootstrap":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, localBootstrap)
		case "/api/v2/events":
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "event: ingestion\ndata: {\"contextId\":\"context\"}\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			close(cancelled)
		default:
			http.NotFound(w, r)
		}
	}))
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	t.Cleanup(transport.CloseIdleConnections)
	client, err := goclient.NewClientWithResponses("http://localhost", goclient.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	response, err := client.GetBootstrapWithResponse(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		t.Fatalf("unexpected bootstrap response: %#v", response)
	}
	local, err := response.JSON200.AsBootstrap0()
	if err != nil || local.Mode != goclient.Bootstrap0ModeLocal || !local.User.IsNull() {
		t.Fatalf("invalid local bootstrap: %#v, %v", local, err)
	}
	streamCtx, stopStream := context.WithCancel(ctx)
	defer stopStream()
	// Use the raw method for SSE: response helpers buffer the entire body.
	stream, err := client.WatchIngestions(streamCtx)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	want := "event: ingestion\ndata: {\"contextId\":\"context\"}\n\n"
	data := make([]byte, len(want))
	if _, err := io.ReadFull(stream.Body, data); err != nil || string(data) != want {
		t.Fatalf("SSE body: %q, %v", data, err)
	}
	stopStream()
	if _, err := io.ReadAll(stream.Body); !errors.Is(err, context.Canceled) {
		t.Fatalf("stream cancellation: %v", err)
	}
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal("server did not observe stream cancellation")
	}
}
