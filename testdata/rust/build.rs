fn main() {
    match std::env::var("CARGO_CFG_TARGET_OS").as_deref() {
        Ok("macos") => {
            // Reflektor maps the image without registering it for dyld's lazy
            // binder, so resolve libcurl and libSystem imports up front.
            println!("cargo:rustc-cdylib-link-arg=-Wl,-bind_at_load");
        }
        Ok("windows") => {
            // The fixture has no runtime initialization and intentionally carries no
            // CRT entrypoint. This keeps it compatible with Reflektor's in-memory PE
            // loader on all three Windows architectures.
            println!("cargo:rustc-cdylib-link-arg=/NOENTRY");
        }
        _ => {}
    }
}
