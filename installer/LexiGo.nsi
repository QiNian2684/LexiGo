Unicode true
RequestExecutionLevel user
ManifestDPIAware true

!include "MUI2.nsh"
!include "nsDialogs.nsh"
!include "FileFunc.nsh"
!include "x64.nsh"

!define VERSION "3.5.7"
!define UNINSTALL_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\LexiGo"
!define DATA_CONFIG "LexiGo.data-dir"

Name "LexiGo"
Caption "LexiGo ${VERSION} 安装"
UninstallCaption "卸载 LexiGo"
OutFile "${PACKAGE_SETUP}"
InstallDir "$LOCALAPPDATA\Programs\LexiGo"
InstallDirRegKey HKCU "${UNINSTALL_KEY}" "InstallLocation"
BrandingText "LexiGo"
SetCompressor /SOLID lzma
SetFont "Microsoft YaHei UI" 9
VIProductVersion "${VERSION}.0"
VIAddVersionKey /LANG=2052 "ProductName" "LexiGo"
VIAddVersionKey /LANG=2052 "CompanyName" "LexiGo"
VIAddVersionKey /LANG=2052 "FileDescription" "LexiGo 安装向导"
VIAddVersionKey /LANG=2052 "FileVersion" "${VERSION}"
VIAddVersionKey /LANG=2052 "ProductVersion" "${VERSION}"
VIAddVersionKey /LANG=2052 "LegalCopyright" "LexiGo"

!define MUI_ICON "lexigo.ico"
!define MUI_UNICON "lexigo.ico"
!define MUI_HEADERIMAGE
!define MUI_HEADERIMAGE_RIGHT
!define MUI_HEADERIMAGE_BITMAP "header.bmp"
!define MUI_BGCOLOR "F7F8FB"
!define MUI_DIRECTORYPAGE_TEXT_TOP "选择程序文件夹。默认安装在当前用户的 LexiGo 文件夹中。升级便携版时，可在这里选择旧 LexiGo.exe 所在的文件夹。"
!define MUI_PAGE_HEADER_TEXT "程序位置"
!define MUI_PAGE_HEADER_SUBTEXT "第 1 步  选择 LexiGo.exe 的安装位置"
!insertmacro MUI_PAGE_DIRECTORY
Page custom DataPage DataLeave
!insertmacro MUI_PAGE_INSTFILES

!define MUI_UNCONFIRMPAGE_TEXT_TOP "卸载会永久删除程序、使用中的词库、个人笔记、复习记录、本地备份和设置，并清理安装创建的快捷方式与注册表项。请先导出需要保留的数据。"
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "SimpChinese"

Var DataDir
Var DataProgram
Var DataEdit
Var ShortcutCheck
Var MakeShortcut
Var Stage
Var DataLock
Var InstallError

Function .onInit
  ${IfNot} ${RunningX64}
    MessageBox MB_OK|MB_ICONSTOP "LexiGo 需要 64 位 Windows。"
    Abort
  ${EndIf}
  SetRegView 64
  SetShellVarContext current
  StrCpy $MakeShortcut ${BST_CHECKED}
FunctionEnd

Function LoadDataDir
  StrCpy $DataProgram "$INSTDIR"
  StrCpy $DataDir "$INSTDIR\Data"
  ClearErrors
  FileOpen $0 "$INSTDIR\${DATA_CONFIG}" r
  ${IfNot} ${Errors}
    StrCpy $1 ""
    FileReadUTF16LE $0 $1
    FileClose $0
    ${If} $1 != ""
    ${AndIf} $1 != "Data"
      StrCpy $DataDir "$1"
    ${EndIf}
  ${EndIf}
FunctionEnd

