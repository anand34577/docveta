package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeLLM is a tiny OpenAI-compatible server: deterministic "embeddings" (bag of words
// with a few synonyms) and canned chat answers driven by what the prompt contains.
type fakeLLM struct {
	srv         *httptest.Server
	chatCalls   atomic.Int32
	schemaCalls atomic.Int32 // requests that asked for a strict json_schema (this server refuses them)
}

var synonyms = map[string]string{"power": "electricity", "consumption": "usage", "invoice": "bill"}

func embedText(s string) []float64 {
	v := make([]float64, 64)
	for _, w := range regexp.MustCompile(`[a-z]+`).FindAllString(strings.ToLower(s), -1) {
		if syn, ok := synonyms[w]; ok {
			w = syn
		}
		h := 0
		for _, c := range w {
			h = (h*31 + int(c)) % 64
		}
		v[h]++
	}
	return v
}

func newFakeLLM(t *testing.T) *fakeLLM {
	f := &fakeLLM{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "fake-chat"}, {"id": "fake-embed"}}})
	})
	mux.HandleFunc("POST /v1/embeddings", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Input []string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		var data []map[string]any
		for i, s := range in.Input {
			data = append(data, map[string]any{"index": i, "embedding": embedText(s)})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	})
	mux.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		f.chatCalls.Add(1)
		var in struct {
			Messages       []struct{ Role, Content string } `json:"messages"`
			Stream         bool
			ResponseFormat struct {
				Type string `json:"type"`
			} `json:"response_format"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.ResponseFormat.Type == "json_schema" {
			f.schemaCalls.Add(1)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"response_format json_schema is not supported"}}`))
			return
		}
		sys, user := in.Messages[0].Content, in.Messages[len(in.Messages)-1].Content
		reply := "I'm not sure."
		switch {
		case strings.Contains(sys, "organise their scanned documents"):
			reply = classifyReply(user)
		case strings.Contains(sys, "SOURCES"):
			reply = "Your electricity bill for August was 2,860 rupees [1]."
		}
		if in.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			words := strings.SplitAfter(reply, " ")
			for _, wd := range words {
				b, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"delta": map[string]string{"content": wd}}}})
				fmt.Fprintf(w, "data: %s\n\n", b)
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": reply}}}})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func classifyReply(prompt string) string {
	out := map[string]any{"title": nil, "document_date": nil, "correspondent": nil, "document_type": nil, "tags": []any{}, "custom_fields": []any{}}
	if strings.Contains(prompt, "BESCOM") {
		out["correspondent"] = map[string]any{"name": "BESCOM", "confidence": 0.95}
		out["document_type"] = map[string]any{"name": "Bill", "confidence": 0.7}
		out["tags"] = []any{map[string]any{"name": "Utilities", "confidence": 0.9}, map[string]any{"name": "Invented tag", "confidence": 0.99}}
		out["document_date"] = "2026-08-05"
	}
	b, _ := json.Marshal(out)
	return "Sure! Here is the JSON:\n```json\n" + string(b) + "\n```"
}

type sugDTO struct {
	ID         string          `json:"id"`
	Field      string          `json:"field"`
	Value      json.RawMessage `json:"value"`
	Confidence float64         `json:"confidence"`
}

func (e *env) suggestions(id string) []sugDTO {
	var l jsonList[sugDTO]
	e.c.do("GET", "/api/v1/documents/"+id+"/suggestions", nil, 200, &l)
	return l.Items
}

func (e *env) waitFor(what string, ok func() bool) {
	e.c.t.Helper()
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(150 * time.Millisecond) {
		if ok() {
			return
		}
	}
	e.c.t.Fatalf("timed out waiting for %s", what)
}

