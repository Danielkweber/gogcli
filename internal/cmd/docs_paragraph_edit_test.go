package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"google.golang.org/api/docs/v1"
	"google.golang.org/api/option"

	"github.com/steipete/gogcli/internal/outfmt"
	"github.com/steipete/gogcli/internal/ui"
)

// structuredDocResponse returns a JSON response with realistic doc structure
// including StartIndex/EndIndex that the API would actually return.
func structuredDocResponse(id string) map[string]any {
	return map[string]any{
		"documentId": id,
		"revisionId": "rev-123",
		"body": map[string]any{
			"content": []any{
				map[string]any{
					"sectionBreak": map[string]any{},
					"startIndex":   0,
					"endIndex":     0,
				},
				map[string]any{
					"startIndex": 0,
					"endIndex":   14,
					"paragraph": map[string]any{
						"paragraphStyle": map[string]any{"namedStyleType": "TITLE"},
						"elements": []any{
							map[string]any{
								"textRun": map[string]any{"content": "Meeting Notes\n"},
							},
						},
					},
				},
				map[string]any{
					"startIndex": 14,
					"endIndex":   25,
					"paragraph": map[string]any{
						"paragraphStyle": map[string]any{"namedStyleType": "HEADING_1"},
						"elements": []any{
							map[string]any{
								"textRun": map[string]any{"content": "Attendees\n"},
							},
						},
					},
				},
				map[string]any{
					"startIndex": 25,
					"endIndex":   44,
					"paragraph": map[string]any{
						"paragraphStyle": map[string]any{"namedStyleType": "NORMAL_TEXT"},
						"elements": []any{
							map[string]any{
								"textRun": map[string]any{"content": "Alice, Bob, Carol\n"},
							},
						},
					},
				},
			},
		},
	}
}

func newStructuredTestServer(t *testing.T) (*docs.Service, *[]docs.BatchUpdateDocumentRequest, func()) {
	t.Helper()

	var captured []docs.BatchUpdateDocumentRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasPrefix(path, "/v1/documents/") && r.Method == http.MethodGet:
			id := strings.TrimPrefix(path, "/v1/documents/")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(structuredDocResponse(id))

		case strings.HasSuffix(path, ":batchUpdate") && r.Method == http.MethodPost:
			var req docs.BatchUpdateDocumentRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode batchUpdate: %v", err)
			}
			captured = append(captured, req)
			w.Header().Set("Content-Type", "application/json")
			docID := strings.TrimSuffix(strings.TrimPrefix(path, "/v1/documents/"), ":batchUpdate")
			_ = json.NewEncoder(w).Encode(map[string]any{"documentId": docID})

		default:
			http.NotFound(w, r)
		}
	}))

	docSvc, err := docs.NewService(context.Background(),
		option.WithoutAuthentication(),
		option.WithHTTPClient(srv.Client()),
		option.WithEndpoint(srv.URL+"/"),
	)
	if err != nil {
		srv.Close()
		t.Fatalf("NewDocsService: %v", err)
	}
	return docSvc, &captured, srv.Close
}

func TestDocsStructure_Text(t *testing.T) {
	origDocs := newDocsService
	t.Cleanup(func() { newDocsService = origDocs })

	docSvc, _, cleanup := newStructuredTestServer(t)
	defer cleanup()
	newDocsService = func(context.Context, string) (*docs.Service, error) { return docSvc, nil }

	flags := &RootFlags{Account: "a@b.com"}

	out := captureStdout(t, func() {
		u, _ := ui.New(ui.Options{Stdout: os.Stdout, Stderr: io.Discard, Color: "never"})
		ctx := ui.WithUI(context.Background(), u)
		cmd := &DocsStructureCmd{}
		if err := runKong(t, cmd, []string{"doc1"}, ctx, flags); err != nil {
			t.Fatalf("docs structure: %v", err)
		}
	})

	if !strings.Contains(out, "TITLE") {
		t.Fatalf("expected TITLE in output, got: %q", out)
	}
	if !strings.Contains(out, "Meeting Notes") {
		t.Fatalf("expected 'Meeting Notes' in output, got: %q", out)
	}
	if !strings.Contains(out, "HEADING_1") {
		t.Fatalf("expected HEADING_1 in output, got: %q", out)
	}
	if !strings.Contains(out, "Alice, Bob, Carol") {
		t.Fatalf("expected 'Alice, Bob, Carol' in output, got: %q", out)
	}
}