Function DataPage
  ${If} $DataProgram != $INSTDIR
    Call LoadDataDir
  ${EndIf}
  !insertmacro MUI_HEADER_TEXT "数据位置" "第 2 步  选择词库保存位置和桌面快捷方式"
  nsDialogs::Create 1018
  Pop $0
  ${If} $0 == "error"
    Abort
  ${EndIf}
  ${NSD_CreateLabel} 0 1u 100% 20u "选择 LexiGo 的数据文件夹。默认在程序文件夹下新建 Data。"
  Pop $0
  SetCtlColors $0 3F5B7E transparent
  ${NSD_CreateDirRequest} 0 29u -52u 14u "$DataDir"
  Pop $DataEdit
  ${NSD_CreateBrowseButton} -47u 28u 47u 16u "浏览…"
  Pop $0
  ${NSD_OnClick} $0 BrowseData
  ${NSD_CreateLabel} 0 53u 100% 34u "这里保存词库、个人笔记、复习进度和应用设置。已有词库时，请选择原数据文件夹。安装会保留其中的现有数据。"
  Pop $0
  ${NSD_CreateCheckbox} 0 95u 100% 14u "创建桌面快捷方式"
  Pop $ShortcutCheck
  ${NSD_SetState} $ShortcutCheck $MakeShortcut
  nsDialogs::Show
FunctionEnd

Function BrowseData
  Pop $0
  ${NSD_GetText} $DataEdit $0
  # The default subfolder may not exist until the installation is confirmed.
  ${DoWhile} $0 != ""
    ${If} ${FileExists} "$0\."
      ${ExitDo}
    ${EndIf}
    ${GetParent} "$0" $1
    ${If} $1 == $0
      StrCpy $0 ""
    ${Else}
      StrCpy $0 "$1"
    ${EndIf}
  ${Loop}
  ${If} $0 == ""
    StrCpy $0 "$INSTDIR" 3
    ${IfNot} ${FileExists} "$0."
      StrCpy $0 ""
    ${EndIf}
  ${EndIf}
  nsDialogs::SelectFolderDialog "选择存放 LexiGo 数据的文件夹" "$0"
  Pop $0
  ${If} $0 != "error"
    ${NSD_SetText} $DataEdit "$0"
  ${EndIf}
FunctionEnd

Function DataLeave
  ${NSD_GetText} $DataEdit $0
  ${NSD_GetState} $ShortcutCheck $MakeShortcut
  StrCpy $1 $0 2 1
  System::Call 'shlwapi::PathGetDriveNumberW(w r0) i.r2'
  ${If} $1 != ":\"
  ${OrIf} $2 == -1
    StrCpy $1 $0 4
    System::Call 'shlwapi::PathIsUNCW(w r0) i.r2'
    ${If} $2 == 0
    ${OrIf} $1 == "\\?\"
    ${OrIf} $1 == "\\.\"
      MessageBox MB_OK|MB_ICONEXCLAMATION "请输入完整的数据文件夹路径，例如 D:\LexiGo\Data。"
      Abort
    ${EndIf}
  ${EndIf}
  # NSIS GetFullPathName requires an existing parent. Win32 only resolves the spelling.
  System::Call 'kernel32::GetFullPathNameW(w r0, i ${NSIS_MAX_STRLEN}, w .r1, p 0) i.r2'
  ${If} $2 == 0
  ${OrIf} $2 >= ${NSIS_MAX_STRLEN}
    MessageBox MB_OK|MB_ICONEXCLAMATION "无法解析数据文件夹路径：$\r$\n$0"
    Abort
  ${EndIf}
  StrCpy $DataDir "$1"
FunctionEnd

