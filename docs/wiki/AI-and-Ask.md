# AI: suggestions, meaning-based search and Ask

Docveta can use a language model for three things. All of them are optional; without AI,
documents are still read (OCR), organised by your rules and found by keyword search.

| Feature | What it does | Needs |
|---|---|---|
| **Suggestions** | Proposes a title, date, sender, type and tags for new documents (or fills them in, if a space allows it) | a *chat model* |
| **Meaning-based search** and **Similar documents** | "electricity" finds a bill that only says "power consumption" | an *embedding model* |
| **Ask your documents** | Answers questions from your documents, with every fact linked to the page it came from | a *chat model* (an embedding model makes answers much better) |

Any OpenAI-compatible server works: [Ollama](https://ollama.com), LM Studio, llama.cpp,
vLLM, LocalAI, OpenAI, Azure OpenAI, Groq, OpenRouter and others.

## 1. Add a provider

**Administration → AI → Add provider**

| Field | Example |
|---|---|
| Address | `http://localhost:11434/v1` (Ollama on the same computer), `http://192.168.1.50:11434/v1`, `https://api.openai.com/v1` |
| API key | only for hosted services |
| Chat model | `qwen2.5:7b`, `llama3.1:8b`, `gpt-4o-mini` |
| Embedding model | `nomic-embed-text`, `bge-m3` (best for Hindi and other Indian languages), `text-embedding-3-small` |
| Runs on your own hardware | tick for Ollama/LM Studio on your network; spaces set to "Only with a local provider" use only such providers |

Use **Test** to check the address and see which models the server offers.

With Ollama:

```sh
ollama pull qwen2.5:7b        # chat (about 5 GB; 3b models work on smaller machines)
ollama pull bge-m3            # embeddings, many languages
```

If Docveta runs in Docker and Ollama on the host, use `http://host.docker.internal:11434/v1`
(Docker Desktop) or the host's LAN address, and start Ollama with `OLLAMA_HOST=0.0.0.0`.

## 2. Allow AI per space

Each space decides what AI may see: **Space settings → AI assistance → Use AI for this space**

- **Off** (the default for personal spaces): no document from this space is sent to a model.
- **Only with a local provider**: only providers marked as running on your own hardware.
- **Any configured provider**.

**When AI has suggestions** chooses between *Ask me first (shown in the Inbox)* and *Apply automatically*.

## 3. Index existing documents

New documents are prepared for meaning-based search after their text is read. For documents
that were already there (or after changing the embedding model), use
**Administration → AI → Meaning-based search → Prepare existing documents**. It runs in the background.

## Ask your documents

Open **Ask** in the sidebar (or the bottom bar of the Android app), or **⋯ → Ask about this
document** on a document to ask about that document only ("Summarise this", "What are the
important dates?").

How an answer is made:

1. Docveta searches your documents twice: by keywords (exact words, numbers, names) and by
   meaning (with the embedding model). Follow-up questions ("and the year before?",
   "when is it due?") are searched together with the previous question.
2. The best passages (at most 8, at most 3 from one document) are sent to the chat model with
   the document's title, sender, type, date and tags.
3. The model must answer only from those passages and cite them like `[1]`. Each number
   links to the page. If nothing relevant is found, Docveta says so without asking the model.

Conversations are private to each person. Rename or delete them from the list (on phones:
**History**); **Manage history → Delete all conversations** removes all of yours.

**Stop** ends an answer early; what was written so far is kept.

## Meaning-based search with pgvector (recommended for large libraries)

Docveta stores the embedding vectors in PostgreSQL. Without extra software it compares the
question with every stored vector itself: fine for a few thousand documents, slower beyond.

With the [pgvector](https://github.com/pgvector/pgvector) extension installed in your
PostgreSQL server, Docveta uses it automatically: vectors go into a `vector` column with an
HNSW index (one per vector size, built in the background without blocking uploads), and
searches stay fast with hundreds of thousands of documents. Vectors stored before pgvector
was installed are copied over by the maintenance job (every 10 minutes, 20,000 passages per run).

1. Install pgvector for your PostgreSQL version (`apt install postgresql-17-pgvector`, the
   Windows installer's Stack Builder, or the `pgvector/pgvector` Docker image).
2. If Docveta's database user is not allowed to create extensions, run once as a superuser:
   `CREATE EXTENSION vector;` in the Docveta database.
3. Restart Docveta, or wait up to five minutes: it checks again by itself.

pgvector 0.8 or newer is best: filtered searches (one space, one document) then keep looking
until they have enough results. The built-in database (setup page option) doesn't include
pgvector; use your own PostgreSQL for very large libraries.

## Models and languages

- **Hindi, Tamil, Telugu, Kannada and other Indian languages**: use a multilingual embedding
  model (`bge-m3`, `multilingual-e5-large`, `text-embedding-3-small`). English-only models
  (`nomic-embed-text`, `all-minilm`) find little in these scripts.
- **Reasoning models** (DeepSeek-R1, Qwen3 "thinking"): their hidden reasoning is removed from
  answers. They are slower; a plain instruct model is usually the better choice for Ask.
- Answers are in the language of the question.

## Troubleshooting

| You see | What to do |
|---|---|
| "AI is turned off for these spaces" | Set **Use AI for this space** (Space settings → AI assistance). Spaces set to "Only with a local provider" need a provider marked as running on your own hardware. |
| "The AI server doesn't know the chat model" | The model name doesn't match the server's; use **Test** to list the models it offers. |
| "The AI server isn't responding; try again in a minute" | After five failures in a row Docveta pauses calls to that provider for a minute. Check the server is running and reachable from the Docveta computer. |
| "The AI server stopped answering for 1m30s" (the provider's timeout) | The model is stuck or overloaded (often: not enough memory). Raise the timeout in the provider's settings, or use a smaller model. |
| Answers say nothing was found, but the document exists | Check the document has text (its **Text** tab), that its space allows AI, and that **Prepare existing documents** has run after you added the embedding model. |
| Answers stop after a minute behind nginx | Docveta sends a keep-alive every 15 s; make sure the proxy doesn't buffer: `proxy_buffering off;` for `/api/v1/ai/ask` (see [Remote access and HTTPS](Remote-access-and-HTTPS)). |

## Privacy

Only the passages needed for an answer (and, for suggestions, the start of the document) are
sent to the provider. Nothing is sent from spaces where AI is **Off**, and spaces set to **Only
with a local provider** never reach a hosted service. Conversations stay in your Docveta database.
