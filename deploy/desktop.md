# Zazzzor for Windows

**English** · [Русский](desktop.ru.md)

1. Open the [official releases](https://github.com/whoaskedssselery/Zazzzor/releases),
   choose the latest `desktop-vX.Y.Z` release, and download
   `Zazzzor_X.Y.Z_x64-setup.exe`. You need Windows 10 or 11 (x64).
   The installer downloads WebView2 if it is missing.
2. The app has no code-signing certificate. If SmartScreen shows
   **Windows protected your PC**, select **More info** → **Run anyway**.
3. Choose the installer language. English appears first, followed by Russian.
   Installation is per user and requires no administrator privileges.
4. On first launch, enter the Zazzzor server address from your server owner,
   such as `https://friends.example`, and sign in.
5. The app checks for updates at startup and every six hours. It downloads and
   verifies the new version before prompting you. **Yes** installs it and
   restarts the app. **Later** defers the prompt until the next launch.
   To check manually, select **Check for updates** in the system tray menu.

If you're setting up your own server, see the [server owner's guide](README.md).
