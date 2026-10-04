package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/platform/httpx"
	"github.com/anand34577/docveta/internal/search"
	"github.com/anand34577/docveta/internal/taxonomy"
)

// MCP (Model Context Protocol) over streamable HTTP: AI assistants such as Claude can
// search and read your documents with a personal access token (scope "documents:read").
// Only the request/response part of the transport is implemented; there is no
// server-initiated stream, so GET answers 405.
const mcpProtocol = "2025-03-26"

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

var (
	str = func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	num = func(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
)

var mcpTools = []mcpTool{
	{"search_documents", "Search the user's documents. Understands filters like tag:tax from:hdfc date:2026 and custom fields like amount:>1500. Returns titles, ids, dates and matching passages.",
		obj(map[string]any{"query": str("Search text and filters"), "space": str("Limit to a space (name)"), "limit": num("Maximum results (1-25, default 10)"),
			"mode": map[string]any{"type": "string", "enum": []string{"keyword", "semantic", "hybrid"}, "description": "hybrid also matches by meaning when AI embeddings are set up"}}, "query")},
	{"get_document", "Get a document's details: title, date, correspondent, type, tags, custom fields, page count.",
		obj(map[string]any{"id": str("Document id")}, "id")},
	{"get_document_text", "Read the text of a document (all pages, or one page). Long documents are cut at 40,000 characters.",
		obj(map[string]any{"id": str("Document id"), "page": num("Only this page (1-based)")}, "id")},
	{"find_similar_documents", "Find documents similar to a given one.", obj(map[string]any{"id": str("Document id"), "limit": num("Maximum results (default 5)")}, "id")},
	{"list_spaces", "List the spaces (Personal, Family, ...) the user can see.", obj(map[string]any{})},
	{"list_tags", "List tags, with how many documents use each.", obj(map[string]any{"space": str("Limit to a space (name)")})},
	{"list_correspondents", "List correspondents (who documents are from), with document counts.", obj(map[string]any{"space": str("Limit to a space (name)")})},
	{"list_document_types", "List document types, with document counts.", obj(map[string]any{"space": str("Limit to a space (name)")})},
}

func (a *API) registerMCP(mux router) {
	mux.HandleFunc("GET /mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", "POST")
		http.Error(w, "this MCP server answers POST requests only", http.StatusMethodNotAllowed)
	})
	mux.HandleFunc("POST /mcp", a.mcp)
}