func TestAISuggestionsAndSearch(t *testing.T) {
	e := newEnv(t)
	c := e.c
	fake := newFakeLLM(t)

	// No provider yet: documents become ready without any suggestions.
	c.do("POST", "/api/v1/tags", map[string]any{"space_id": e.family, "name": "Utilities"}, 201, nil)
	d0 := c.upload(e.family, "plain.pdf", minimalPDF("BESCOM electricity bill August with usage of 342 units"))
	c.waitStatus(d0.ID, "ready")
	if got := e.suggestions(d0.ID); len(got) != 0 {
		t.Fatalf("suggestions without a provider: %+v", got)
	}

	// Providers are admin-only and validated.
	c.do("POST", "/api/v1/admin/ai/providers", map[string]any{"name": "Bad", "base_url": "not a url"}, 422, nil)
	var prov struct {
		ID        string `json:"id"`
		IsDefault bool   `json:"is_default"`
		HasKey    bool   `json:"has_api_key"`
	}
	c.do("POST", "/api/v1/admin/ai/providers", map[string]any{"name": "Fake", "base_url": fake.srv.URL + "/v1", "api_key": "sk-test",
		"chat_model": "fake-chat", "embedding_model": "fake-embed"}, 201, &prov)
	if !prov.IsDefault || !prov.HasKey {
		t.Fatalf("provider: %+v", prov)
	}
	var tr struct {
		OK     bool     `json:"ok"`
		Models []string `json:"models"`
		Chat   *bool    `json:"chat_model_found"`
	}
	c.do("POST", "/api/v1/admin/ai/providers/"+prov.ID+"/test", nil, 200, &tr)
	if !tr.OK || len(tr.Models) != 2 || tr.Chat == nil || !*tr.Chat {
		t.Fatalf("test connection: %+v", tr)
	}
	c.do("POST", "/api/v1/admin/ai/providers", map[string]any{"name": "Fake", "base_url": fake.srv.URL}, 409, nil)

	// Spaces default to AI off. "Local only" refuses a provider that isn't marked local.
	c.do("POST", "/api/v1/documents/"+d0.ID+"/ai", nil, 204, nil)
	time.Sleep(time.Second)
	if got := e.suggestions(d0.ID); len(got) != 0 {
		t.Fatalf("AI ran in a space where it is off: %+v", got)
	}
	c.do("PATCH", "/api/v1/spaces/"+e.family, map[string]any{"ai_policy": "local_only"}, 200, nil)
	c.do("POST", "/api/v1/documents/"+d0.ID+"/ai", nil, 204, nil)
	time.Sleep(time.Second)
	if got := e.suggestions(d0.ID); len(got) != 0 {
		t.Fatalf("a non-local provider saw a local-only space: %+v", got)
	}
	c.do("PATCH", "/api/v1/admin/ai/providers/"+prov.ID, map[string]any{"is_local": true}, 200, nil)

	// Suggest mode: everything waits for a person.
	c.do("POST", "/api/v1/documents/"+d0.ID+"/ai", nil, 204, nil)
	e.waitFor("suggestions", func() bool { return len(e.suggestions(d0.ID)) > 0 })
	got := e.suggestions(d0.ID)
	byField := map[string]sugDTO{}
	tagNames := 0
	for _, s := range got {
		byField[s.Field] = s
		if s.Field == "tag" {
			tagNames++
			if !strings.Contains(string(s.Value), "Utilities") {
				t.Errorf("tag suggestion outside the vocabulary: %s", s.Value)
			}
		}
	}
	if tagNames != 1 { // "Invented tag" doesn't exist, so it must be dropped
		t.Errorf("tag suggestions: %d, want 1 (%+v)", tagNames, got)
	}
	for _, f := range []string{"correspondent", "document_type", "document_date"} {
		if _, ok := byField[f]; !ok {
			t.Errorf("missing %s suggestion in %+v", f, got)
		}
	}
	if fake.schemaCalls.Load() == 0 {
		t.Error("the strict-schema request wasn't tried first")
	}
	var d docDTO
	c.do("GET", "/api/v1/documents/"+d0.ID, nil, 200, &d)
	if d.Tags != nil && len(d.Tags) > 0 || d.DocumentDate != nil && *d.DocumentDate != "" {
		// the PDF's own text may have set the date by rule; tags must not be applied yet
		if len(d.Tags) > 0 {
			t.Errorf("suggestions were applied without being accepted: %+v", d.Tags)
		}
	}

	// Accept the correspondent, reject the rest.
	c.do("POST", "/api/v1/documents/"+d0.ID+"/suggestions/accept", map[string]any{"ids": []string{byField["correspondent"].ID}}, 204, nil)
	c.do("POST", "/api/v1/documents/"+d0.ID+"/suggestions/reject", map[string]any{}, 204, nil)
	var full struct {
		Correspondent *struct{ Name string }  `json:"correspondent"`
		Tags          []struct{ Name string } `json:"tags"`
		Suggestions   int                     `json:"suggestion_count"`
	}
	c.do("GET", "/api/v1/documents/"+d0.ID, nil, 200, &full)
	if full.Correspondent == nil || full.Correspondent.Name != "BESCOM" || len(full.Tags) != 0 || full.Suggestions != 0 {
		t.Fatalf("after resolving: %+v", full)
	}
	var st struct {
		Accepted int     `json:"accepted"`
		Rejected int     `json:"rejected"`
		Rate     float64 `json:"accept_rate"`
	}
	c.do("GET", "/api/v1/spaces/"+e.family+"/ai-stats", nil, 200, &st)
	if st.Accepted != 1 || st.Rejected < 2 || st.Rate <= 0 || st.Rate >= 1 {
		t.Errorf("stats: %+v", st)
	}

	// Automatic mode applies confident suggestions; the less certain ones wait.
	c.do("PATCH", "/api/v1/spaces/"+e.family, map[string]any{"ai_apply_mode": "auto"}, 200, nil)
	d1 := c.upload(e.family, "second.pdf", minimalPDF("BESCOM power usage statement for September, bill number 77"))
	c.waitStatus(d1.ID, "ready")
	e.waitFor("auto-applied correspondent", func() bool {
		c.do("GET", "/api/v1/documents/"+d1.ID, nil, 200, &full)
		return full.Correspondent != nil
	})
	if full.Correspondent.Name != "BESCOM" || len(full.Tags) != 1 {
		t.Errorf("auto-apply: %+v", full)
	}
	left := e.suggestions(d1.ID)
	fields := map[string]bool{}
	for _, s := range left {
		fields[s.Field] = true
	}
	if len(left) != 2 || !fields["document_type"] || !fields["document_date"] { // confidence 0.7 and 0.8 are below 0.85
		t.Errorf("uncertain suggestions should wait: %+v", left)
	}

	// Meaning-based search: "power consumption" shares no words with "electricity usage".
	var embedded int
	e.waitFor("embeddings", func() bool {
		_ = e.a.Pool.QueryRow(e.ctx, `SELECT count(DISTINCT document_id) FROM embeddings`).Scan(&embedded)
		return embedded >= 2
	})
	var res struct {
		Mode  string   `json:"mode"`
		Items []docDTO `json:"items"`
	}
	c.do("GET", "/api/v1/documents?q=power+consumption&mode=keyword", nil, 200, &res)
	if len(res.Items) != 0 {
		t.Fatalf("keyword search shouldn't understand synonyms: %+v", res.Items)
	}
	c.do("GET", "/api/v1/documents?q=power+consumption&mode=hybrid", nil, 200, &res)
	if res.Mode != "hybrid" || len(res.Items) < 2 {
		t.Fatalf("hybrid search: mode=%s items=%d", res.Mode, len(res.Items))
	}
	c.do("GET", "/api/v1/documents?q=power+consumption&mode=semantic&tag_id=00000000-0000-4000-8000-000000000000", nil, 200, &res)
	if len(res.Items) != 0 {
		t.Errorf("semantic results ignored the filters: %d items", len(res.Items))
	}
	c.do("GET", "/api/v1/documents?q=x&mode=magic", nil, 422, nil)

	// Similar documents.
	var sim jsonList[struct {
		Document docDTO  `json:"document"`
		Reason   string  `json:"reason"`
		Score    float64 `json:"score"`
	}]
	c.do("GET", "/api/v1/documents/"+d1.ID+"/similar", nil, 200, &sim)
	if len(sim.Items) == 0 || sim.Items[0].Document.ID == d1.ID || sim.Items[0].Reason != "meaning" || math.IsNaN(sim.Items[0].Score) {
		t.Fatalf("similar: %+v", sim.Items)
	}

	// Ask: streamed, cited, remembered.
	var citeDoc string
	var convID string
	events := ask(t, c, map[string]any{"question": "What was my power consumption bill?"})
	if len(events["citations"]) != 1 || events["done"] == nil {
		t.Fatalf("ask events: %+v", events)
	}
	var cites []struct {
		DocumentID string `json:"document_id"`
		Page       int    `json:"page"`
	}
	_ = json.Unmarshal([]byte(events["citations"][0]), &cites)
	if len(cites) == 0 || cites[0].Page != 1 {
		t.Fatalf("citations: %s", events["citations"][0])
	}
	citeDoc = cites[0].DocumentID
	if citeDoc != d0.ID && citeDoc != d1.ID {
		t.Errorf("cited a document that doesn't match: %s", citeDoc)
	}
	if !strings.Contains(strings.Join(events["delta"], ""), "[1]") {
		t.Errorf("answer has no citation marker: %v", events["delta"])
	}
	var done struct {
		Conv string `json:"conversation_id"`
	}
	_ = json.Unmarshal([]byte(events["done"][0]), &done)
	convID = done.Conv

	// Nothing relevant: it says so and never calls the model.
	before := fake.chatCalls.Load()
	events = ask(t, c, map[string]any{"question": "zebra migration patterns", "conversation_id": convID})
	if !strings.Contains(strings.Join(events["delta"], ""), "couldn't find") || fake.chatCalls.Load() != before {
		t.Errorf("unanswerable question: %v (chat calls %d→%d)", events["delta"], before, fake.chatCalls.Load())
	}
	var msgs jsonList[struct {
		Role string `json:"role"`
	}]
	c.do("GET", "/api/v1/ai/conversations/"+convID+"/messages", nil, 200, &msgs)
	if len(msgs.Items) != 4 {
		t.Errorf("conversation has %d messages, want 4", len(msgs.Items))
	}
	var convs jsonList[struct{ ID string }]
	c.do("GET", "/api/v1/ai/conversations", nil, 200, &convs)
	if len(convs.Items) != 1 {
		t.Errorf("conversations: %+v", convs.Items)
	}
	// Conversations are private.
	c.do("POST", "/api/v1/admin/users", map[string]any{"email": "kid@example.com", "display_name": "Kid", "password": "another-long-pw"}, 201, nil)
	kid := newClient(t, e.srv.URL)
	kid.do("POST", "/api/v1/auth/login", map[string]any{"email": "kid@example.com", "password": "another-long-pw"}, 200, nil)
	kid.do("GET", "/api/v1/ai/conversations/"+convID+"/messages", nil, 404, nil)
	kid.do("DELETE", "/api/v1/ai/conversations/"+convID, nil, 404, nil)
	kid.do("GET", "/api/v1/admin/ai/providers", nil, 403, nil)
	c.do("DELETE", "/api/v1/ai/conversations/"+convID, nil, 204, nil)

	// AI off everywhere (the personal space is off by default): Ask explains instead of failing mysteriously.
	c.do("PATCH", "/api/v1/spaces/"+e.family, map[string]any{"ai_policy": "off"}, 200, nil)
	var problem struct {
		Code string `json:"code"`
	}
	c.do("POST", "/api/v1/ai/ask", map[string]any{"question": "anything"}, 503, &problem)
	if problem.Code != "ai_off" {
		t.Errorf("ask with AI off: %+v", problem)
	}
}

// ask posts a question and returns the server-sent events by name.
func ask(t *testing.T, c *client, body map[string]any) map[string][]string {
	t.Helper()
	b := c.raw("POST", "/api/v1/ai/ask", bytes.NewReader(mustJSON(body)), map[string]string{"Content-Type": "application/json"}, 200)
	out := map[string][]string{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var event string
	for sc.Scan() {
		line := sc.Text()
		if v, ok := strings.CutPrefix(line, "event: "); ok {
			event = v
		} else if v, ok := strings.CutPrefix(line, "data: "); ok {
			if event == "delta" && strings.HasPrefix(v, `"`) {
				var s string
				_ = json.Unmarshal([]byte(v), &s)
				v = s
			}
			out[event] = append(out[event], v)
		}
	}
	return out
}
