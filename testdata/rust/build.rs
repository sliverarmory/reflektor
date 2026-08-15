fn main() {
    if std::env::var_os("CARGO_CFG_TARGET_OS").as_deref() == Some(std::ffi::OsStr::new("windows")) {
        // The fixture has no runtime initialization and intentionally carries no
        // CRT entrypoint. This keeps it compatible with Reflektor's in-memory PE
        // loader on all three Windows architectures.
        println!("cargo:rustc-cdylib-link-arg=/NOENTRY");
    }
}