Section "LexiGo"
  ${If} $DataProgram == ""
    Call LoadDataDir
    StrCpy $MakeShortcut ${BST_UNCHECKED}
  ${EndIf}
  StrCpy $InstallError "无法创建或写入数据文件夹：$\r$\n$DataDir$\r$\n请确认磁盘已连接，且当前账户对该路径有写入权限。"
  ClearErrors
  CreateDirectory "$DataDir"
  IfErrors installFailed
  GetTempFileName $0 "$DataDir"
  IfErrors installFailed
  Delete "$0"
  IfErrors installFailed
  StrCpy $InstallError "无法创建或写入程序文件夹：$\r$\n$INSTDIR$\r$\n请确认磁盘已连接，且当前账户对该路径有写入权限。"
  CreateDirectory "$INSTDIR"
  IfErrors installFailed
  GetTempFileName $Stage "$INSTDIR"
  IfErrors installFailed
  Delete "$Stage"
  IfErrors installFailed
  StrCpy $InstallError "未能更新程序或注册卸载入口。请关闭旧版 LexiGo，并确认程序文件夹可写入后重试。"
  SetOutPath "$Stage"
  SetOverwrite on
  File /oname=LexiGo.exe "${PACKAGE_EXE}"
  IfErrors installFailed
  WriteUninstaller "$Stage\Uninstall LexiGo.exe"
  IfErrors installFailed
  FileOpen $0 "$Stage\${DATA_CONFIG}" w
  IfErrors installFailed
  StrCpy $1 "$DataDir"
  ${If} $DataDir == "$INSTDIR\Data"
    StrCpy $1 "Data"
  ${EndIf}
  FileWriteUTF16LE /BOM $0 "$1"
  FileClose $0
  IfErrors installFailed

  # Publish the executable first. A running instance may prevent replacement.
  System::Call 'kernel32::MoveFileExW(w "$Stage\LexiGo.exe", w "$INSTDIR\LexiGo.exe", i 9) i.r0'
  StrCmp $0 0 installFailed
  System::Call 'kernel32::MoveFileExW(w "$Stage\${DATA_CONFIG}", w "$INSTDIR\${DATA_CONFIG}", i 9) i.r0'
  StrCmp $0 0 installFailed
  System::Call 'kernel32::MoveFileExW(w "$Stage\Uninstall LexiGo.exe", w "$INSTDIR\Uninstall LexiGo.exe", i 9) i.r0'
  StrCmp $0 0 installFailed
  SetOutPath "$INSTDIR"
  RMDir "$Stage"

  ReadRegStr $2 HKCU "${UNINSTALL_KEY}" "InstallLocation"
  ${If} $2 != $INSTDIR
    DeleteRegValue HKCU "${UNINSTALL_KEY}" "DesktopShortcut"
  ${EndIf}
  ClearErrors
  WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayName" "LexiGo"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "Publisher" "LexiGo"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayIcon" "$INSTDIR\LexiGo.exe"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "DataLocation" "$1"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "UninstallString" '$\"$INSTDIR\Uninstall LexiGo.exe$\"'
  WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoRepair" 1
  WriteRegDWORD HKCU "${UNINSTALL_KEY}" "EstimatedSize" 29000
  IfErrors installFailed
  ${If} $MakeShortcut == ${BST_CHECKED}
    ClearErrors
    CreateShortCut "$DESKTOP\LexiGo.lnk" "$INSTDIR\LexiGo.exe" "" "$INSTDIR\LexiGo.exe"
    ${If} ${Errors}
      MessageBox MB_OK|MB_ICONEXCLAMATION "LexiGo 已安装，但桌面快捷方式创建失败。"
    ${Else}
      WriteRegStr HKCU "${UNINSTALL_KEY}" "DesktopShortcut" "$DESKTOP\LexiGo.lnk"
    ${EndIf}
  ${EndIf}
  Goto installDone

  installFailed:
    SetOutPath "$INSTDIR"
    ${If} $Stage != ""
      Delete "$Stage\LexiGo.exe"
      Delete "$Stage\Uninstall LexiGo.exe"
      Delete "$Stage\${DATA_CONFIG}"
      RMDir "$Stage"
    ${EndIf}
    MessageBox MB_OK|MB_ICONSTOP "$InstallError$\r$\n学习数据已保留。" /SD IDOK
    SetErrorLevel 1
    Quit
  installDone:
SectionEnd