func TestDocsStructure_JSON(t *testing.T) {
	origDocs := newDocsService
	t.Cleanup(func() { newDocsService = origDocs })

	docSvc, _, cleanup := newStructuredTestServer(t)
	defer cleanup()
	newDocsService = func(context.Context, string) (*docs.Service, error) { return docSvc, nil }

	flags := &RootFlags{Account: "a@b.com"}
	u, _ := ui.New(ui.Options{Stdout: io.Discard, Stderr: io.Discard, Color: "never"})
	ctx := ui.WithUI(context.Background(), u)
	ctx = outfmt.WithMode(ctx, outfmt.Mode{JSON: true})

	out := captureStdout(t, func() {
		cmd := &DocsStructureCmd{}
		if err := runKong(t, cmd, []string{"doc1"}, ctx, flags); err != nil {
			t.Fatalf("docs structure --json: %v", err)
		}
	})

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("JSON parse: %v\nraw: %q", err, out)
	}

	if result["documentId"] != "doc1" {
		t.Fatalf("unexpected documentId: %v", result["documentId"])
	}
	if result["revisionId"] != "rev-123" {
		t.Fatalf("unexpected revisionId: %v", result["revisionId"])
	}

	paragraphs, ok := result["paragraphs"].([]any)
	if !ok || len(paragraphs) != 3 {
		t.Fatalf("expected 3 paragraphs, got: %v", result["paragraphs"])
	}

	first := paragraphs[0].(map[string]any)
	if first["type"] != "TITLE" {
		t.Fatalf("expected TITLE, got: %v", first["type"])
	}
	if first["text"] != "Meeting Notes" {
		t.Fatalf("expected 'Meeting Notes', got: %v", first["text"])
	}
}

func TestDocsCat_Numbered(t *testing.T) {
	origDocs := newDocsService
	t.Cleanup(func() { newDocsService = origDocs })

	docSvc, _, cleanup := newStructuredTestServer(t)
	defer cleanup()
	newDocsService = func(context.Context, string) (*docs.Service, error) { return docSvc, nil }

	flags := &RootFlags{Account: "a@b.com"}
	u, _ := ui.New(ui.Options{Stdout: io.Discard, Stderr: io.Discard, Color: "never"})
	ctx := ui.WithUI(context.Background(), u)

	out := captureStdout(t, func() {
		cmd := &DocsCatCmd{}
		if err := runKong(t, cmd, []string{"doc1", "--numbered"}, ctx, flags); err != nil {
			t.Fatalf("docs cat --numbered: %v", err)
		}
	})

	if !strings.Contains(out, "[1] Meeting Notes") {
		t.Fatalf("expected '[1] Meeting Notes', got: %q", out)
	}
	if !strings.Contains(out, "[2] Attendees") {
		t.Fatalf("expected '[2] Attendees', got: %q", out)
	}
	if !strings.Contains(out, "[3] Alice, Bob, Carol") {
		t.Fatalf("expected '[3] Alice, Bob, Carol', got: %q", out)
	}
}

