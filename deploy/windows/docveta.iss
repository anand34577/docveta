; Docveta installer for Windows (Inno Setup 6.3+).
;
; Built by the release workflow:
;   iscc /DAppVersion=1.2.3 /DArch=x64 /DSourceDir=..\..\dist\windows-x64 /DOutputDir=..\..\dist deploy\windows\docveta.iss
;
; SourceDir must contain docveta.exe, LICENSE, README.txt and, for builds with OCR,
; an ocr\ folder (docveta-ocr.exe, _internal\, models\, fonts\).
;
; What it does: installs Docveta to Program Files, keeps data in C:\ProgramData\Docveta,
; writes docveta.conf from the wizard answers, registers and starts the "Docveta" Windows
; service, optionally opens the firewall, and finally opens the setup page (where the
; built-in database is one click).
; Upgrades keep docveta.conf and all data.

#ifndef AppVersion
  #define AppVersion "0.0.0-dev"
#endif
#ifndef Arch
  #define Arch "x64"
#endif
#ifndef SourceDir
  #define SourceDir "..\..\dist\windows-" + Arch
#endif
#ifndef OutputDir
  #define OutputDir "..\..\dist"
#endif
#define WithOCR DirExists(SourceDir + "\ocr")
#define WithPostgres DirExists(SourceDir + "\postgres")