Function un.onInit
  SetRegView 64
  SetShellVarContext current
  ReadRegStr $0 HKCU "${UNINSTALL_KEY}" "InstallLocation"
  ${If} $0 != $INSTDIR
    MessageBox MB_OK|MB_ICONSTOP "未找到此位置的安装记录。请在 Windows 设置的已安装应用中卸载。"
    Abort
  ${EndIf}
  ReadRegStr $DataDir HKCU "${UNINSTALL_KEY}" "DataLocation"
  ${If} $DataDir == "Data"
    StrCpy $DataDir "$INSTDIR\Data"
  ${EndIf}
  System::Call 'shlwapi::PathIsRelativeW(w "$DataDir") i.r0'
  ${If} $DataDir == ""
  ${OrIf} $0 != 0
    MessageBox MB_OK|MB_ICONSTOP "无法识别安装时选择的数据文件夹。请修复安装后重试。"
    Abort
  ${EndIf}
FunctionEnd

Section "Uninstall"
  StrCpy $DataLock ""
  ${If} ${FileExists} "$DataDir\."
    System::Call 'kernel32::CreateFileW(w "$DataDir\lock", i 0xC0000000, i 0, p 0, i 4, i 0x80, p 0) p.r0'
    StrCmp $0 -1 uninstallFailed
    StrCpy $DataLock "$0"
  ${EndIf}
  ClearErrors
  Delete "$INSTDIR\LexiGo.exe"
  IfErrors uninstallFailed

  # Remove only files owned by LexiGo from a user-selected data folder.
  ClearErrors
  Delete "$DataDir\vocabulary.db"
  Delete "$DataDir\vocabulary.db-wal"
  Delete "$DataDir\vocabulary.db-shm"
  Delete "$DataDir\vocabulary.db-journal"
  Delete "$DataDir\appearance.json"
  Delete "$DataDir\instance"
  Delete "$DataDir\.migrate-*.db"
  Delete "$DataDir\.wordreview-*.tmp"
  Delete "$DataDir\.write-test-*"
  Delete "$DataDir\backups\before-restore.json"
  Delete "$DataDir\backups\legacy-before-restore.json"
  Delete "$DataDir\backups\before-transfer-*.json"
  Delete "$DataDir\backups\before-dual-memory-v2-*.json"
  System::Call 'kernel32::GetFileAttributesW(w "$DataDir\WebView2") i.r0'
  ${If} $0 != -1
    IntOp $1 $0 & 0x400
    ${If} $1 != 0
      RMDir "$DataDir\WebView2"
    ${Else}
      RMDir /r "$DataDir\WebView2"
    ${EndIf}
  ${EndIf}
  IfErrors uninstallFailed

  ${If} $DataLock != ""
    System::Call 'kernel32::CloseHandle(p $DataLock)'
    StrCpy $DataLock ""
    Delete "$DataDir\lock"
    IfErrors uninstallFailed
  ${EndIf}
  RMDir "$DataDir\backups"
  RMDir "$DataDir"
  ReadRegStr $0 HKCU "${UNINSTALL_KEY}" "DesktopShortcut"
  ClearErrors
  ${If} $0 == "$DESKTOP\LexiGo.lnk"
    Delete "$DESKTOP\LexiGo.lnk"
  ${EndIf}
  Delete "$INSTDIR\${DATA_CONFIG}"
  IfErrors uninstallFailed
  DeleteRegKey HKCU "${UNINSTALL_KEY}"
  Delete "$INSTDIR\Uninstall LexiGo.exe"
  RMDir "$INSTDIR"
  Goto uninstallDone

  uninstallFailed:
    ${If} $DataLock != ""
      System::Call 'kernel32::CloseHandle(p $DataLock)'
    ${EndIf}
    MessageBox MB_OK|MB_ICONSTOP "有文件正在使用或无法删除。请退出 LexiGo，检查目录权限后重新卸载。" /SD IDOK
    SetErrorLevel 1
    Quit
  uninstallDone:
SectionEnd
