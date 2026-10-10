package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/platform/db"
	"github.com/anand34577/docveta/internal/textindex"
)

// Citation points at the passage an answer relies on.
type Citation struct {
	N          int       `json:"n"`
	DocumentID uuid.UUID `json:"document_id"`
	Title      string    `json:"title"`
	Page       int       `json:"page"`
	Snippet    string    `json:"snippet"`
}

type ConversationMessage struct { // a stored conversation message
	ID        uuid.UUID  `json:"id"`
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	Citations []Citation `json:"citations"`
	CreatedAt time.Time  `json:"created_at"`
}

type Conversation struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AskInput struct {
	Question       string      `json:"question"`
	ConversationID *uuid.UUID  `json:"conversation_id"`
	SpaceIDs       []uuid.UUID `json:"space_ids"`
	// DocumentIDs limits the question to these documents ("ask about this document").
	DocumentIDs []uuid.UUID `json:"document_ids"`
}

const (
	maxQuestion  = 2000 // characters
	passageCap   = 4000 // characters of one page read whole ("summarise this")
	passageChars = 1600 // a keyword hit is cut to this window around the matching words
	historyTurns = 6    // earlier messages sent along for follow-up questions
)

var stop = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a an and are as at be but by can did do does for from had has have how i if in into is it its me my of on or our
		so than that the their them then there these they this to was we were what when where which who whom why will with would you your about
		tell show give find please much many any document documents doc docs paper papers`) {
		stop[w] = true
	}
}

// questionTerms are the words of a question worth searching for.
func questionTerms(q string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range textindex.Terms(q) {
		if stop[t] || utf8.RuneCountInString(t) < 2 || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// keywordQuery turns a question into an OR query over our lexemes: "what is my
// electricity bill" → 'electricity' | 'bill'. (Search's AND would find nothing.)
func keywordQuery(q string) string {
	var parts []string
	for _, t := range questionTerms(q) {
		parts = append(parts, textindex.TSQuery([]textindex.QueryPart{{Terms: []string{t}}}))
	}
	return strings.Join(parts, " | ")
}

// bestWindow returns the part of a page (about size characters) with the most question
// words, so a long page contributes its relevant paragraph instead of everything.
func bestWindow(text string, terms []string, size int) string {
	r := []rune(text)
	if len(r) <= size {
		return text
	}
	lower := []rune(strings.ToLower(text))
	if len(lower) != len(r) { // case folding changed the length (rare scripts): fall back to the start
		return string(r[:size])
	}
	hits := make([]int, 0, 32)
	low := string(lower)
	for _, t := range terms {
		for off := 0; ; {
			i := strings.Index(low[off:], t)
			if i < 0 {
				break
			}
			hits = append(hits, utf8.RuneCountInString(low[:off+i]))
			off += i + len(t)
		}
	}
	if len(hits) == 0 {
		return string(r[:size])
	}
	sort.Ints(hits)
	bestStart, bestCount := 0, 0
	for i, h := range hits {
		n := sort.SearchInts(hits[i:], h+size)
		if n > bestCount {
			bestCount, bestStart = n, h
		}
	}
	start := max(0, bestStart-size/5) // a little context before the first hit
	end := min(len(r), start+size)
	start = max(0, end-size)
	// Begin and end on word boundaries.
	for start > 0 && start < end && r[start-1] != ' ' && r[start-1] != '\n' {
		start++
	}
	for end < len(r) && end > start && r[end] != ' ' && r[end] != '\n' {
		end--
	}
	out := strings.TrimSpace(string(r[start:end]))
	if start > 0 {
		out = "…" + out
	}
	if end < len(r) {
		out += "…"
	}
	return out
}

type source struct {
	doc   uuid.UUID
	page  int
	text  string
	score float64
}

type docInfo struct {
	title, from, typ, date, tags string
}

// retrieve finds the passages to answer from: keyword matches on the pages plus, when
// embeddings exist, meaning-based matches, fused with reciprocal rank fusion. docs limits
// the search to those documents.
func (s *Service) retrieve(ctx context.Context, question string, spaceIDs, docs []uuid.UUID, semantic bool, maxSources, perDocSources int) ([]source, error) {
	type key struct {
		doc  uuid.UUID
		page int
	}
	scores := map[key]float64{}
	texts := map[key]string{}
	var bestSemantic float32
	keywordHits := 0
	if docs == nil {
		docs = []uuid.UUID{}
	}

	if tsq := keywordQuery(question); tsq != "" {
		terms := questionTerms(question)
		rows, err := s.pool.Query(ctx, `SELECT pg.document_id, pg.page_no, pg.text
			FROM pages pg JOIN documents d ON d.id=pg.document_id
			WHERE d.space_id = ANY($1) AND d.deleted_at IS NULL AND (cardinality($3::uuid[]) = 0 OR d.id = ANY($3))
			  AND pg.fts @@ ($2::text::tsquery || to_tsquery('english', $2::text))
			ORDER BY ts_rank(pg.fts, ($2::text::tsquery || to_tsquery('english', $2::text))) DESC LIMIT 16`, spaceIDs, tsq, docs)
		if err != nil {
			return nil, err
		}
		rank := 0
		for rows.Next() {
			var id uuid.UUID
			var page int
			var text string
			if err := rows.Scan(&id, &page, &text); err != nil {
				rows.Close()
				return nil, err
			}
			k := key{id, page}
			scores[k] += 1 / float64(60+rank)
			texts[k] = bestWindow(text, terms, passageChars)
			rank++
			keywordHits++
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	if semantic {
		// Meaning-based search is a bonus: when the embedding server is down, keywords still answer.
		if hits, err := s.Passages(ctx, question, spaceIDs, docs, 2*maxSources, perDocSources); err == nil {
			for i, h := range hits {
				k := key{h.DocumentID, h.Page}
				scores[k] += 1 / float64(60+i)
				texts[k] = h.Text // the focused chunk is better context than a keyword window
				if i == 0 {
					bestSemantic = h.Score
				}
			}
		}
	}
	// Only a weak meaning-based match and no keyword hit at all is more likely noise than an answer.
	if keywordHits == 0 && bestSemantic > 0 && bestSemantic < 0.4 && len(docs) == 0 {
		return nil, nil
	}
	var out []source
	for k, sc := range scores {
		out = append(out, source{doc: k.doc, page: k.page, text: texts[k], score: sc})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].page < out[j].page
	})
	perDoc := map[uuid.UUID]int{}
	kept := out[:0]
	for _, sc := range out {
		if perDoc[sc.doc] >= perDocSources {
			continue
		}
		perDoc[sc.doc]++
		kept = append(kept, sc)
		if len(kept) == maxSources {
			break
		}
	}
	out = kept
	// Asked about specific documents but nothing matched the words ("summarise this"): read
	// their first pages instead.
	if len(out) == 0 && len(docs) > 0 {
		rows, err := s.pool.Query(ctx, `SELECT pg.document_id, pg.page_no, pg.text FROM pages pg JOIN documents d ON d.id=pg.document_id
			WHERE pg.document_id = ANY($1) AND d.space_id = ANY($2) AND d.deleted_at IS NULL AND length(pg.text) > 0
			ORDER BY pg.document_id, pg.page_no LIMIT $3`, docs, spaceIDs, maxSources)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var sc source
			if err := rows.Scan(&sc.doc, &sc.page, &sc.text); err != nil {
				rows.Close()
				return nil, err
			}
			if r := []rune(sc.text); len(r) > passageCap {
				sc.text = string(r[:passageCap]) + "…"
			}
			out = append(out, sc)
		}
		rows.Close()
	}
	return out, nil
}

// describe loads what the model should know about each source document.
func (s *Service) describe(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]docInfo, error) {
	rows, err := s.pool.Query(ctx, `SELECT d.id, d.title, coalesce(c.name,''), coalesce(t.name,''), coalesce(to_char(d.document_date,'YYYY-MM-DD'),''),
			coalesce((SELECT string_agg(tg.name, ', ' ORDER BY tg.name) FROM document_tags dt JOIN tags tg ON tg.id=dt.tag_id WHERE dt.document_id=d.id), '')
		FROM documents d LEFT JOIN correspondents c ON c.id=d.correspondent_id LEFT JOIN document_types t ON t.id=d.document_type_id
		WHERE d.id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]docInfo{}
	for rows.Next() {
		var id uuid.UUID
		var i docInfo
		if err := rows.Scan(&id, &i.title, &i.from, &i.typ, &i.date, &i.tags); err != nil {
			return nil, err
		}
		out[id] = i
	}
	return out, rows.Err()
}

