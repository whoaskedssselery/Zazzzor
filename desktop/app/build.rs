fn main() {
    // An ACL manifest for the app's own commands: with it every command
    // needs a permission, which the capabilities grant (the setup page's in
    // capabilities/, the instance's at run time, see share.rs).
    let commands = &[
        "current_instance",
        "set_instance",
        "open_instance",
        "share_sources",
        "share_thumbnail",
        "share_start",
        "share_stop",
        "share_status",
    ];
    tauri_build::try_build(
        tauri_build::Attributes::new().app_manifest(tauri_build::AppManifest::new().commands(commands)),
    )
    .expect("tauri build");
}
