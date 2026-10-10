# AI: suggestions, meaning-based search and Ask

Docveta can use a language model for three things. All of them are optional; without AI,
documents are still read (OCR), organised by your rules and found by keyword search.

| Feature | What it does | Needs |
|---|---|---|
| **Suggestions** | Works out what kind of document each new one is (invoice, contract, certificate…), who it is from, its date, title and tags, and proposes them (or fills them in, if a space allows it) | a *chat model* |
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
| Context size (tokens) | how much text the chat model takes at once: `4096` for many local models, `8192` (the default) or more for hosted ones. See [Small models](#small-models-and-long-documents) |

Use **Test** to check the address and see which models the server offers.

With Ollama:

```sh
ollama pull qwen2.5:7b        # chat (about 5 GB; 3b models work on smaller machines)
ollama pull bge-m3            # embeddings, many languages
```

If Docveta runs in Docker and Ollama on the host, use `http://host.docker.internal:11434/v1`
(Docker Desktop) or the host's LAN address, and start Ollama with `OLLAMA_HOST=0.0.0.0`.

## How a document gets its suggestions

1. **Reading.** The document's text is taken from the file, or read from the scan by the OCR
   engine. No AI is involved, and the document is ready and searchable when this is done.
2. **Your rules.** Tags, senders and types with a matching rule are applied, and the date is
   found in the text.
3. **AI, in the background.** If the space allows AI, the text goes to the chat model together
   with the space's tags, senders and types, your custom fields and a few examples of how you
   filed recent documents. The model answers with a small JSON object: type, sender, date,
   title, tags and field values, each with how sure it is.
4. **Checking the answer.** Docveta matches the names against what exists, drops what it can't
   use (a tag when new tags are off, an impossible date), and never touches a type, sender or
   date that is already set.
5. **Suggestions or automatic.** What is left is shown as suggestions on the document and in the
   Inbox, or applied straight away in spaces set to *Apply automatically* (85% sure or more).

The model only sees text: it is not sent the picture of the page.

## Small models and long documents

A model can only take so much text at once (its *context size*). Set the provider's **Context
size** to what your model runs with, and Docveta fits every request into it:

- For suggestions it sends the start of the document (letterhead, title, date, sender: what says
  what a document is) and a little of its end (totals, signatures), not the middle. With 4096
  tokens that is about 8,000 English characters, roughly the first two or three pages.
- With less than 8192 tokens it also sends shorter lists: the 40 most used tags, senders and
  types instead of 150, and two examples instead of five.
- For Ask it sends fewer or shorter passages and less of the earlier conversation.
- Hindi, Tamil and other Indian scripts use about four times as many tokens per character as
  English, so less of such a document fits.

Docveta can't change the model's own limit. With Ollama, set it there too (`num_ctx` in a
Modelfile, or `OLLAMA_CONTEXT_LENGTH=8192`); if Ollama runs with less than the number entered
here, it cuts the request itself and suggestions get worse without any error.

## 2. Allow AI per space

Each space decides what AI may see: **Space settings → AI assistance → Use AI for this space**

- **Off** (the default for personal spaces): no document from this space is sent to a model.
- **Only with a local provider**: only providers marked as running on your own hardware.
- **Any configured provider**.

**When AI has suggestions** chooses between *Ask me first (shown in the Inbox)* and *Apply automatically*.
Automatic applies what the AI is at least 85% sure of; the rest waits for you as suggestions.

### Document types and tags

A document has one **type** and any number of **tags**. The type is the broad kind, the drawer
it goes in; the tags say what exactly it is. An Aadhaar card: type *Identification*, tag
*Aadhaar*. An electricity bill: type *Bill*, tags *Electricity*, *Home*.

- **Document type.** The AI picks from the space's own types first (**Space settings → Document
  types**), then from a list of common ones (Identification, Banking, Tax, Bill, Invoice,
  Receipt, Insurance, Medical, Education, Employment, Property, Vehicle, Legal, Contract,
  Certificate, Travel, Warranty, Letter, Report, Manual), and only then makes up a name. A type
  that doesn't exist yet is created when you accept it. Browse by type from Home and the sidebar
  (web), the type chips above the list (Android), the **Type** filter, or search `type:identification`.
- **Tags.** The AI uses your tags when they fit and may add up to three new ones per document.
  To keep to your own tags, turn off **Let AI create new tags** in the space's AI settings.
- A document that already has a type keeps it, and so do a sender or date you set yourself: the
  AI only fills in what is empty and adds tags.
- You can always set or create a type and tags yourself: **What is it?** and **Tags** on a document.

### If the AI server is down

Nothing else waits for it. Documents are read, indexed and become ready first; the AI step runs
afterwards in the background. If it fails, the document simply has no suggestions (ask again
with **⋯ → Ask AI for suggestions**), search falls back to keywords, and
**Administration → AI** shows the last error.

## 3. Index existing documents

New documents are prepared for meaning-based search after their text is read. For documents
that were already there (or after changing the embedding model), use
**Administration → AI → Meaning-based search → Prepare existing documents**. It runs in the background.

## Settings you can tune

Everything below has a sensible default; nothing needs changing to start.

| Where | Setting | Default | What it does |
|---|---|---|---|
| Space settings → AI assistance | Use AI for this space | Off | Which providers may see this space's documents |
| | When AI has suggestions | Ask me first | Ask first, or apply automatically |
| Space settings → AI assistance → Fine-tuning | Apply automatically when AI is at least this sure | 85% | Lower applies more by itself; higher leaves more to check |
| | Let AI create new tags | On | Off keeps to the tags you made |
| | New tags per document, at most | 3 | 1 to 10 |
| | Let AI create new document types | On | Off keeps to the space's own types |
| | Create something new when AI is at least this sure | 60% | For tags, senders and types that don't exist yet |
| Administration → AI → a provider | Context size (tokens) | 8192 | How much text the model takes at once |
| | Waits up to (seconds) | 90 | Raise it for slow local models |
| | Requests at once | 2 | Lower it if the model's computer struggles |
| Administration → AI → Tuning | Document types AI may suggest | 20 broad types | The list offered when none of a space's own types fits |
| | Passages an answer is made from | 8 | For Ask; more needs a larger context size |
| | Of those, from one document at most | 3 | |
| | Text read for suggestions, at most (tokens) | 4000 | How far into a long document AI reads to file it |

The same settings are in the Android app (a space → AI, and Administration → AI). **Reset** and
**Back to the defaults** undo your changes.

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
