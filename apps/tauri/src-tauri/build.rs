fn main() {
    if std::env::var("CARGO_CFG_TARGET_OS").as_deref() == Ok("macos") {
        cc::Build::new()
            .file("src/dictation.m")
            .file("src/location.m")
            .file("src/sharing.m")
            .flag("-fobjc-arc")
            .flag("-fblocks")
            .compile("polymux_dictation");
        for framework in [
            "Foundation",
            "AVFoundation",
            "Speech",
            "CoreLocation",
            "AppKit",
            "QuickLookUI",
        ] {
            println!("cargo:rustc-link-lib=framework={framework}");
        }
        println!("cargo:rerun-if-changed=src/dictation.m");
        println!("cargo:rerun-if-changed=src/location.m");
        println!("cargo:rerun-if-changed=src/sharing.m");
    }
    tauri_build::build()
}