func TestDocsEditParagraph_SendsExpectedRequests(t *testing.T) {
	origDocs := newDocsService
	t.Cleanup(func() { newDocsService = origDocs })

	docSvc, captured, cleanup := newStructuredTestServer(t)
	defer cleanup()
	newDocsService = func(context.Context, string) (*docs.Service, error) { return docSvc, nil }

	flags := &RootFlags{Account: "a@b.com"}
	cmd := &DocsEditParaCmd{}
	if err := runKong(t, cmd, []string{"doc1", "--paragraph", "3", "--text", "Updated list"}, newDocsCmdContext(t), flags); err != nil {
		t.Fatalf("docs edit: %v", err)
	}

	if len(*captured) != 1 {
		t.Fatalf("expected 1 batchUpdate call, got %d", len(*captured))
	}

	reqs := (*captured)[0].Requests
	if len(reqs) != 2 {
		t.Fatalf("expected 2 requests (delete + insert), got %d", len(reqs))
	}

	// First request should be delete.
	if reqs[0].DeleteContentRange == nil {
		t.Fatal("expected DeleteContentRange as first request")
	}
	rng := reqs[0].DeleteContentRange.Range
	if rng.StartIndex != 25 || rng.EndIndex != 43 {
		t.Fatalf("delete range: %d-%d, want 25-43", rng.StartIndex, rng.EndIndex)
	}

	// Second should be insert.
	if reqs[1].InsertText == nil {
		t.Fatal("expected InsertText as second request")
	}
	if reqs[1].InsertText.Text != "Updated list" {
		t.Fatalf("insert text = %q, want 'Updated list'", reqs[1].InsertText.Text)
	}
	if reqs[1].InsertText.Location.Index != 25 {
		t.Fatalf("insert index = %d, want 25", reqs[1].InsertText.Location.Index)
	}
}

func TestDocsEditParagraph_WithRevision(t *testing.T) {
	origDocs := newDocsService
	t.Cleanup(func() { newDocsService = origDocs })

	docSvc, captured, cleanup := newStructuredTestServer(t)
	defer cleanup()
	newDocsService = func(context.Context, string) (*docs.Service, error) { return docSvc, nil }

	flags := &RootFlags{Account: "a@b.com"}
	cmd := &DocsEditParaCmd{}
	if err := runKong(t, cmd, []string{"doc1", "--paragraph", "1", "--text", "New Title", "--revision", "rev-123"}, newDocsCmdContext(t), flags); err != nil {
		t.Fatalf("docs edit --revision: %v", err)
	}

	if len(*captured) != 1 {
		t.Fatalf("expected 1 batchUpdate call, got %d", len(*captured))
	}

	req := (*captured)[0]
	if req.WriteControl == nil || req.WriteControl.RequiredRevisionId != "rev-123" {
		t.Fatalf("expected WriteControl with revision rev-123, got: %+v", req.WriteControl)
	}
}

func TestDocsEditParagraph_JSON(t *testing.T) {
	origDocs := newDocsService
	t.Cleanup(func() { newDocsService = origDocs })

	docSvc, _, cleanup := newStructuredTestServer(t)
	defer cleanup()
	newDocsService = func(context.Context, string) (*docs.Service, error) { return docSvc, nil }

	flags := &RootFlags{Account: "a@b.com"}
	u, _ := ui.New(ui.Options{Stdout: io.Discard, Stderr: io.Discard, Color: "never"})
	ctx := ui.WithUI(context.Background(), u)
	ctx = outfmt.WithMode(ctx, outfmt.Mode{JSON: true})

	out := captureStdout(t, func() {
		cmd := &DocsEditParaCmd{}
		if err := runKong(t, cmd, []string{"doc1", "--paragraph", "1", "--text", "X"}, ctx, flags); err != nil {
			t.Fatalf("docs edit --json: %v", err)
		}
	})

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("JSON parse: %v\nraw: %q", err, out)
	}
	if result["action"] != "edited" {
		t.Fatalf("expected action=edited, got: %v", result["action"])
	}
}

func TestDocsEditParagraph_OutOfRange(t *testing.T) {
	origDocs := newDocsService
	t.Cleanup(func() { newDocsService = origDocs })

	docSvc, _, cleanup := newStructuredTestServer(t)
	defer cleanup()
	newDocsService = func(context.Context, string) (*docs.Service, error) { return docSvc, nil }

	flags := &RootFlags{Account: "a@b.com"}
	cmd := &DocsEditParaCmd{}
	err := runKong(t, cmd, []string{"doc1", "--paragraph", "99", "--text", "X"}, newDocsCmdContext(t), flags)
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("expected out of range error, got: %v", err)
	}
}

