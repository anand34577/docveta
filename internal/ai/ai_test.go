package ai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

func TestChunkPagesTokenBudget(t *testing.T) {
	english := strings.Repeat("The premium of policy 4455 is due on 5 August. ", 200)
	hindi := strings.Repeat("बिजली का बिल अगस्त महीने का है और राशि जमा करनी है। ", 120)
	chunks := chunkPages(map[int]string{1: english, 2: hindi, 3: "too short"})
	if len(chunks) == 0 {
		t.Fatal("no chunks")
	}
	pages := map[int]int{}
	for _, c := range chunks {
		pages[c.page]++
		cost := 0.0
		for _, r := range c.text {
			cost += tokenCost(r)
		}
		if cost > chunkTokens+1 {
			t.Errorf("page %d chunk is %.0f tokens, over the %d budget", c.page, cost, chunkTokens)
		}
	}
	if pages[3] != 0 || pages[1] < 2 || pages[2] < 2 {
		t.Fatalf("chunks per page: %v", pages)
	}
	// Hindi uses far more tokens per character, so the same characters make more chunks.
	if utf8.RuneCountInString(hindi) < utf8.RuneCountInString(english) && pages[2] <= pages[1]/2 {
		t.Errorf("Hindi page wasn't split by tokens: %v", pages)
	}
	// Chunks end at sentence breaks where possible.
	for _, c := range chunks[:len(chunks)-1] {
		if c.page == 1 && !strings.HasSuffix(c.text, ".") {
			t.Errorf("English chunk doesn't end at a sentence: …%q", c.text[max(0, len(c.text)-20):])
			break
		}
	}
}

func TestThinkFilter(t *testing.T) {
	cases := []struct {
		pieces []string
		want   string
	}{
		{[]string{"Hello ", "world"}, "Hello world"},
		{[]string{"<think>plan the answer</think>The bill is ₹1,842 [1]."}, "The bill is ₹1,842 [1]."},
		{[]string{"<thi", "nk>secret", " reasoning</th", "ink>Answer"}, "Answer"},
		{[]string{"<thinking>x</thinking>A", " < B"}, "A < B"},
		{[]string{"a <", "b>"}, "a <b>"},
		{[]string{"<think>never closed"}, ""},
	}
	for i, c := range cases {
		var f thinkFilter
		var got strings.Builder
		for _, p := range c.pieces {
			got.WriteString(f.Write(p))
		}
		got.WriteString(f.Flush())
		if got.String() != c.want {
			t.Errorf("case %d: got %q, want %q", i, got.String(), c.want)
		}
	}
}

// sse serves an OpenAI-style stream of the given data lines.
func sse(t *testing.T, lines []string, pause time.Duration) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, l := range lines {
			fmt.Fprintf(w, "data: %s\n\n", l)
			w.(http.Flusher).Flush()
		}
		if pause > 0 {
			select {
			case <-time.After(pause):
			case <-r.Context().Done():
			}
		}
	}))
	t.Cleanup(srv.Close)
	return NewClient(Provider{ID: uuid.New(), BaseURL: srv.URL, ChatModel: "m", TimeoutSeconds: 30, MaxConcurrency: 2})
}

func delta(s string) string { return fmt.Sprintf(`{"choices":[{"delta":{"content":%q}}]}`, s) }

func TestChatStream(t *testing.T) {
	var got strings.Builder
	c := sse(t, []string{delta("<think>hm</think>"), delta("\n\nYour bill"), delta(" is due [1]."), "[DONE]"}, 0)
	if err := c.ChatStream(context.Background(), nil, func(s string) { got.WriteString(s) }); err != nil {
		t.Fatal(err)
	}
	if got.String() != "Your bill is due [1]." {
		t.Fatalf("got %q", got.String())
	}

	c = sse(t, []string{delta("Part"), `{"error":{"message":"model unloaded"}}`}, 0)
	if err := c.ChatStream(context.Background(), nil, func(string) {}); err == nil || !strings.Contains(err.Error(), "model unloaded") {
		t.Fatalf("error event: %v", err)
	}

	c = sse(t, []string{delta("<think>only thoughts</think>"), "[DONE]"}, 0)
	if err := c.ChatStream(context.Background(), nil, func(string) {}); !errors.Is(err, ErrEmptyAnswer) {
		t.Fatalf("empty answer: %v", err)
	}
}

