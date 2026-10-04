package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
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
}

var stop = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a an and are as at be but by can did do does for from had has have how i if in into is it its me my of on or our
		so than that the their them then there these they this to was we were what when where which who whom why will with would you your about
		tell show give find please much many any`) {
		stop[w] = true
	}
}

// keywordQuery turns a question into an OR query over our lexemes: "what is my
// electricity bill" → 'electricity' | 'bill'. (Search's AND would find nothing.)
func keywordQuery(q string) string {
	seen := map[string]bool{}
	var parts []string
	for _, t := range textindex.Terms(q) {
		if stop[t] || utf8.RuneCountInString(t) < 2 || seen[t] {
			continue
		}
		seen[t] = true
		parts = append(parts, textindex.TSQuery([]textindex.QueryPart{{Terms: []string{t}}}))
	}
	return strings.Join(parts, " | ")
}

type source struct {
	doc   uuid.UUID
	title string
	page  int
	text  string
	score float64
}

// retrieve finds the passages to answer from: keyword matches on the pages plus, when
// embeddings exist, meaning-based matches, fused with reciprocal rank fusion.
func (s *Service) retrieve(ctx context.Context, p *auth.Principal, question string, spaceIDs []uuid.UUID, semantic bool) ([]source, error) {
	type key struct {
		doc  uuid.UUID
		page int
	}
	scores := map[key]float64{}
	texts := map[key]string{}
	titles := map[uuid.UUID]string{}
	var bestSemantic float32
	keywordHits := 0

	if tsq := keywordQuery(question); tsq != "" {
		rows, err := s.pool.Query(ctx, `SELECT pg.document_id, pg.page_no, pg.text, d.title
			FROM pages pg JOIN documents d ON d.id=pg.document_id
			WHERE d.space_id = ANY($1) AND d.deleted_at IS NULL AND pg.fts @@ ($2::text::tsquery || to_tsquery('english', $2::text))
			ORDER BY ts_rank(pg.fts, ($2::text::tsquery || to_tsquery('english', $2::text))) DESC LIMIT 12`, spaceIDs, tsq)
		if err != nil {
			return nil, err
		}
		rank := 0
		for rows.Next() {
			var id uuid.UUID
			var page int
			var text, title string
			if err := rows.Scan(&id, &page, &text, &title); err != nil {
				rows.Close()
				return nil, err
			}
			k := key{id, page}
			scores[k] += 1 / float64(60+rank)
			texts[k], titles[id] = text, title
			rank++
			keywordHits++
		}
		rows.Close()
	}
	if semantic {
		hits, err := s.Semantic(ctx, p, question, spaceIDs, 12)
		if err == nil {
			for i, h := range hits {
				k := key{h.DocumentID, h.Page}
				scores[k] += 1 / float64(60+i)
				if _, ok := texts[k]; !ok {
					texts[k] = h.Text
				}
				if i == 0 {
					bestSemantic = h.Score
				}
			}
			var ids []uuid.UUID
			for _, h := range hits {
				if _, ok := titles[h.DocumentID]; !ok {
					ids = append(ids, h.DocumentID)
				}
			}
			if len(ids) > 0 {
				rows, err := s.pool.Query(ctx, `SELECT id, title FROM documents WHERE id = ANY($1)`, ids)
				if err == nil {
					for rows.Next() {
						var id uuid.UUID
						var t string
						if rows.Scan(&id, &t) == nil {
							titles[id] = t
						}
					}
					rows.Close()
				}
			}
		}
	}
	// Only a weak meaning-based match and no keyword hit at all is more likely noise than an answer.
	if keywordHits == 0 && bestSemantic > 0 && bestSemantic < 0.4 {
		return nil, nil
	}
	var out []source
	for k, sc := range scores {
		out = append(out, source{doc: k.doc, title: titles[k.doc], page: k.page, text: texts[k], score: sc})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].score > out[j].score })
	if len(out) > 8 {
		out = out[:8]
	}
	return out, nil
}

func snippetOf(text string) string {
	t := strings.Join(strings.Fields(text), " ")
	r := []rune(t)
	if len(r) > 240 {
		return string(r[:240]) + "…"
	}
	return t
}

// Ask answers a question from the user's documents, streaming the answer through emit.
// Events: "citations" ([]Citation), "delta" (string), "done" ({conversation_id, message_id}).
// If nothing relevant is found it says so without calling the model.
func (s *Service) Ask(ctx context.Context, p *auth.Principal, in AskInput, emit func(event string, data any)) error {
	q := strings.TrimSpace(in.Question)
	if q == "" || utf8.RuneCountInString(q) > 1000 {
		return apperr.Invalid("question", "Ask a question of up to 1000 characters")
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
			return &apperr.Error{Kind: apperr.KindUnavailable, Code: "ai_off", Msg: "AI is turned off for your spaces. An administrator can enable it in the space's settings."}
		case errors.Is(err, ErrNoModel):
			return &apperr.Error{Kind: apperr.KindUnavailable, Code: "ai_not_configured", Msg: "No AI provider is set up yet. An administrator can add one in Administration."}
		}
		return err
	}
	convID, history, err := s.conversation(ctx, p, in.ConversationID, q)
	if err != nil {
		return err
	}
	srcs, err := s.retrieve(ctx, p, q, allowed, prov.EmbeddingModel != "")
	if err != nil {
		return err
	}

	cites := make([]Citation, len(srcs))
	for i, sc := range srcs {
		cites[i] = Citation{N: i + 1, DocumentID: sc.doc, Title: sc.title, Page: sc.page, Snippet: snippetOf(sc.text)}
	}
	var answer strings.Builder
	finish := func() error {
		mid, err := s.saveTurn(ctx, convID, q, answer.String(), cites)
		if err != nil {
			return err
		}
		emit("done", map[string]any{"conversation_id": convID, "message_id": mid})
		return nil
	}
	if len(srcs) == 0 {
		answer.WriteString("I couldn't find anything about that in your documents.")
		emit("citations", []Citation{})
		emit("delta", answer.String())
		return finish()
	}
	emit("citations", cites)

	var ctxText strings.Builder
	budget := 14000
	for i, sc := range srcs {
		t := sc.text
		if r := []rune(t); len(r) > 2500 {
			t = string(r[:2500])
		}
		if budget -= len(t); budget < 0 {
			break
		}
		fmt.Fprintf(&ctxText, "[%d] %s (page %d)\n%s\n\n", i+1, sc.title, sc.page, strings.TrimSpace(t))
	}
	sys := `You answer questions about a person's own documents using ONLY the numbered sources below.
