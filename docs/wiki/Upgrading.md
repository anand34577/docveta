# Upgrading

Docveta updates its database by itself on start (migrations). Your data and settings are kept.
**Make a [backup](Backup-and-restore) first**, then:

| Installed with | Upgrade |
|---|---|
| Windows installer | Run the new `docveta-setup-…exe`. It stops the service, replaces the program, keeps `docveta.conf` and data, and starts it again |
| Windows portable | Close Docveta, replace `docveta.exe` (and the `ocr` folder, if the release has a new one), start it |
| Linux `install.sh` | Unpack the new release, run `sudo ./install.sh` again |
| macOS `install.sh` | Unpack the new release, run `./install.sh` again |
| Docker | `docker compose pull && docker compose up -d` |

Read the release notes before upgrading across several versions. Docveta refuses to start
with a database from a *newer* version (after a downgrade); restore the backup instead.

After upgrading, `docveta doctor` confirms that everything is in order.

## Upgrading PostgreSQL

Docveta works with PostgreSQL 16 and newer. To move to a new major version (e.g. 17 → 18),
follow your system's PostgreSQL upgrade guide (`pg_upgrade`, or dump and restore as in
[Backup and restore](Backup-and-restore)). Docveta needs no changes; it reconnects when
PostgreSQL is back.
