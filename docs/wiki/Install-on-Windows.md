# Install on Windows

Two ways, same program:

| | **Installer** (recommended) | **Portable ZIP** |
|---|---|---|
| Runs | in the background as a Windows service, starts with Windows | while its window is open |
| Data | `C:\ProgramData\Docveta` | `data` folder next to `docveta.exe` |
| Settings | `C:\Program Files\Docveta\docveta.conf` | `docveta.conf` next to `docveta.exe` |
| Needs administrator rights | yes, to install | no |
| Good for | the PC or server that holds your documents | trying Docveta, USB sticks, no admin rights |

Windows 10 or 11, 64-bit (x64 or ARM). Nothing else to install: Docveta brings its own
database. A 32-bit build exists for old PCs (without text recognition or the built-in database:
it needs [your own PostgreSQL](Database#windows)).

## Installer

1. Download `docveta-setup-<version>-windows-x64.exe` from the
   [Releases page](https://github.com/anand34577/docveta/releases)
   (`…-arm64.exe` for Snapdragon / Windows on ARM).
2. Run it. If **Windows SmartScreen** says it protected your PC, click *More info* →
   *Run anyway* (Docveta isn't code-signed yet; check the download against `SHA256SUMS` if
   you like: `Get-FileHash .\docveta-setup-*.exe`).
3. The wizard asks:
   - **Components**: keep *Text recognition* to read scans on this PC.
   - **Port**: `8080` unless something else uses it.
   - **Text recognition**: *Automatic* picks your graphics card and falls back to the
     processor. Details: [OCR engines](OCR-engines).
   - **Network access**: tick it to use Docveta from your phone or other PCs; this opens the
     port in Windows Firewall.
4. At the end, your browser opens Docveta. Click **Save and start** to use the built-in
   database (or [connect your own PostgreSQL](Database#connect-docveta-the-setup-page)), then
   create your account.

Start-menu entries: *Open Docveta*, *Docveta data folder*, *Docveta log*, *Docveta help*.

**Manage the service** in *Services* (`services.msc`, "Docveta Document Manager") or in an
administrator command prompt:

```bat
sc stop Docveta
sc start Docveta
```

**Change settings** in `C:\Program Files\Docveta\docveta.conf` (open Notepad as administrator),
then restart the service. See [Configuration](Configuration).

**Uninstall** from *Settings → Apps*. Your documents in `C:\ProgramData\Docveta` and your
PostgreSQL database are kept; delete them yourself if you no longer need them.

## Portable ZIP

1. Download `docveta-<version>-windows-x64.zip` and, for text recognition,
   `docveta-ocr-<version>-windows-x64.zip`.
2. Unzip Docveta to a folder you own, e.g. `Documents\Docveta`. Unzip the OCR package **into
   the same folder**, so it looks like this:

   ```
   Docveta\
     docveta.exe
     README.txt
     ocr\
       docveta-ocr.exe
       models\ …
   ```

3. Double-click `docveta.exe`. A window shows what Docveta is doing and your browser opens
   <http://localhost:8080>. Keep the window open; close it (or press Ctrl+C) to stop Docveta.

To change the port or data folder, create `docveta.conf` next to `docveta.exe`:

```ini
DOCVETA_LISTEN=:9000
DOCVETA_BASE_URL=http://localhost:9000
DOCVETA_DATA_DIR=D:\DocvetaData
```

### Turn the portable version into a service

Open a command prompt **as administrator** in the Docveta folder:

```bat
docveta service install
docveta service start
```

The service uses the `docveta.conf` and `data` folder next to `docveta.exe`. Logs go to
`data\logs\docveta.log`. Remove it with `docveta service uninstall` (data is kept).

## Using Docveta from other devices

Open `http://<this-PC's-name-or-IP>:8080` on the other device. If it doesn't load:

- the installer's *Network access* option (or this, as administrator) opens the firewall:
  `netsh advfirewall firewall add rule name="Docveta" dir=in action=allow protocol=TCP localport=8080`
- set `DOCVETA_BASE_URL` to the address people use, so links in emails and single sign-on work.

For use over the internet, put Docveta behind HTTPS: [Remote access and HTTPS](Remote-access-and-HTTPS).
