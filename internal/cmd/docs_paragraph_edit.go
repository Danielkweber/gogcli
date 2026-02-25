package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"google.golang.org/api/docs/v1"

	"github.com/steipete/gogcli/internal/outfmt"
	"github.com/steipete/gogcli/internal/ui"
)

// DocsEditParaCmd replaces the text content of a paragraph by number.
type DocsEditParaCmd struct {
	DocID     string `arg:"" name:"docId" help:"Doc ID"`
	Paragraph int    `name:"paragraph" required:"" help:"Paragraph number to edit (1-based)"`
	Text      string `name:"text" short:"t" help:"New text content"`
	File      string `name:"file" short:"f" help:"Read content from file (use - for stdin)"`
	Tab       string `name:"tab" help:"Tab title or ID"`
	Revision  string `name:"revision" help:"Required revision ID for conflict detection"`
}

func (c *DocsEditParaCmd) Run(ctx context.Context, flags *RootFlags) error {
	u := ui.FromContext(ctx)
	account, err := requireAccount(flags)
	if err != nil {
		return err
	}

	docID := strings.TrimSpace(c.DocID)
	if docID == "" {
		return usage("empty docId")
	}

	content, err := resolveContentInput(c.Text, c.File)
	if err != nil {
		return err
	}
	if content == "" {
		return usage("no content provided (use --text, --file, or stdin)")
	}

	if c.Paragraph < 1 {
		return usage("--paragraph must be >= 1")
	}

	svc, err := newDocsService(ctx, account)
	if err != nil {
		return err
	}

	pm, err := fetchAndBuildMap(ctx, svc, docID, c.Tab)
	if err != nil {
		return err
	}

	para, err := pm.get(c.Paragraph)
	if err != nil {
		return err
	}
	if para.ElemType != "paragraph" {
		return fmt.Errorf("paragraph %d is a %s, not a paragraph — use the appropriate command", c.Paragraph, para.ElemType)
	}

	// Build batch update: delete existing text, then insert new text.
	// endIndex-1 preserves the trailing \n (paragraph terminator).
	var requests []*docs.Request

	// Only delete if there's text to delete (startIndex < endIndex-1).
	if para.StartIndex < para.EndIndex-1 {
		requests = append(requests, &docs.Request{
			DeleteContentRange: &docs.DeleteContentRangeRequest{
				Range: &docs.Range{
					StartIndex: para.StartIndex,
					EndIndex:   para.EndIndex - 1,
					TabId:      pm.TabID,
				},
			},
		})
	}

	requests = append(requests, &docs.Request{
		InsertText: &docs.InsertTextRequest{
			Text:     content,
			Location: &docs.Location{Index: para.StartIndex, TabId: pm.TabID},
		},
	})

	batchReq := &docs.BatchUpdateDocumentRequest{Requests: requests}
	if c.Revision != "" {
		batchReq.WriteControl = &docs.WriteControl{
			RequiredRevisionId: c.Revision,
		}
	}

	result, err := svc.Documents.BatchUpdate(docID, batchReq).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("editing paragraph %d: %w", c.Paragraph, err)
	}

	if outfmt.IsJSON(ctx) {
		return outfmt.WriteJSON(ctx, os.Stdout, map[string]any{
			"documentId": result.DocumentId,
			"paragraph":  c.Paragraph,
			"action":     "edited",
		})
	}

	u.Out().Printf("documentId\t%s", result.DocumentId)
	u.Out().Printf("paragraph\t%d", c.Paragraph)
	u.Out().Printf("action\tedited")
	return nil
}

// DocsInsertParaCmd inserts a new paragraph after a given paragraph number.
type DocsInsertParaCmd struct {
	DocID    string `arg:"" name:"docId" help:"Doc ID"`
	After    int    `name:"after" required:"" help:"Insert after this paragraph number (0 = beginning)"`
	Text     string `name:"text" short:"t" help:"Text for the new paragraph"`
	File     string `name:"file" short:"f" help:"Read content from file (use - for stdin)"`
	Tab      string `name:"tab" help:"Tab title or ID"`
	Revision string `name:"revision" help:"Required revision ID for conflict detection"`
}