func snippetOf(text string) string {
	t := strings.Join(strings.Fields(text), " ")
	r := []rune(t)
	if len(r) > 240 {
		return string(r[:240]) + "…"
	}
	return t
}

var citeMarker = regexp.MustCompile(`\s?\[\d{1,2}\]`)

// stripCitations removes [n] markers from an earlier answer: in a new turn the numbers point
// at different sources and would mislead the model.
func stripCitations(s string) string { return citeMarker.ReplaceAllString(s, "") }

const systemPrompt = `You are Docveta's assistant. You answer questions about the user's own documents (bills, IDs, contracts, policies, certificates, letters) using ONLY the numbered sources below.
Rules:
- Every fact must come from the sources. Cite the source number in square brackets right after it, like [1] or [2][3].
- If the sources don't contain the answer, say plainly that you couldn't find it in the documents, and mention what you did find if it is related. Never invent names, amounts, numbers or dates.
- Quote amounts, dates, policy and account numbers exactly as written.
- Today's date is %s; use it for questions like "is it expired" or "how long until".
- Be concise and direct. Use short paragraphs or a list; use a table only when comparing several documents.
- Reply in the language of the question.`

// Ask answers a question from the user's documents, streaming the answer through emit.
// Events: "status" ({stage}), "citations" ([]Citation), "delta" (string),
// "done" ({conversation_id, message_id}). If nothing relevant is found it says so without
// calling the model. When the client goes away mid-answer, what was written is kept.
func (s *Service) Ask(ctx context.Context, p *auth.Principal, in AskInput, emit func(event string, data any)) error {
	q := strings.TrimSpace(in.Question)
	if q == "" || utf8.RuneCountInString(q) > maxQuestion {
		return apperr.Invalid("question", fmt.Sprintf("Ask a question of up to %d characters", maxQuestion))
	}
	if len(in.DocumentIDs) > 50 {
		return apperr.Invalid("document_ids", "Ask about at most 50 documents at a time")
	}
	visible, err := s.spaces.VisibleSpaceIDs(ctx, p.UserID)
	if err != nil {
		return err
	}
	if len(in.SpaceIDs) > 0 {
		var keep []uuid.UUID
		for _, id := range in.SpaceIDs {
			for _, v := range visible {
				if v == id {
					keep = append(keep, id)
				}
			}
		}
		visible = keep
	}
	allowed, prov, client, err := s.allowedSpaces(ctx, visible, false)
	if err != nil {
		switch {
		case errors.Is(err, ErrOff):
			return &apperr.Error{Kind: apperr.KindUnavailable, Code: "ai_off", Msg: "AI is turned off for these spaces (or they only allow AI on your own hardware). A space owner can change this in the space's settings."}
		case errors.Is(err, ErrNoModel):
			return &apperr.Error{Kind: apperr.KindUnavailable, Code: "ai_not_configured", Msg: "No AI chat model is set up yet. An administrator can add one in Administration → AI."}
		}
		return err
	}
	// The provider's context window is shared out: room for the answer first, then the earlier
	// conversation (a quarter at most), and the rest for the passages found.
	sys := fmt.Sprintf(systemPrompt, time.Now().Format("2 January 2006"))
	room := prov.contextTokens() - askReplyTokens - estTokens(sys) - estTokens(q)
	convID, history, prevQuestion, err := s.conversation(ctx, p, in.ConversationID, min(historyTokens, max(room/4, 0)))
	if err != nil {
		return err
	}
	for _, m := range history {
		room -= estTokens(m.Content)
	}

	emit("status", map[string]string{"stage": "searching"})
	// A follow-up ("and the year before?") only makes sense with the previous question.
	searchText := q
	if prevQuestion != "" && isFollowUp(q) {
		searchText = prevQuestion + "\n" + q
	}
	tune := s.tuning(ctx)
	srcs, err := s.retrieve(ctx, searchText, allowed, in.DocumentIDs, prov.EmbeddingModel != "", tune.AskSources, tune.AskSourcesPerDocument)
	if err != nil {
		return err
	}
	ids := make([]uuid.UUID, 0, len(srcs))
	for _, sc := range srcs {
		ids = append(ids, sc.doc)
	}
	info, err := s.describe(ctx, ids)
	if err != nil {
		return err
	}
	cites := make([]Citation, len(srcs))
	for i, sc := range srcs {
		cites[i] = Citation{N: i + 1, DocumentID: sc.doc, Title: info[sc.doc].title, Page: sc.page, Snippet: snippetOf(sc.text)}
	}

	var answer strings.Builder
	// Saving must outlive the request: a person who presses Stop keeps what they saw.
	saveCtx, cancelSave := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancelSave()
	finish := func() error {
		cid, mid, err := s.saveTurn(saveCtx, p, convID, q, answer.String(), cites)
		if err != nil {
			return err
		}
		emit("done", map[string]any{"conversation_id": cid, "message_id": mid})
		return nil
	}
	if len(srcs) == 0 {
		if len(in.DocumentIDs) > 0 {
			answer.WriteString("These documents have no readable text yet, so I can't answer from them. If they were just added, try again once they've been processed.")
		} else {
			answer.WriteString("I couldn't find anything about that in your documents. Try other words, or check that the document has been uploaded and processed.")
		}
		emit("citations", []Citation{})
		emit("delta", answer.String())
		return finish()
	}
	emit("citations", cites)

	var ctxText strings.Builder
	budget := min(max(room, 300), askPassageTokens)
	for i, sc := range srcs {
		d := info[sc.doc]
		if budget < 50 && i > 0 {
			break // no room for more than a scrap of this passage
		}
		t := cutToTokens(strings.TrimSpace(sc.text), max(budget, 0))
		budget -= estTokens(t) + 40 // and its heading line
		fmt.Fprintf(&ctxText, "[%d] %q, page %d", i+1, d.title, sc.page)
		for _, f := range [][2]string{{"from", d.from}, {"type", d.typ}, {"dated", d.date}, {"tags", d.tags}} {
			if f[1] != "" {
				fmt.Fprintf(&ctxText, "; %s: %s", f[0], f[1])
			}
		}
		fmt.Fprintf(&ctxText, "\n%s\n\n", t)
	}
	msgs := []Message{{Role: "system", Content: sys + "\n\nSOURCES:\n" + ctxText.String()}}
	msgs = append(msgs, history...)
	msgs = append(msgs, Message{Role: "user", Content: q})

	emit("status", map[string]string{"stage": "answering"})
	err = client.ChatStream(ctx, msgs, func(d string) {
		answer.WriteString(d)
		emit("delta", d)
	})
	if ctx.Err() != nil { // the person stopped it or left: keep what was written
		if answer.Len() > 0 {
			_ = finish()
		}
		return ctx.Err()
	}
	s.markResult(ctx, prov.ID, err)
	if err != nil {
		if answer.Len() > 0 { // keep what the person already saw
			answer.WriteString("\n\n_(The answer was cut short: " + err.Error() + ")_")
			_ = finish()
		}
		return &apperr.Error{Kind: apperr.KindUnavailable, Code: "ai_error", Msg: aiErrorMessage(err), Err: err}
	}
	return finish()
}

