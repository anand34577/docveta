Docveta - your documents, organised and searchable
=================================================

PORTABLE (ZIP) VERSION
  1. Unzip this folder anywhere you like (e.g. Documents\Docveta). Keep it together:
     docveta.exe, and the "ocr" folder if you downloaded the OCR package.
  2. Double-click docveta.exe. A window shows the log; your browser opens
     http://localhost:8080. Keep the window open while you use Docveta.
  3. In the browser, click "Save and start" to use the built-in database (or connect
     your own PostgreSQL), then create your account.

  Your documents, settings and the built-in database are stored in the "data" folder
  next to docveta.exe. To back up, close Docveta and copy that folder.

  To run Docveta in the background at every start-up instead, open a command prompt
  as administrator in this folder and run:
      docveta service install
      docveta service start

INSTALLER VERSION
  The installer sets up Docveta as a Windows service that starts with Windows. Data
  lives in C:\ProgramData\Docveta, settings in docveta.conf in the install folder.

YOUR OWN POSTGRESQL? (optional: the built-in database needs nothing installed)
  Download it from https://www.postgresql.org/download/windows/ (version 16 or
  newer) and remember the password you set for the "postgres" user. The 32-bit
  version of Docveta has no built-in database and always needs one.

TEXT RECOGNITION (OCR)
  With the "ocr" folder present, Docveta reads text from scans on your graphics card
  (NVIDIA, AMD or Intel, built-in or external), an NPU, or the processor. Choose with
  DOCVETA_OCR_DEVICE=auto|gpu|igpu|npu|cpu in docveta.conf. See which devices are found:
      ocr\docveta-ocr.exe --list-devices
      ocr\docveta-ocr.exe --self-test

SETTINGS
  Create docveta.conf next to docveta.exe (one KEY=VALUE per line), for example:
      DOCVETA_LISTEN=:8080
      DOCVETA_DATA_DIR=D:\DocvetaData
      DOCVETA_OCR_DEVICE=gpu

HELP
  Guides for every step: https://github.com/anand34577/docveta/wiki
  Problems and questions: https://github.com/anand34577/docveta/issues
