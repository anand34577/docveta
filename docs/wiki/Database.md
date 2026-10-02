# Database (PostgreSQL)

Docveta stores its records (documents' details, tags, users, search index) in
**PostgreSQL 16 or newer**. The files themselves (PDFs, images) stay in Docveta's data folder.

You need PostgreSQL once; Docveta then sets up its tables by itself. PostgreSQL can run on the
same computer as Docveta or on another one.

- [Install PostgreSQL](#install-postgresql)
- [Connect Docveta: the setup page](#connect-docveta-the-setup-page)
- [Create the database yourself](#create-the-database-yourself-optional)
- [A database on another computer](#a-database-on-another-computer)
- [Change the database later](#change-the-database-later)

## Install PostgreSQL

### Windows

1. Download the installer from <https://www.postgresql.org/download/windows/> (choose
   version 17).
2. Run it and keep the defaults. When it asks for a password for the **postgres** user,
   choose one and **write it down**: you'll enter it in Docveta in a minute.
3. Untick *Stack Builder* at the end; Docveta doesn't need it.

PostgreSQL now runs in the background and starts with Windows.

### macOS

The easiest way is **[Postgres.app](https://postgresapp.com)**: download, move to
Applications, open it, click *Initialize*. The user is your macOS user name, without a
password.

With Homebrew instead:

```bash
brew install postgresql@17
brew services start postgresql@17
```

### Ubuntu, Debian, Raspberry Pi OS

```bash
sudo apt update
sudo apt install postgresql postgresql-contrib
```

Check the version with `psql --version`. If it's older than 16 (Debian 12 ships 15, Ubuntu
22.04 ships 14), add the official PostgreSQL repository first:

```bash
sudo apt install -y postgresql-common
sudo /usr/share/postgresql-common/pgdg/apt.postgresql.org.sh
sudo apt install postgresql-17 postgresql-contrib
```

### Fedora, RHEL, Rocky, Alma

```bash
sudo dnf install postgresql-server postgresql-contrib
sudo postgresql-setup --initdb
sudo systemctl enable --now postgresql
```

### Docker

The [Docker setup](Install-with-Docker) includes PostgreSQL; nothing to do.

## Connect Docveta: the setup page

The first time Docveta starts without a database, it shows a **setup page** instead of the app.
Open Docveta in your browser (usually <http://localhost:8080>) and fill in:

| Field | What to enter |
|---|---|
| Server | `localhost` if PostgreSQL is on the same computer, otherwise its name or IP address |
| Port | `5432` (PostgreSQL's default) |
| Database name | `docveta` |
| User / Password | an account that may create databases: on Windows the `postgres` user and the password you chose; on macOS (Postgres.app) your user name and no password |
| Encryption | *Use if available* on the same computer; *Required* over a network |
| Create the database if it doesn't exist | leave ticked |

Click **Test connection**. Docveta checks everything it needs (version, the `pg_trgm` and
`citext` extensions, permission to create tables) and explains what to fix if something is
missing. Then click **Save and start**: Docveta creates its tables and opens the account
setup.

The settings are saved in `docveta.conf` in Docveta's data folder (line `DOCVETA_DATABASE_URL`).

> **From another computer?** The setup page then asks for a **setup code**, so nobody
> else on your network can take over a fresh installation. Docveta prints the code in its log
> and saves it in `setup-code.txt` in the data folder:
>
> - Linux: `sudo cat /var/lib/docveta/setup-code.txt` or `journalctl -u docveta | grep setup_code`
> - Windows (installer): `C:\ProgramData\Docveta\setup-code.txt` (open Notepad as administrator)
> - Docker: `docker compose logs docveta | grep setup_code`

## Create the database yourself (optional)

If you'd rather not give Docveta an administrator account, create a dedicated user and
database, then enter *those* on the setup page:

```bash
# Linux / macOS (Homebrew)
sudo -u postgres createuser --pwprompt docveta
sudo -u postgres createdb --owner docveta docveta
```

```sql
-- or in psql / pgAdmin, as an administrator
CREATE ROLE docveta LOGIN PASSWORD 'choose-a-long-password';
CREATE DATABASE docveta OWNER docveta ENCODING 'UTF8' TEMPLATE template0;
```

Docveta needs to **own** its database (it creates tables and the `pg_trgm` and `citext`
extensions, which database owners may do since PostgreSQL 13).

## A database on another computer

On the database server:

1. In `postgresql.conf`, set `listen_addresses = '*'` (or the server's address).
2. In `pg_hba.conf`, allow the Docveta computer, e.g.
   `host  docveta  docveta  192.168.1.20/32  scram-sha-256`
3. Restart PostgreSQL and open port 5432 in its firewall for the Docveta computer only.

Prefer **Encryption: Required** for connections over a network.

## Change the database later

Edit `DOCVETA_DATABASE_URL` in `docveta.conf` in the data folder, or delete that line and restart
Docveta to get the setup page again. The format is
`postgres://USER:PASSWORD@HOST:5432/DATABASE?sslmode=prefer` (special characters in the
password must be URL-encoded, e.g. `@` → `%40`). An environment variable
`DOCVETA_DATABASE_URL` takes precedence over the file.

When Docveta can't reach the database at start-up (for example PostgreSQL starts more slowly
after a reboot), it waits and retries every few seconds instead of giving up.
