# Backup and restore

Three things make up your Docveta:

| What | Where |
|---|---|
| **The database** (document details, tags, users, search index) | PostgreSQL |
| **The files** (originals, searchable PDFs, thumbnails) | `blobs/` in the data folder |
| **`docveta.conf` in the data folder** (database connection, **secret key**) | the data folder |

Back up all three, and keep at least one copy on another device or in the cloud.

**Built-in database:** everything, the database included, is in the data folder (the database
in its `postgres` subfolder). Stop Docveta, copy the whole data folder, start Docveta again.
To restore, put the folder back while Docveta is stopped. The sections below are for your own
PostgreSQL server.

Files are never changed after they're stored, and files Docveta no longer needs are only
deleted a week later. So the safe order is: **database first, then files.** A backup taken
that way is always consistent, even while Docveta runs.

## Back up

### Linux / macOS

```bash
# 1. database (adjust user/host; on Debian/Ubuntu run it as the postgres user)
pg_dump -Fc -U docveta -h localhost docveta > docveta-$(date +%F).dump
# 2. files and settings (restic, borg, rsync, or any backup tool)
restic -r /mnt/backup/docveta backup /var/lib/docveta
```

Run it nightly with cron or a systemd timer.

### Windows

```bat
:: 1. database (pg_dump comes with PostgreSQL)
"C:\Program Files\PostgreSQL\17\bin\pg_dump.exe" -Fc -U postgres -h localhost -f D:\Backup\docveta.dump docveta
:: 2. files and settings
robocopy C:\ProgramData\Docveta D:\Backup\DocvetaData /MIR /XD tmp logs
```

Put both lines in a `.bat` file and schedule it with *Task Scheduler*. Tools like Veeam Agent,
Duplicati or Macrium also work for the data folder.

### Docker

```bash
docker compose exec -T db pg_dump -U docveta -Fc docveta > docveta-$(date +%F).dump
restic -r /mnt/backup/docveta backup ./data
```

## Restore

1. Install Docveta and PostgreSQL (same or newer versions).
2. Create an empty database and restore it:
   ```bash
   createdb -U postgres -O docveta docveta
   pg_restore -U postgres -d docveta --no-owner --role=docveta docveta-YYYY-MM-DD.dump
   ```
3. Put the data folder back, including its `docveta.conf` (adjust `DOCVETA_DATABASE_URL` in it if
   the database moved).
4. Start Docveta and check everything:
   ```bash
   docveta doctor --verify-blobs
   ```

> Without the original `DOCVETA_SECRET_KEY`, everything still works except stored passwords
> (email server, single sign-on, notification channels), which you then enter again.

## Moving to a new computer

Same as a restore: back up on the old machine, install on the new one, restore, start.