// aiErrorMessage explains an AI server failure to the person asking.
func aiErrorMessage(err error) string {
	var he *httpError
	switch {
	case errors.Is(err, ErrUnavailable), errors.Is(err, ErrEmptyAnswer):
		return err.Error()
	case errors.As(err, &he) && he.Status == 404:
		return "The AI server doesn't know the chat model that's set up. An administrator can pick another one in Administration → AI. (" + he.Error() + ")"
	case errors.As(err, &he) && (he.Status == 401 || he.Status == 403):
		return "The AI server refused Docveta's API key. An administrator can check it in Administration → AI."
	case errors.As(err, &he) && he.Status == 429:
		return "The AI server is busy or over its limit. Try again in a minute."
	}
	return "The AI server couldn't answer: " + err.Error()
}

// conversation checks an existing conversation and returns recent turns for context and the
// last question asked. A new conversation (id nil) is only created when the first answer
// is saved, so failed first questions don't leave empty conversations behind.
func (s *Service) conversation(ctx context.Context, p *auth.Principal, id *uuid.UUID, maxTokens int) (uuid.UUID, []Message, string, error) {
	if id == nil {
		return uuid.Nil, nil, "", nil
	}
	var owner uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT user_id FROM ai_conversations WHERE id=$1`, *id).Scan(&owner); err != nil || owner != p.UserID {
		return uuid.Nil, nil, "", apperr.NotFound("Conversation")
	}
	rows, err := s.pool.Query(ctx, `SELECT role, content FROM (SELECT role, content, created_at FROM ai_messages WHERE conversation_id=$1
		ORDER BY created_at DESC LIMIT $2) m ORDER BY created_at`, *id, historyTurns)
	if err != nil {
		return uuid.Nil, nil, "", err
	}
	hist, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Message, error) {
		var m Message
		err := r.Scan(&m.Role, &m.Content)
		return m, err
	})
	if err != nil {
		return uuid.Nil, nil, "", err
	}
	return *id, trimHistory(hist, maxTokens), lastQuestion(hist), nil
}

// trimHistory drops citation markers from earlier answers and keeps the newest turns that fit.
func trimHistory(hist []Message, maxTokens int) []Message {
	out := make([]Message, 0, len(hist))
	total := 0
	for i := len(hist) - 1; i >= 0; i-- {
		m := hist[i]
		if m.Role == "assistant" {
			m.Content = stripCitations(m.Content)
		}
		if total += estTokens(m.Content); total > maxTokens {
			break
		}
		out = append(out, m)
	}
	// Back to oldest first, starting with a question (models expect user/assistant pairs).
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	for len(out) > 0 && out[0].Role != "user" {
		out = out[1:]
	}
	return out
}

func lastQuestion(hist []Message) string {
	for i := len(hist) - 1; i >= 0; i-- {
		if hist[i].Role == "user" {
			return hist[i].Content
		}
	}
	return ""
}

// Words that refer back to the previous question ("when is it due?", "and the one before?").
var followUpWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`it its it's that this those these them they their theirs same previous above earlier one ones
		यह वह इसका उसका इसकी उसकी इसके उसके ये वे`) {
		followUpWords[w] = true
	}
}

// isFollowUp reports whether a question only makes sense together with the one before it.
func isFollowUp(q string) bool {
	words := strings.FieldsFunc(strings.ToLower(q), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsMark(r) && !unicode.IsDigit(r) && r != '\''
	})
	if len(words) == 0 || len(words) > 12 {
		return false
	}
	if words[0] == "and" || words[0] == "also" || words[0] == "और" || (len(words) > 1 && (words[0] == "what" || words[0] == "how") && words[1] == "about") {
		return true
	}
	for _, w := range words {
		if followUpWords[w] {
			return true
		}
	}
	return false
}

func conversationTitle(question string) string {
	t := strings.Join(strings.Fields(question), " ")
	if r := []rune(t); len(r) > 80 {
		t = string(r[:80]) + "…"
	}
	return t
}

// saveTurn stores a question and its answer, creating the conversation on the first turn.
func (s *Service) saveTurn(ctx context.Context, p *auth.Principal, conv uuid.UUID, question, answer string, cites []Citation) (uuid.UUID, uuid.UUID, error) {
	mid := uuid.Must(uuid.NewV7())
	cj, _ := json.Marshal(cites)
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if conv == uuid.Nil {
			conv = uuid.Must(uuid.NewV7())
			if _, err := tx.Exec(ctx, `INSERT INTO ai_conversations (id, user_id, title) VALUES ($1,$2,$3)`, conv, p.UserID, conversationTitle(question)); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO ai_messages (id, conversation_id, role, content) VALUES ($1,$2,'user',$3)`,
			uuid.Must(uuid.NewV7()), conv, question); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO ai_messages (id, conversation_id, role, content, citations, created_at) VALUES ($1,$2,'assistant',$3,$4, now() + interval '1 millisecond')`,
			mid, conv, answer, cj); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE ai_conversations SET updated_at=now() WHERE id=$1`, conv)
		return err
	})
	return conv, mid, err
}

