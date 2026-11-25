; Inno Setup script for the Ebur installer. Built by build\build.ps1, which
; passes AppVersion, StageDir (the built programs) and OutDir.
;
; Per-user install into C:\ebur, no administrator rights needed (SRS §2.4:
; only the hosts file needs elevation). The only registry entry is the
; standard per-user uninstall entry, which Windows needs to list Ebur in
; Settings > Apps.

#ifndef AppVersion
  #define AppVersion "0.1.0-dev"
#endif
#ifndef NumVersion
  ; Numeric part only: Windows version fields cannot hold "-dev".
  #define NumVersion "0.0.0"
#endif
#ifndef StageDir
  #error StageDir must point at the built programs
#endif
#ifndef OutDir
  #define OutDir "..\dist"
#endif

[Setup]
AppId={{6B0E3C1A-0F7D-4C1B-9E2A-EB0B0E8B5A11}
AppName=Ebur
AppVersion={#AppVersion}
AppVerName=Ebur {#AppVersion}
AppPublisher=Ebur
AppPublisherURL=https://github.com/ebur/ebur
DefaultDirName={sd}\ebur
UsePreviousAppDir=yes
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
OutputDir={#OutDir}
OutputBaseFilename=ebur-setup-{#AppVersion}
SetupIconFile=ebur.ico
UninstallDisplayIcon={app}\ebur.ico
UninstallDisplayName=Ebur
Compression=lzma2/ultra64
SolidCompression=yes
WizardStyle=modern
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0.19044
VersionInfoVersion={#NumVersion}
VersionInfoProductVersion={#NumVersion}
VersionInfoProductTextVersion={#AppVersion}
VersionInfoDescription=Ebur setup

[Tasks]
Name: "desktopicon"; Description: "Create a desktop shortcut"; Flags: unchecked

[Files]
Source: "{#StageDir}\ebur.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#StageDir}\ebur-app.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#StageDir}\ebur-helper.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#StageDir}\ebur.ico"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#StageDir}\THIRD_PARTY.md"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\Ebur"; Filename: "{app}\ebur-app.exe"; IconFilename: "{app}\ebur.ico"; Comment: "Local PHP development environment"
Name: "{autoprograms}\Ebur terminal"; Filename: "{app}\ebur.exe"; Parameters: "terminal"; IconFilename: "{app}\ebur.ico"; Comment: "Console with Ebur's PHP, Composer and MariaDB on PATH"
Name: "{autodesktop}\Ebur"; Filename: "{app}\ebur-app.exe"; IconFilename: "{app}\ebur.ico"; Tasks: desktopicon

[Run]
; Opens the window, which shows the first-run wizard on a new install.
Filename: "{app}\ebur-app.exe"; Description: "Start Ebur"; Flags: postinstall nowait skipifsilent

[UninstallRun]
; Stop every service first, then undo the two changes outside the Ebur
; folder. Each shows its own Windows prompt; declining leaves that change.
Filename: "{app}\ebur.exe"; Parameters: "quit"; Flags: runhidden waituntilterminated; RunOnceId: "StopEbur"
Filename: "{app}\ebur.exe"; Parameters: "ssl uninstall"; Flags: runhidden waituntilterminated; RunOnceId: "RemoveCA"
Filename: "{app}\ebur.exe"; Parameters: "hosts clear"; Flags: runhidden waituntilterminated; RunOnceId: "ClearHosts"

[UninstallDelete]
; Downloaded runtimes, generated configs, logs and certificates go.
; www\ (your sites) and data\ (your databases) are kept.
Type: filesandordirs; Name: "{app}\bin"
Type: filesandordirs; Name: "{app}\etc"
Type: filesandordirs; Name: "{app}\logs"
Type: filesandordirs; Name: "{app}\ssl"
Type: filesandordirs; Name: "{app}\templates"
Type: files; Name: "{app}\ebur.json"

[Code]
// Stop a running Ebur before files are replaced on an upgrade.
function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  Code: Integer;
begin
  if FileExists(ExpandConstant('{app}\ebur.exe')) then
    Exec(ExpandConstant('{app}\ebur.exe'), 'quit', '', SW_HIDE, ewWaitUntilTerminated, Code);
  Result := '';
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usPostUninstall then
    if DirExists(ExpandConstant('{app}\www')) or DirExists(ExpandConstant('{app}\data')) then
      SuppressibleMsgBox('Ebur was removed. Your sites (www) and databases (data) were kept in ' +
        ExpandConstant('{app}') + '. Delete that folder yourself if you no longer need them.',
        mbInformation, MB_OK, IDOK);
end;
