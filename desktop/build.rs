fn main() {
    println!("cargo:rerun-if-changed=backend-sha256.txt");
    tauri_build::build()
}
