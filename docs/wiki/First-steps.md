# First steps

After [connecting the database](Database#connect-docveta-the-setup-page), Docveta shows a short
setup wizard.

## 1. Create your account

- **Who will use Docveta?** *My household*, *An organisation or team* or *Just me*. This only
  decides whether a shared space is created now; you can add spaces any time.
- **Name of your shared space**: e.g. *Family* or *Office*.
- **Your name, email and password**: this first account is the administrator.

## 2. Spaces: who sees what

Every document lives in a **space**. Everyone has a private **Personal** space; shared
spaces (*Family*, *Accounts*, *Client X*) are for documents several people need.

In a shared space each member is an **owner** (manages members and settings), **editor**
(adds and changes documents) or **viewer** (reads and downloads). Change members under the
space's settings (gear icon next to its name).

## 3. Add documents

- **Drag and drop** files onto the page, use **Upload**, or paste an image (Ctrl+V).
- **From your phone**: open Docveta in the browser and choose *Add to Home screen*. On
  Android you can then *Share → Docveta* from any app.
- PDFs, JPEG, PNG, TIFF (multi-page), HEIC/AVIF (phone photos), WebP, GIF, BMP and text files.

New documents land in the **Inbox**. Docveta reads the text ([OCR](OCR-engines)), finds the
document date and applies your tag rules. In the Inbox you check the suggestions, adjust the
title, tags, correspondent ("from") and type, and mark the document as reviewed. On a
computer the Inbox works from the keyboard: `J`/`K` (or arrow keys) move between documents,
`E` marks one as reviewed, `Enter` opens it.

## 4. Find documents

Type in the search box (Ctrl+K anywhere). Docveta searches titles, notes and the text inside
documents, in all scripts, and tolerates typos. Filters:

| Type | Finds |
|---|---|
| `electricity bill` | documents containing both words |
| `"policy number"` | the exact phrase |
| `tag:tax` | documents tagged *tax* |
| `from:hdfc` | from the correspondent HDFC |
| `type:invoice` | of the document type *invoice* |
| `date:2025` / `date:2025-04..2026-03` | by document date |
| `added:>30d` | added in the last 30 days |
| `is:inbox` / `is:failed` | not yet reviewed / couldn't be processed |
| `-draft` | without the word *draft* |

Save a search as a **view** (e.g. "FY 2025-26 tax") to reopen it with one click.

## 5. Invite people

*Administration → Users → Add user*, then add them to a shared space. Or connect your
company login: *Administration → Single sign-on*.

## 6. Get notified

*Settings → Notifications*: in the app, by email, or to your phone through Gotify or ntfy,
when a document is processed, someone mentions you in a note, or something needs attention.

## Next

- [OCR engines](OCR-engines): use your GPU or NPU for faster text recognition
- [Remote access and HTTPS](Remote-access-and-HTTPS): use Docveta safely from outside your home
- [Backup and restore](Backup-and-restore): set up backups **today**