func (a *API) mcp(w http.ResponseWriter, r *http.Request) {
	// Browsers must not be able to drive this from another site (DNS rebinding).
	if o := r.Header.Get("Origin"); o != "" && o != a.origin && o != originOf(a.publicBase(r)) {
		httpx.Error(w, r, apperr.Forbidden("Cross-origin requests aren't allowed"))
		return
	}
	p := auth.From(r.Context())
	if p == nil || p.Kind != auth.KindToken || !p.Has(auth.ScopeRead) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="docveta"`)
		httpx.Error(w, r, apperr.Unauthorized("Use a personal access token with the \"Read documents\" permission"))
		return
	}
	var req rpcRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		reply(w, nil, nil, &rpcError{Code: -32700, Message: "Parse error"})
		return
	}
	if len(req.ID) == 0 { // a notification: nothing to answer
		w.WriteHeader(http.StatusAccepted)
		return
	}
	switch req.Method {
	case "initialize":
		reply(w, req.ID, map[string]any{"protocolVersion": mcpProtocol, "capabilities": map[string]any{"tools": map[string]any{}},
			"serverInfo":   map[string]any{"name": "docveta", "version": a.Version},
			"instructions": "Docveta holds the user's documents (bills, IDs, contracts...). Use search_documents to find them and get_document_text to read them."}, nil)
	case "ping":
		reply(w, req.ID, map[string]any{}, nil)
	case "tools/list":
		reply(w, req.ID, map[string]any{"tools": mcpTools}, nil)
	case "tools/call":
		var call struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &call); err != nil {
			reply(w, req.ID, nil, &rpcError{Code: -32602, Message: "Invalid params"})
			return
		}
		text, err := a.callTool(r, p, call.Name, call.Arguments)
		if err != nil {
			if err == errUnknownTool {
				reply(w, req.ID, nil, &rpcError{Code: -32602, Message: "Unknown tool " + call.Name})
				return
			}
			msg := "Something went wrong."
			if ae, ok := apperr.As(err); ok && ae.Kind != apperr.KindInternal {
				msg = ae.Msg
			} else {
				httpx.Logger(r.Context()).Error("mcp tool failed", "tool", call.Name, "err", err)
			}
			reply(w, req.ID, map[string]any{"isError": true, "content": []map[string]string{{"type": "text", "text": msg}}}, nil)
			return
		}
		reply(w, req.ID, map[string]any{"content": []map[string]string{{"type": "text", "text": text}}}, nil)
	default:
		reply(w, req.ID, nil, &rpcError{Code: -32601, Message: "Method not found"})
	}
}

func reply(w http.ResponseWriter, id json.RawMessage, result any, e *rpcError) {
	if id == nil {
		id = json.RawMessage("null")
	}
	httpx.JSON(w, http.StatusOK, rpcResponse{JSONRPC: "2.0", ID: id, Result: result, Error: e})
}

var errUnknownTool = fmt.Errorf("unknown tool")

func jsonText(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	return string(b), err
}

func (a *API) spaceByName(r *http.Request, p *auth.Principal, name string) (*uuid.UUID, error) {
	if strings.TrimSpace(name) == "" {
		return nil, nil
	}
	sp, err := a.Spaces.List(r.Context(), p)
	if err != nil {
		return nil, err
	}
	for _, s := range sp {
		if strings.EqualFold(s.Name, name) || (s.Kind == "personal" && strings.EqualFold(name, "personal")) {
			return &s.ID, nil
		}
	}
	return nil, apperr.Invalid("space", "No space called \""+name+"\"")
}

func (a *API) callTool(r *http.Request, p *auth.Principal, name string, raw json.RawMessage) (string, error) {
	var args struct {
		Query string `json:"query"`
		Space string `json:"space"`
		Limit int    `json:"limit"`
		Mode  string `json:"mode"`
		ID    string `json:"id"`
		Page  int    `json:"page"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", apperr.Invalid("arguments", "Invalid arguments")
		}
	}
	ctx := r.Context()
	docID := func() (uuid.UUID, error) {
		id, err := uuid.Parse(args.ID)
		if err != nil {
			return id, apperr.Invalid("id", "That isn't a document id")
		}
		return id, nil
	}
	switch name {
	case "search_documents":
		if strings.TrimSpace(args.Query) == "" {
			return "", apperr.Invalid("query", "Give something to search for")
		}
		q := search.Query{Q: args.Query, Limit: min(max(args.Limit, 1), 25), WithTotal: true, Mode: args.Mode}
		if args.Limit == 0 {
			q.Limit = 10
		}
		if sp, err := a.spaceByName(r, p, args.Space); err != nil {
			return "", err
		} else if sp != nil {
			q.SpaceIDs = []uuid.UUID{*sp}
		}
		res, err := a.listDocuments(r, p, q)
		if err != nil {
			return "", err
		}
		type hit struct {
			ID            string   `json:"id"`
			Title         string   `json:"title"`
			Date          *string  `json:"date"`
			Correspondent string   `json:"correspondent,omitempty"`
			Type          string   `json:"type,omitempty"`
			Tags          []string `json:"tags"`
			Space         string   `json:"space"`
			Page          *int     `json:"matched_page,omitempty"`
			Excerpt       string   `json:"excerpt,omitempty"`
		}
		out := []hit{}
		for _, d := range res.Items {
			h := hit{ID: d.ID.String(), Title: d.Title, Date: d.DocumentDate, Space: d.Space.Name, Page: d.MatchedPage, Tags: []string{}}
			if d.Correspondent != nil {
				h.Correspondent = d.Correspondent.Name
			}
			if d.DocumentType != nil {
				h.Type = d.DocumentType.Name
			}
			for _, t := range d.Tags {
				h.Tags = append(h.Tags, t.Name)
			}
			for _, s := range d.Snippet {
				h.Excerpt += s.Text
			}
			out = append(out, h)
		}
		return jsonText(map[string]any{"total": res.Total, "results": out})
	case "get_document":
		id, err := docID()
		if err != nil {
			return "", err
		}
		d, err := a.Documents.Get(ctx, p, id)
		if err != nil {
			return "", err
		}
		return jsonText(d)
	case "get_document_text":
		id, err := docID()
		if err != nil {
			return "", err
		}
		pages, err := a.Documents.Pages(ctx, p, id)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		for _, pg := range pages {
			if args.Page > 0 && pg.PageNo != args.Page {
				continue
			}
			fmt.Fprintf(&b, "--- page %d ---\n%s\n", pg.PageNo, pg.Text)
			if b.Len() > 40000 {
				b.WriteString("\n[…cut: the document is longer. Ask for a specific page.]")
				break
			}
		}
		if b.Len() == 0 {
			return "This document has no text yet (it may still be processing, or have no readable text).", nil
		}
		return b.String(), nil
	case "find_similar_documents":
		id, err := docID()
		if err != nil {
			return "", err
		}
		l, err := a.AI.Similar(ctx, a.Documents, p, id, min(max(args.Limit, 1), 20))
		if err != nil {
			return "", err
		}
		type sim struct {
			ID    string  `json:"id"`
			Title string  `json:"title"`
			Score float32 `json:"score"`
		}
		out := []sim{}
		for _, s := range l {
			out = append(out, sim{s.Document.ID.String(), s.Document.Title, s.Score})
		}
		return jsonText(out)
	case "list_spaces":
		sp, err := a.Spaces.List(ctx, p)
		if err != nil {
			return "", err
		}
		type row struct {
			Name      string `json:"name"`
			Kind      string `json:"kind"`
			Role      string `json:"your_role"`
			Documents int    `json:"documents"`
		}
		out := []row{}
		for _, s := range sp {
			out = append(out, row{s.Name, s.Kind, string(s.Role), s.DocumentCount})
		}
		return jsonText(out)
	case "list_tags", "list_correspondents", "list_document_types":
		kind := map[string]taxonomy.Kind{"list_tags": taxonomy.Tags, "list_correspondents": taxonomy.Correspondents, "list_document_types": taxonomy.DocumentTypes}[name]
		sp, err := a.spaceByName(r, p, args.Space)
		if err != nil {
			return "", err
		}
		l, err := a.Taxonomy.List(ctx, p, kind, sp)
		if err != nil {
			return "", err
		}
		type row struct {
			Name      string `json:"name"`
			Documents int    `json:"documents"`
		}
		out := []row{}
		for _, it := range l {
			out = append(out, row{it.Name, it.DocumentCount})
		}
		return jsonText(out)
	}
	return "", errUnknownTool
}