func TestDocsInsertParagraph_SendsExpectedRequest(t *testing.T) {
	origDocs := newDocsService
	t.Cleanup(func() { newDocsService = origDocs })

	docSvc, captured, cleanup := newStructuredTestServer(t)
	defer cleanup()
	newDocsService = func(context.Context, string) (*docs.Service, error) { return docSvc, nil }

	flags := &RootFlags{Account: "a@b.com"}
	cmd := &DocsInsertParaCmd{}
	if err := runKong(t, cmd, []string{"doc1", "--after", "2", "--text", "New bullet"}, newDocsCmdContext(t), flags); err != nil {
		t.Fatalf("docs insert-paragraph: %v", err)
	}

	if len(*captured) != 1 {
		t.Fatalf("expected 1 batchUpdate call, got %d", len(*captured))
	}

	reqs := (*captured)[0].Requests
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request (insert), got %d", len(reqs))
	}

	ins := reqs[0].InsertText
	if ins == nil {
		t.Fatal("expected InsertText request")
	}
	// Paragraph 2 ends at index 25. Insert at endIndex-1 = 24.
	if ins.Location.Index != 24 {
		t.Fatalf("insert index = %d, want 24", ins.Location.Index)
	}
	if ins.Text != "\nNew bullet" {
		t.Fatalf("insert text = %q, want '\\nNew bullet'", ins.Text)
	}
}

func TestDocsInsertParagraph_AtBeginning(t *testing.T) {
	origDocs := newDocsService
	t.Cleanup(func() { newDocsService = origDocs })

	docSvc, captured, cleanup := newStructuredTestServer(t)
	defer cleanup()
	newDocsService = func(context.Context, string) (*docs.Service, error) { return docSvc, nil }

	flags := &RootFlags{Account: "a@b.com"}
	cmd := &DocsInsertParaCmd{}
	if err := runKong(t, cmd, []string{"doc1", "--after", "0", "--text", "Prologue"}, newDocsCmdContext(t), flags); err != nil {
		t.Fatalf("docs insert-paragraph --after 0: %v", err)
	}

	if len(*captured) != 1 {
		t.Fatalf("expected 1 batchUpdate call, got %d", len(*captured))
	}

	ins := (*captured)[0].Requests[0].InsertText
	if ins == nil {
		t.Fatal("expected InsertText request")
	}
	// First paragraph starts at index 0, so insert at 0.
	if ins.Location.Index != 0 {
		t.Fatalf("insert index = %d, want 0", ins.Location.Index)
	}
	if ins.Text != "Prologue\n" {
		t.Fatalf("insert text = %q, want 'Prologue\\n'", ins.Text)
	}
}

func TestDocsRemoveParagraph_SendsExpectedRequest(t *testing.T) {
	origDocs := newDocsService
	t.Cleanup(func() { newDocsService = origDocs })

	docSvc, captured, cleanup := newStructuredTestServer(t)
	defer cleanup()
	newDocsService = func(context.Context, string) (*docs.Service, error) { return docSvc, nil }

	flags := &RootFlags{Account: "a@b.com"}
	cmd := &DocsRemoveParaCmd{}
	if err := runKong(t, cmd, []string{"doc1", "--paragraph", "2"}, newDocsCmdContext(t), flags); err != nil {
		t.Fatalf("docs remove-paragraph: %v", err)
	}

	if len(*captured) != 1 {
		t.Fatalf("expected 1 batchUpdate call, got %d", len(*captured))
	}

	del := (*captured)[0].Requests[0].DeleteContentRange
	if del == nil {
		t.Fatal("expected DeleteContentRange request")
	}
	// Paragraph 2: startIndex=14, endIndex=25 (middle paragraph, not last).
	if del.Range.StartIndex != 14 || del.Range.EndIndex != 25 {
		t.Fatalf("delete range: %d-%d, want 14-25", del.Range.StartIndex, del.Range.EndIndex)
	}
}

