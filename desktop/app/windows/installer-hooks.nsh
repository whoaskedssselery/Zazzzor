; The app writes itself under the user's Run key when “Start with
; Windows” is ticked (src/autostart.rs). Removing the app takes that line
; out too; an update, which runs the old uninstaller with /UPDATE, keeps it.
!macro NSIS_HOOK_POSTUNINSTALL
  ${If} $UpdateMode <> 1
    DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "Zazzzor"
  ${EndIf}
!macroend