[Setup]
AppId={{35315C92-5E2C-4F9F-B595-DAC420E3B553}
AppName=Docveta
AppVersion={#AppVersion}
AppVerName=Docveta {#AppVersion}
AppPublisher=Docveta contributors
AppPublisherURL=https://github.com/anand34577/docveta
AppSupportURL=https://github.com/anand34577/docveta/wiki
AppUpdatesURL=https://github.com/anand34577/docveta/releases
DefaultDirName={autopf}\Docveta
DefaultGroupName=Docveta
DisableProgramGroupPage=yes
LicenseFile={#SourceDir}\LICENSE
OutputDir={#OutputDir}
OutputBaseFilename=docveta-setup-{#AppVersion}-windows-{#Arch}
SetupIconFile=docveta.ico
UninstallDisplayIcon={app}\docveta.exe
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
PrivilegesRequired=admin
CloseApplications=no
#if Arch == "x64"
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
#elif Arch == "arm64"
ArchitecturesAllowed=arm64
ArchitecturesInstallIn64BitMode=arm64
#else
ArchitecturesAllowed=x86compatible
#endif
MinVersion=10.0

[Types]
Name: "full"; Description: "Docveta with text recognition (recommended)"
Name: "server"; Description: "Docveta only (use an OCR engine on another machine)"
Name: "custom"; Description: "Custom"; Flags: iscustom

[Components]
Name: "server"; Description: "Docveta server and web app"; Types: full server custom; Flags: fixed
#if WithOCR
Name: "ocr"; Description: "Text recognition (OCR) on this computer: GPU, NPU or CPU"; Types: full custom
#endif

[Files]
; WriteConf runs right after copying, before [Run] registers and starts the service.
Source: "{#SourceDir}\docveta.exe"; DestDir: "{app}"; Flags: ignoreversion; Components: server; AfterInstall: WriteConf
Source: "{#SourceDir}\LICENSE"; DestDir: "{app}"; Components: server
Source: "{#SourceDir}\README.txt"; DestDir: "{app}"; Flags: isreadme; Components: server
#if WithOCR
Source: "{#SourceDir}\ocr\*"; DestDir: "{app}\ocr"; Flags: ignoreversion recursesubdirs createallsubdirs; Components: ocr
#endif
#if WithPostgres
; PostgreSQL for the built-in database, so setting it up needs no download.
Source: "{#SourceDir}\postgres\*"; DestDir: "{app}\postgres"; Flags: ignoreversion; Components: server
#endif

[Dirs]
Name: "{commonappdata}\Docveta"; Flags: uninsneveruninstall

[Icons]
Name: "{group}\Open Docveta"; Filename: "{code:AppURL}"; IconFilename: "{app}\docveta.exe"
Name: "{group}\Docveta data folder"; Filename: "{commonappdata}\Docveta"
Name: "{group}\Docveta log"; Filename: "{commonappdata}\Docveta\logs\docveta.log"
Name: "{group}\Docveta help (wiki)"; Filename: "https://github.com/anand34577/docveta/wiki"
Name: "{group}\Uninstall Docveta"; Filename: "{uninstallexe}"

[Run]
; The data folder holds docveta.conf with the database password and secret key: only
; the service (SYSTEM) and administrators may read it.
Filename: "{sys}\icacls.exe"; Parameters: """{commonappdata}\Docveta"" /inheritance:r /grant:r *S-1-5-18:(OI)(CI)F *S-1-5-32-544:(OI)(CI)F"; Flags: runhidden waituntilterminated; StatusMsg: "Protecting the data folder..."
Filename: "{app}\docveta.exe"; Parameters: "service install"; Flags: runhidden waituntilterminated; StatusMsg: "Registering the Docveta service..."
Filename: "{sys}\netsh.exe"; Parameters: "advfirewall firewall add rule name=""Docveta"" dir=in action=allow protocol=TCP localport={code:Port}"; Flags: runhidden waituntilterminated; Check: AllowNetwork; StatusMsg: "Allowing access from other computers..."
Filename: "{app}\docveta.exe"; Parameters: "service start"; Flags: runhidden waituntilterminated; StatusMsg: "Starting Docveta..."
Filename: "{code:AppURL}"; Description: "Open Docveta in the browser to finish setup"; Flags: postinstall shellexec nowait skipifsilent

[UninstallRun]
Filename: "{app}\docveta.exe"; Parameters: "service uninstall"; Flags: runhidden waituntilterminated; RunOnceId: "RemoveService"
Filename: "{sys}\netsh.exe"; Parameters: "advfirewall firewall delete rule name=""Docveta"""; Flags: runhidden waituntilterminated; RunOnceId: "RemoveFirewall"

[UninstallDelete]
Type: files; Name: "{app}\docveta.conf"

[Messages]
FinishedLabel=Docveta is installed and running as a Windows service.%n%nIn the browser page that opens next, click Save and start to use the built-in database (or connect your own PostgreSQL), then create the first account.

[Code]
var
  SettingsPage: TInputQueryWizardPage;
  DevicePage: TInputOptionWizardPage;
  NetworkPage: TInputOptionWizardPage;

function ConfPath(): String;
begin
  Result := ExpandConstant('{app}\docveta.conf');
end;

{ The port: from the wizard, or on upgrades from the existing docveta.conf (DOCVETA_LISTEN=:8080). }
function Port(Param: String): String;
var
  Lines: TArrayOfString;
  I, P: Integer;
  L: String;
begin
  Result := Trim(SettingsPage.Values[0]);
  if FileExists(ConfPath()) and LoadStringsFromFile(ConfPath(), Lines) then
    for I := 0 to GetArrayLength(Lines) - 1 do
    begin
      L := Trim(Lines[I]);
      if Pos('DOCVETA_LISTEN=', L) = 1 then
      begin
        P := Pos(':', Copy(L, 14, Length(L)));
        if P > 0 then
          Result := Copy(L, 14 + P, Length(L));
      end;
    end;
end;

function AppURL(Param: String): String;
begin
  Result := 'http://localhost:' + Port('');
end;

function AllowNetwork(): Boolean;
begin
  Result := NetworkPage.Values[0];
end;

function DeviceSetting(): String;
begin
  case DevicePage.SelectedValueIndex of
    0: Result := 'auto';
    1: Result := 'gpu';
    2: Result := 'igpu';
    3: Result := 'npu';
  else
    Result := 'cpu';
  end;
end;

procedure InitializeWizard();
begin
  SettingsPage := CreateInputQueryPage(wpSelectComponents,
    'Web address', 'Which port should Docveta use?',
    'You open Docveta in the browser at http://localhost:<port>. Change it only if 8080 is taken.');
  SettingsPage.Add('Port:', False);
  SettingsPage.Values[0] := '8080';

  DevicePage := CreateInputOptionPage(SettingsPage.ID,
    'Text recognition', 'Where should Docveta read text from scanned documents?',
    'Docveta falls back to the processor automatically if the chosen device isn''t available. You can change this later in docveta.conf (DOCVETA_OCR_DEVICE).',
    True, False);
  DevicePage.Add('Automatic: the best graphics card, otherwise the processor (recommended)');
  DevicePage.Add('Dedicated or external graphics card (NVIDIA, AMD, Intel Arc, eGPU)');
  DevicePage.Add('Integrated graphics (saves power, keeps the big GPU free)');
  DevicePage.Add('NPU / AI accelerator (Copilot+ PCs, Intel Core Ultra)');
  DevicePage.Add('Processor only');
  DevicePage.SelectedValueIndex := 0;

  NetworkPage := CreateInputOptionPage(DevicePage.ID,
    'Network access', 'Who should be able to open Docveta?',
    'Docveta always works on this computer. Allow other devices (phones, other PCs) on your network to reach it?',
    False, False);
  NetworkPage.Add('Allow access from other devices on my network (opens the port in Windows Firewall)');
  NetworkPage.Values[0] := False;
end;

function ShouldSkipPage(PageID: Integer): Boolean;
begin
  Result := False;
  { Upgrades keep the existing configuration. }
  if ((PageID = SettingsPage.ID) or (PageID = NetworkPage.ID) or (PageID = DevicePage.ID)) and FileExists(ConfPath()) then
    Result := True;
#if WithOCR
  if (PageID = DevicePage.ID) and not WizardIsComponentSelected('ocr') then
    Result := True;
#else
  if PageID = DevicePage.ID then
    Result := True;
#endif
end;

function NextButtonClick(CurPageID: Integer): Boolean;
var
  P: Integer;
begin
  Result := True;
  if CurPageID = SettingsPage.ID then
  begin
    P := StrToIntDef(Port(''), 0);
    if (P < 1) or (P > 65535) then
    begin
      MsgBox('Enter a port number between 1 and 65535.', mbError, MB_OK);
      Result := False;
    end;
  end;
end;

{ Stop the running service before files are replaced (upgrades). }
function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  Code: Integer;
begin
  Exec(ExpandConstant('{sys}\sc.exe'), 'stop Docveta', '', SW_HIDE, ewWaitUntilTerminated, Code);
  Sleep(3000);
  Result := '';
end;

{ Writes docveta.conf on a fresh install. It must exist before the service first starts,
  or the service would keep its data next to the program instead of in ProgramData. }
procedure WriteConf();
var
  Lines: String;
begin
  if not FileExists(ConfPath()) then
  begin
    Lines :=
      '# Docveta settings. Restart the "Docveta" service after changing this file.' + #13#10 +
      '# All options: https://github.com/anand34577/docveta/wiki/Configuration' + #13#10 +
      'DOCVETA_DATA_DIR=' + ExpandConstant('{commonappdata}\Docveta') + #13#10 +
      'DOCVETA_LISTEN=:' + Port('') + #13#10 +
      'DOCVETA_BASE_URL=http://localhost:' + Port('') + #13#10 +
      'DOCVETA_OCR_DEVICE=' + DeviceSetting() + #13#10;
    SaveStringToFile(ConfPath(), Lines, False);
  end;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  { Not in silent uninstalls: a plain message box would wait for a click nobody can make. }
  if (CurUninstallStep = usPostUninstall) and not UninstallSilent() then
    MsgBox('Docveta was removed. Your documents, settings and the built-in database are still in ' +
      ExpandConstant('{commonappdata}\Docveta') + '. Delete that folder yourself if you no longer need it.', mbInformation, MB_OK);
end;