func TestDocsRemoveParagraph_Last(t *testing.T) {
	origDocs := newDocsService
	t.Cleanup(func() { newDocsService = origDocs })

	docSvc, captured, cleanup := newStructuredTestServer(t)
	defer cleanup()
	newDocsService = func(context.Context, string) (*docs.Service, error) { return docSvc, nil }

	flags := &RootFlags{Account: "a@b.com"}
	cmd := &DocsRemoveParaCmd{}
	if err := runKong(t, cmd, []string{"doc1", "--paragraph", "3"}, newDocsCmdContext(t), flags); err != nil {
		t.Fatalf("docs remove-paragraph (last): %v", err)
	}

	if len(*captured) != 1 {
		t.Fatalf("expected 1 batchUpdate call, got %d", len(*captured))
	}

	del := (*captured)[0].Requests[0].DeleteContentRange
	if del == nil {
		t.Fatal("expected DeleteContentRange request")
	}
	// Last paragraph (3 of 3): special case — delete from prev.EndIndex-1 to para.EndIndex-1.
	// prev (paragraph 2) endIndex=25, so start=24. This paragraph endIndex=44, so end=43.
	if del.Range.StartIndex != 24 || del.Range.EndIndex != 43 {
		t.Fatalf("delete range for last paragraph: %d-%d, want 24-43", del.Range.StartIndex, del.Range.EndIndex)
	}
}

func TestDocsRemoveParagraph_JSON(t *testing.T) {
	origDocs := newDocsService
	t.Cleanup(func() { newDocsService = origDocs })

	docSvc, _, cleanup := newStructuredTestServer(t)
	defer cleanup()
	newDocsService = func(context.Context, string) (*docs.Service, error) { return docSvc, nil }

	flags := &RootFlags{Account: "a@b.com"}
	u, _ := ui.New(ui.Options{Stdout: io.Discard, Stderr: io.Discard, Color: "never"})
	ctx := ui.WithUI(context.Background(), u)
	ctx = outfmt.WithMode(ctx, outfmt.Mode{JSON: true})

	out := captureStdout(t, func() {
		cmd := &DocsRemoveParaCmd{}
		if err := runKong(t, cmd, []string{"doc1", "--paragraph", "1"}, ctx, flags); err != nil {
			t.Fatalf("docs remove-paragraph --json: %v", err)
		}
	})

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("JSON parse: %v\nraw: %q", err, out)
	}
	if result["action"] != "removed" {
		t.Fatalf("expected action=removed, got: %v", result["action"])
	}
}

func TestDocsEditParagraph_TableRejected(t *testing.T) {
	origDocs := newDocsService
	t.Cleanup(func() { newDocsService = origDocs })

	// Create a server that returns a doc with a table.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"documentId": "doc1",
			"revisionId": "rev-1",
			"body": map[string]any{
				"content": []any{
					map[string]any{
						"startIndex": 0,
						"endIndex":   20,
						"table": map[string]any{
							"tableRows": []any{
								map[string]any{
									"tableCells": []any{
										map[string]any{
											"content": []any{
												map[string]any{
													"paragraph": map[string]any{
														"elements": []any{
															map[string]any{
																"textRun": map[string]any{"content": "A"},
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	docSvc, err := docs.NewService(context.Background(),
		option.WithoutAuthentication(),
		option.WithHTTPClient(srv.Client()),
		option.WithEndpoint(srv.URL+"/"),
	)
	if err != nil {
		t.Fatalf("NewDocsService: %v", err)
	}
	newDocsService = func(context.Context, string) (*docs.Service, error) { return docSvc, nil }

	flags := &RootFlags{Account: "a@b.com"}
	cmd := &DocsEditParaCmd{}
	err = runKong(t, cmd, []string{"doc1", "--paragraph", "1", "--text", "X"}, newDocsCmdContext(t), flags)
	if err == nil || !strings.Contains(err.Error(), "not a paragraph") {
		t.Fatalf("expected 'not a paragraph' error, got: %v", err)
	}
}