func (c *DocsInsertParaCmd) Run(ctx context.Context, flags *RootFlags) error {
	u := ui.FromContext(ctx)
	account, err := requireAccount(flags)
	if err != nil {
		return err
	}

	docID := strings.TrimSpace(c.DocID)
	if docID == "" {
		return usage("empty docId")
	}

	content, err := resolveContentInput(c.Text, c.File)
	if err != nil {
		return err
	}
	if content == "" {
		return usage("no content provided (use --text, --file, or stdin)")
	}

	if c.After < 0 {
		return usage("--after must be >= 0")
	}

	svc, err := newDocsService(ctx, account)
	if err != nil {
		return err
	}

	pm, err := fetchAndBuildMap(ctx, svc, docID, c.Tab)
	if err != nil {
		return err
	}

	// Determine insertion index.
	var insertIndex int64
	if c.After == 0 {
		// Insert at the very beginning (after initial section break).
		if len(pm.Paragraphs) == 0 {
			insertIndex = 1
		} else {
			insertIndex = pm.Paragraphs[0].StartIndex
		}
		// Insert text + newline to create a new paragraph before existing content.
		content += "\n"
	} else {
		para, lookupErr := pm.get(c.After)
		if lookupErr != nil {
			return lookupErr
		}
		// Insert \nnewText at endIndex-1 (before the trailing \n of the target paragraph).
		insertIndex = para.EndIndex - 1
		content = "\n" + content
	}

	batchReq := &docs.BatchUpdateDocumentRequest{
		Requests: []*docs.Request{{
			InsertText: &docs.InsertTextRequest{
				Text:     content,
				Location: &docs.Location{Index: insertIndex, TabId: pm.TabID},
			},
		}},
	}
	if c.Revision != "" {
		batchReq.WriteControl = &docs.WriteControl{
			RequiredRevisionId: c.Revision,
		}
	}

	result, err := svc.Documents.BatchUpdate(docID, batchReq).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("inserting paragraph after %d: %w", c.After, err)
	}

	if outfmt.IsJSON(ctx) {
		return outfmt.WriteJSON(ctx, os.Stdout, map[string]any{
			"documentId": result.DocumentId,
			"after":      c.After,
			"action":     "inserted",
		})
	}

	u.Out().Printf("documentId\t%s", result.DocumentId)
	u.Out().Printf("after\t%d", c.After)
	u.Out().Printf("action\tinserted")
	return nil
}

// DocsRemoveParaCmd deletes a paragraph by number.
type DocsRemoveParaCmd struct {
	DocID     string `arg:"" name:"docId" help:"Doc ID"`
	Paragraph int    `name:"paragraph" required:"" help:"Paragraph number to remove (1-based)"`
	Tab       string `name:"tab" help:"Tab title or ID"`
	Revision  string `name:"revision" help:"Required revision ID for conflict detection"`
}

func (c *DocsRemoveParaCmd) Run(ctx context.Context, flags *RootFlags) error {
	u := ui.FromContext(ctx)
	account, err := requireAccount(flags)
	if err != nil {
		return err
	}

	docID := strings.TrimSpace(c.DocID)
	if docID == "" {
		return usage("empty docId")
	}

	if c.Paragraph < 1 {
		return usage("--paragraph must be >= 1")
	}

	svc, err := newDocsService(ctx, account)
	if err != nil {
		return err
	}

	pm, err := fetchAndBuildMap(ctx, svc, docID, c.Tab)
	if err != nil {
		return err
	}

	para, err := pm.get(c.Paragraph)
	if err != nil {
		return err
	}

	// Determine delete range. We need to include the paragraph's newline.
	startIndex := para.StartIndex
	endIndex := para.EndIndex

	// If this is the last paragraph, we can't delete the final \n in the doc body.
	// Instead, delete from the end of the previous paragraph to our end-1.
	isLast := c.Paragraph == len(pm.Paragraphs)
	if isLast && c.Paragraph > 1 {
		prev, _ := pm.get(c.Paragraph - 1)
		startIndex = prev.EndIndex - 1
		endIndex = para.EndIndex - 1
	} else if isLast && c.Paragraph == 1 {
		// Only paragraph in the doc — just clear the text, keep the paragraph structure.
		if para.StartIndex >= para.EndIndex-1 {
			return errors.New("cannot remove the only empty paragraph in the document")
		}
		endIndex = para.EndIndex - 1
	}

	batchReq := &docs.BatchUpdateDocumentRequest{
		Requests: []*docs.Request{{
			DeleteContentRange: &docs.DeleteContentRangeRequest{
				Range: &docs.Range{
					StartIndex: startIndex,
					EndIndex:   endIndex,
					TabId:      pm.TabID,
				},
			},
		}},
	}
	if c.Revision != "" {
		batchReq.WriteControl = &docs.WriteControl{
			RequiredRevisionId: c.Revision,
		}
	}

	result, err := svc.Documents.BatchUpdate(docID, batchReq).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("removing paragraph %d: %w", c.Paragraph, err)
	}

	if outfmt.IsJSON(ctx) {
		return outfmt.WriteJSON(ctx, os.Stdout, map[string]any{
			"documentId": result.DocumentId,
			"paragraph":  c.Paragraph,
			"action":     "removed",
		})
	}

	u.Out().Printf("documentId\t%s", result.DocumentId)
	u.Out().Printf("paragraph\t%d", c.Paragraph)
	u.Out().Printf("action\tremoved")
	return nil
}

// fetchAndBuildMap fetches the document and builds a paragraph map.
func fetchAndBuildMap(ctx context.Context, svc *docs.Service, docID, tabID string) (*paragraphMap, error) {
	getCall := svc.Documents.Get(docID)
	if tabID != "" {
		getCall = getCall.IncludeTabsContent(true)
	}
	doc, err := getCall.Context(ctx).Do()
	if err != nil {
		if isDocsNotFound(err) {
			return nil, fmt.Errorf("doc not found or not a Google Doc (id=%s)", docID)
		}
		return nil, err
	}
	if doc == nil {
		return nil, errors.New("doc not found")
	}

	return buildParagraphMap(doc, tabID)
}

// get returns the paragraph at the given 1-based number.
func (pm *paragraphMap) get(num int) (*docParagraph, error) {
	if num < 1 || num > len(pm.Paragraphs) {
		return nil, fmt.Errorf("paragraph %d out of range (document has %d paragraphs)", num, len(pm.Paragraphs))
	}
	return &pm.Paragraphs[num-1], nil
}