// Conversations lists the user's conversations, newest first; before pages through older ones.
func (s *Service) Conversations(ctx context.Context, p *auth.Principal, before *time.Time, limit int) ([]Conversation, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, title, updated_at FROM ai_conversations WHERE user_id=$1 AND ($2::timestamptz IS NULL OR updated_at < $2)
		ORDER BY updated_at DESC LIMIT $3`, p.UserID, before, limit)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Conversation, error) {
		var c Conversation
		err := r.Scan(&c.ID, &c.Title, &c.UpdatedAt)
		return c, err
	})
	if out == nil {
		out = []Conversation{}
	}
	return out, err
}

func (s *Service) Messages(ctx context.Context, p *auth.Principal, id uuid.UUID) ([]ConversationMessage, error) {
	var owner uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT user_id FROM ai_conversations WHERE id=$1`, id).Scan(&owner); err != nil || owner != p.UserID {
		return nil, apperr.NotFound("Conversation")
	}
	rows, err := s.pool.Query(ctx, `SELECT id, role, content, citations, created_at FROM ai_messages WHERE conversation_id=$1 ORDER BY created_at`, id)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ConversationMessage, error) {
		var m ConversationMessage
		var cj []byte
		err := r.Scan(&m.ID, &m.Role, &m.Content, &cj, &m.CreatedAt)
		_ = json.Unmarshal(cj, &m.Citations)
		if m.Citations == nil {
			m.Citations = []Citation{}
		}
		return m, err
	})
	if out == nil {
		out = []ConversationMessage{}
	}
	return out, err
}

// RenameConversation changes a conversation's title.
func (s *Service) RenameConversation(ctx context.Context, p *auth.Principal, id uuid.UUID, title string) (*Conversation, error) {
	title = strings.Join(strings.Fields(title), " ")
	if title == "" || utf8.RuneCountInString(title) > 200 {
		return nil, apperr.Invalid("title", "Use 1–200 characters")
	}
	var c Conversation
	err := s.pool.QueryRow(ctx, `UPDATE ai_conversations SET title=$3 WHERE id=$1 AND user_id=$2 RETURNING id, title, updated_at`,
		id, p.UserID, title).Scan(&c.ID, &c.Title, &c.UpdatedAt)
	if db.IsNoRows(err) {
		return nil, apperr.NotFound("Conversation")
	}
	return &c, err
}

func (s *Service) DeleteConversation(ctx context.Context, p *auth.Principal, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM ai_conversations WHERE id=$1 AND user_id=$2`, id, p.UserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Conversation")
	}
	return nil
}

// DeleteAllConversations removes every conversation of the user (privacy: "clear history").
func (s *Service) DeleteAllConversations(ctx context.Context, p *auth.Principal) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM ai_conversations WHERE user_id=$1`, p.UserID)
	return tag.RowsAffected(), err
}