func TestChatStreamStalls(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the idle timeout")
	}
	c := sse(t, []string{delta("Start")}, time.Minute)
	c.p.TimeoutSeconds = 1 // ChatStream waits at least 30 s
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	err := c.ChatStream(ctx, nil, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "stopped answering") {
		t.Fatalf("stall: %v after %s", err, time.Since(start))
	}
}

func TestBestWindow(t *testing.T) {
	page := strings.Repeat("filler words here ", 200) + "The policy number is AB-1234 and the premium is 5,000. " + strings.Repeat("more filler ", 200)
	w := bestWindow(page, []string{"premium", "policy"}, 300)
	if !strings.Contains(w, "premium is 5,000") || utf8.RuneCountInString(w) > 302 {
		t.Fatalf("window %q", w)
	}
	if got := bestWindow("short page", []string{"x"}, 300); got != "short page" {
		t.Fatalf("short: %q", got)
	}
}

func TestTrimHistory(t *testing.T) {
	hist := []Message{
		{Role: "assistant", Content: "orphan answer"},
		{Role: "user", Content: "When is the bill due?"},
		{Role: "assistant", Content: "On 5 August [1][2]."},
		{Role: "user", Content: "And the amount?"},
		{Role: "assistant", Content: "₹1,842 [1]."},
	}
	got := trimHistory(hist)
	if len(got) != 4 || got[0].Role != "user" || got[1].Content != "On 5 August." || got[3].Content != "₹1,842." {
		t.Fatalf("got %+v", got)
	}
	if lastQuestion(hist) != "And the amount?" {
		t.Fatal("last question")
	}
	long := []Message{{Role: "user", Content: strings.Repeat("x", historyChars)}, {Role: "assistant", Content: "a"}, {Role: "user", Content: "q"}, {Role: "assistant", Content: "b"}}
	if got := trimHistory(long); len(got) != 2 || got[0].Content != "q" {
		t.Fatalf("budget: %+v", got)
	}
}

func TestIsFollowUp(t *testing.T) {
	for q, want := range map[string]bool{
		"When is it due?": true, "and last year?": true, "What about the car?": true, "how about that one": true,
		"इसकी राशि क्या है?": true, "zebra migration patterns": false, "When does my car insurance expire?": false, "": false,
	} {
		if got := isFollowUp(q); got != want {
			t.Errorf("%q: got %v", q, got)
		}
	}
}

func TestTopKAndCaps(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	k := newTopK(3)
	for i, sc := range []float32{0.2, 0.9, 0.5, 0.7, 0.1} {
		k.add(SemHit{DocumentID: a, Page: i, Score: sc})
	}
	got := k.sorted()
	if len(got) != 3 || got[0].Score != 0.9 || got[2].Score != 0.5 {
		t.Fatalf("topK %+v", got)
	}
	hits := []SemHit{{DocumentID: a, Score: 0.9}, {DocumentID: a, Score: 0.8}, {DocumentID: a, Score: 0.7}, {DocumentID: b, Score: 0.6}, {DocumentID: b, Score: 0.2}}
	if c := capPerDocument(hits, 10, 2, 0.3); len(c) != 3 || c[2].DocumentID != b {
		t.Fatalf("cap %+v", c)
	}
	if d := bestPerDocument(hits, 10, 0.3); len(d) != 2 || d[1].Score != 0.6 {
		t.Fatalf("best per doc %+v", d)
	}
}

func TestVectorHelpers(t *testing.T) {
	if vecLiteral([]float32{0.5, -1, 0.25}) != "[0.5,-1,0.25]" {
		t.Fatal(vecLiteral([]float32{0.5, -1, 0.25}))
	}
	for v, want := range map[string]bool{"0.8.2": true, "0.7.4": false, "1.0": true, "0.10.0": true, "x": false} {
		if versionAtLeast(v, 0, 8) != want {
			t.Errorf("%s", v)
		}
	}
	if vecType(768) != "vector(768)" || vecType(3072) != "halfvec(3072)" || vecType(5000) != "vector(5000)" {
		t.Fatal("vecType")
	}
	if stripCitations("Due 5 Aug [1] and [12].") != "Due 5 Aug and." {
		t.Fatal(stripCitations("Due 5 Aug [1] and [12]."))
	}
}