- Cite the source number in square brackets after each fact, like [1] or [2][3].
- If the sources don't contain the answer, say you couldn't find it in the documents. Never invent facts, amounts or dates.
- Be concise. Reply in the language of the question.`
	msgs := []Message{{Role: "system", Content: sys + "\n\nSOURCES:\n" + ctxText.String()}}
	msgs = append(msgs, history...)
	msgs = append(msgs, Message{Role: "user", Content: q})

	err = client.ChatStream(ctx, msgs, func(d string) {
		answer.WriteString(d)
		emit("delta", d)
	})
	s.markResult(ctx, prov.ID, err)
	if err != nil {
		if answer.Len() > 0 { // keep what the person already saw
			answer.WriteString("\n\n[The answer was cut short: " + err.Error() + "]")
			_ = finish()
		}
		return fmt.Errorf("ask: %w", err)
	}
	return finish()
}

// conversation loads (or starts) a conversation and returns recent turns for context.
func (s *Service) conversation(ctx context.Context, p *auth.Principal, id *uuid.UUID, question string) (uuid.UUID, []Message, error) {
	if id == nil {
		nid := uuid.Must(uuid.NewV7())
		title := question
		if r := []rune(title); len(r) > 60 {
			title = string(r[:60]) + "…"
		}
		_, err := s.pool.Exec(ctx, `INSERT INTO ai_conversations (id, user_id, title) VALUES ($1,$2,$3)`, nid, p.UserID, title)
		return nid, nil, err
	}
	var owner uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT user_id FROM ai_conversations WHERE id=$1`, *id).Scan(&owner); err != nil || owner != p.UserID {
		return uuid.Nil, nil, apperr.NotFound("Conversation")
	}
	rows, err := s.pool.Query(ctx, `SELECT role, content FROM (SELECT role, content, created_at FROM ai_messages WHERE conversation_id=$1
		ORDER BY created_at DESC LIMIT 6) m ORDER BY created_at`, *id)
	if err != nil {
		return uuid.Nil, nil, err
	}
	hist, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Message, error) {
		var m Message
		err := r.Scan(&m.Role, &m.Content)
		return m, err
	})
	return *id, hist, err
}

func (s *Service) saveTurn(ctx context.Context, conv uuid.UUID, question, answer string, cites []Citation) (uuid.UUID, error) {
	mid := uuid.Must(uuid.NewV7())
	cj, _ := json.Marshal(cites)
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
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
	return mid, err
}

func (s *Service) Conversations(ctx context.Context, p *auth.Principal) ([]Conversation, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, title, updated_at FROM ai_conversations WHERE user_id=$1 ORDER BY updated_at DESC LIMIT 100`, p.UserID)
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
