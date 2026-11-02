use std::io::Write;
use std::path::{Path, PathBuf};
use std::sync::Mutex;
use tauri::ipc::{InvokeBody, Request};
use tauri::menu::{MenuBuilder, MenuItemBuilder, PredefinedMenuItem, SubmenuBuilder};
use tauri::{Emitter, Manager, Resource};
use tauri_plugin_dialog::DialogExt;

#[cfg(target_os = "macos")]
mod dictation;
mod location;

const CHUNK_LIMIT: usize = 1024 * 1024;

struct SaveFile {
    file: tempfile::NamedTempFile,
    destination: PathBuf,
}

impl SaveFile {
    fn new(destination: PathBuf) -> std::io::Result<Self> {
        let parent = destination.parent().unwrap_or(Path::new("."));
        Ok(Self {
            file: tempfile::NamedTempFile::new_in(parent)?,
            destination,
        })
    }

    fn commit(mut self) -> std::io::Result<()> {
        self.file.flush()?;
        self.file
            .persist(self.destination)
            .map_err(|error| error.error)?;
        Ok(())
    }
}

struct SaveResource(Mutex<Option<SaveFile>>);
impl Resource for SaveResource {}

struct PreviewFile {
    file: std::fs::File,
    path: PathBuf,
    directory: PathBuf,
}

struct PreviewResource(Mutex<Option<PreviewFile>>);
impl Resource for PreviewResource {}

fn preview_directory() -> std::io::Result<PathBuf> {
    let base = std::env::temp_dir();
    if let Ok(entries) = std::fs::read_dir(&base) {
        for entry in entries.flatten() {
            let name = entry.file_name();
            if !name.to_string_lossy().starts_with("polymux-preview-") {
                continue;
            }
            if let Ok(metadata) = entry.metadata() {
                if let Ok(modified) = metadata.modified() {
                    if modified
                        .elapsed()
                        .map(|age| age.as_secs() > 3600)
                        .unwrap_or(false)
                    {
                        let _ = std::fs::remove_dir_all(entry.path());
                    }
                }
            }
        }
    }
    let stamp = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|value| value.as_nanos())
        .unwrap_or_default();
    let directory = base.join(format!("polymux-preview-{}-{stamp}", std::process::id()));
    std::fs::create_dir_all(&directory)?;
    Ok(directory)
}

#[cfg(target_os = "macos")]
extern "C" {
    fn pm_quick_look(path: *const std::ffi::c_char);
    fn pm_open_file(path: *const std::ffi::c_char);
    fn pm_share_file(path: *const std::ffi::c_char);
}

#[cfg(target_os = "macos")]
fn native_preview(path: &Path, action: &str) -> Result<(), String> {
    let value = std::ffi::CString::new(path.to_string_lossy().as_bytes())
        .map_err(|_| "Invalid file path".to_string())?;
    unsafe {
        match action {
            "quicklook" => pm_quick_look(value.as_ptr()),
            "open" => pm_open_file(value.as_ptr()),
            "share" => pm_share_file(value.as_ptr()),
            "cancel" => return Ok(()),
            _ => return Err("Unknown preview action".into()),
        }
    }
    Ok(())
}

#[cfg(not(target_os = "macos"))]
fn native_preview(_path: &Path, _action: &str) -> Result<(), String> {
    Err("Native previews are only available on macOS.".into())
}

#[tauri::command]
async fn start_preview(window: tauri::WebviewWindow, name: String) -> Result<u32, String> {
    let directory = preview_directory().map_err(|_| "Couldn’t prepare this file.".to_string())?;
    let path = directory.join(save_name(&name));
    let file =
        std::fs::File::create(&path).map_err(|_| "Couldn’t prepare this file.".to_string())?;
    let id = window
        .resources_table()
        .add(PreviewResource(Mutex::new(Some(PreviewFile {
            file,
            path,
            directory,
        }))));
    Ok(id)
}

#[tauri::command]
async fn preview_chunk(window: tauri::WebviewWindow, request: Request<'_>) -> Result<(), String> {
    let id: u32 = request
        .headers()
        .get("x-resource-id")
        .and_then(|value| value.to_str().ok())
        .and_then(|value| value.parse().ok())
        .ok_or("Invalid preview")?;
    let bytes = match request.body() {
        InvokeBody::Raw(bytes) if bytes.len() <= CHUNK_LIMIT => bytes.clone(),
        _ => return Err("Invalid preview data".into()),
    };
    let resource = window
        .resources_table()
        .get::<PreviewResource>(id)
        .map_err(|_| "Invalid preview")?;
    tauri::async_runtime::spawn_blocking(move || -> Result<(), String> {
        let mut guard = resource.0.lock().map_err(|_| "Couldn’t open this file.")?;
        let file = guard.as_mut().ok_or("Invalid preview")?;
        file.file
            .write_all(&bytes)
            .map_err(|_| "Couldn’t open this file.".to_string())
    })
    .await
    .map_err(|_| "Couldn’t open this file.".to_string())?
}

#[tauri::command]
async fn finish_preview(
    window: tauri::WebviewWindow,
    id: u32,
    action: String,
) -> Result<(), String> {
    let resource = window
        .resources_table()
        .take::<PreviewResource>(id)
        .map_err(|_| "Invalid preview")?;
    tauri::async_runtime::spawn_blocking(move || -> Result<(), String> {
        let mut preview = resource
            .0
            .lock()
            .map_err(|_| "Couldn’t open this file.")?
            .take()
            .ok_or("Invalid preview")?;
        preview
            .file
            .flush()
            .map_err(|_| "Couldn’t open this file.".to_string())?;
        drop(preview.file);
        native_preview(&preview.path, &action)?;
        let _ = preview.directory;
        Ok(())
    })
    .await
    .map_err(|_| "Couldn’t open this file.".to_string())?
}

fn save_name(name: &str) -> String {
    let name: String = name
        .rsplit(['/', '\\'])
        .next()
        .unwrap_or("")
        .chars()
        .filter(|character| !character.is_control())
        .take(255)
        .collect();
    if name.is_empty() || name == "." || name == ".." {
        "Attachment".into()
    } else {
        name
    }
}

#[tauri::command]
async fn start_save(window: tauri::WebviewWindow, name: String) -> Result<Option<u32>, String> {
    tauri::async_runtime::spawn_blocking(move || -> Result<Option<u32>, String> {
        let destination = window
            .dialog()
            .file()
            .set_parent(&window)
            .set_file_name(save_name(&name))
            .blocking_save_file();
        let Some(destination) = destination else {
            return Ok(None);
        };
        let path = destination.into_path().map_err(|_| "Invalid destination")?;
        let file = SaveFile::new(path).map_err(|_| "Couldn’t save this file.")?;
        let id = window
            .resources_table()
            .add(SaveResource(Mutex::new(Some(file))));
        Ok(Some(id))
    })
    .await
    .map_err(|_| "Couldn’t save this file.".to_string())?
}

#[tauri::command]
async fn save_chunk(window: tauri::WebviewWindow, request: Request<'_>) -> Result<(), String> {
    let id: u32 = request
        .headers()
        .get("x-resource-id")
        .and_then(|value| value.to_str().ok())
        .and_then(|value| value.parse().ok())
        .ok_or("Invalid save")?;
    let bytes = match request.body() {
        InvokeBody::Raw(bytes) if bytes.len() <= CHUNK_LIMIT => bytes.clone(),
        _ => return Err("Invalid save data".into()),
    };
    let resource = window
        .resources_table()
        .get::<SaveResource>(id)
        .map_err(|_| "Invalid save")?;
    tauri::async_runtime::spawn_blocking(move || -> Result<(), String> {
        let mut guard = resource.0.lock().map_err(|_| "Couldn’t save this file.")?;
        let file = guard.as_mut().ok_or("Invalid save")?;
        file.file
            .write_all(&bytes)
            .map_err(|_| "Couldn’t save this file.".to_string())
    })
    .await
    .map_err(|_| "Couldn’t save this file.".to_string())?
}

#[tauri::command]
async fn finish_save(window: tauri::WebviewWindow, id: u32, commit: bool) -> Result<(), String> {
    let resource = window
        .resources_table()
        .take::<SaveResource>(id)
        .map_err(|_| "Invalid save")?;
    tauri::async_runtime::spawn_blocking(move || -> Result<(), String> {
        let file = resource
            .0
            .lock()
            .map_err(|_| "Couldn’t save this file.")?
            .take();
        if commit {
            file.ok_or("Invalid save")?
                .commit()
                .map_err(|_| "Couldn’t save this file.")?;
        }
        Ok(())
    })
    .await
    .map_err(|_| "Couldn’t save this file.".to_string())?
}

#[cfg(target_os = "macos")]
fn local_host(host: &str) -> bool {
    use std::collections::HashSet;
    use std::ffi::CStr;
    use std::net::ToSocketAddrs;

    let host = host.trim_matches(['[', ']']).to_lowercase();
    let mut name = [0 as libc::c_char; libc::NI_MAXHOST as usize];
    if unsafe { libc::gethostname(name.as_mut_ptr(), name.len()) } != 0 {
        return false;
    }
    let name = unsafe { CStr::from_ptr(name.as_ptr()) }
        .to_string_lossy()
        .to_lowercase();
    let name = name.split('.').next().unwrap_or("");
    if [
        "localhost",
        "127.0.0.1",
        "::1",
        name,
        &format!("{name}.local"),
    ]
    .contains(&host.as_str())
    {
        return true;
    }

    let mut interfaces = std::ptr::null_mut();
    if unsafe { libc::getifaddrs(&mut interfaces) } != 0 {
        return false;
    }
    let mut addresses = HashSet::new();
    let mut current = interfaces;
    while !current.is_null() {
        let address = unsafe { (*current).ifa_addr };
        if !address.is_null() {
            let mut value = [0 as libc::c_char; libc::NI_MAXHOST as usize];
            let result = unsafe {
                libc::getnameinfo(
                    address,
                    (*address).sa_len as libc::socklen_t,
                    value.as_mut_ptr(),
                    value.len() as libc::socklen_t,
                    std::ptr::null_mut(),
                    0,
                    libc::NI_NUMERICHOST,
                )
            };
            if result == 0 {
                addresses.insert(
                    unsafe { CStr::from_ptr(value.as_ptr()) }
                        .to_string_lossy()
                        .to_lowercase(),
                );
            }
        }
        current = unsafe { (*current).ifa_next };
    }
    unsafe { libc::freeifaddrs(interfaces) };
    if addresses.contains(&host) {
        return true;
    }
    host.ends_with(".ts.net")
        && (host.as_str(), 0)
            .to_socket_addrs()
            .map(|resolved| {
                resolved
                    .into_iter()
                    .any(|address| addresses.contains(&address.ip().to_string()))
            })
            .unwrap_or(false)
}

/// Reads the owner token only when the requested server belongs to this machine.
#[tauri::command]
async fn local_token(host: String) -> Option<String> {
    #[cfg(not(target_os = "macos"))]
    {
        let _ = host;
        return None;
    }
    #[cfg(target_os = "macos")]
    if !tauri::async_runtime::spawn_blocking(move || local_host(&host))
        .await
        .ok()?
    {
        return None;
    }
    let data = match std::env::var("POLYMUX_DATA_DIR") {
        Ok(dir) => dir,
        Err(_) => {
            let home = std::env::var("HOME").ok()?;
            format!("{home}/.polymux/data")
        }
    };
    std::fs::read_to_string(format!("{data}/devices/local-token"))
        .ok()
        .map(|token| token.trim().to_string())
}

#[tauri::command]
fn machine_name() -> Option<String> {
    #[cfg(target_os = "macos")]
    {
        let mut name = [0 as libc::c_char; 256];
        if unsafe { libc::gethostname(name.as_mut_ptr(), name.len()) } != 0 {
            return None;
        }
        let value = unsafe { std::ffi::CStr::from_ptr(name.as_ptr()) }
            .to_string_lossy()
            .trim()
            .to_string();
        return if value.is_empty() { None } else { Some(value) };
    }
    #[cfg(not(target_os = "macos"))]
    {
        std::env::var("COMPUTERNAME")
            .or_else(|_| std::env::var("HOSTNAME"))
            .ok()
            .map(|value| value.trim().to_string())
            .filter(|value| !value.is_empty())
    }
}

fn test_window_size() -> Option<(f64, f64)> {
    if !isolated() {
        return None;
    }
    let value = std::env::var("POLYMUX_TEST_WINDOW_SIZE").ok()?;
    let (width, height) = value.split_once('x')?;
    let width: f64 = width.parse().ok()?;
    let height: f64 = height.parse().ok()?;
    (width >= 320.0 && height >= 320.0).then_some((width, height))
}

fn isolated() -> bool {
    #[cfg(debug_assertions)]
    {
        return std::env::var("POLYMUX_TEST_ADDRESS")
            .ok()
            .filter(|value| !value.is_empty())
            .is_some();
    }
    #[cfg(not(debug_assertions))]
    false
}

#[tauri::command]
fn launch_address(app: tauri::AppHandle) -> Option<String> {
    #[cfg(debug_assertions)]
    if let Some(value) = std::env::var("POLYMUX_TEST_ADDRESS")
        .ok()
        .filter(|value| !value.is_empty())
    {
        return Some(value);
    }
    #[cfg(target_os = "macos")]
    {
        // scripts/install.sh points the installed app at the local server.
        let identifier = app.config().identifier.clone();
        if let Ok(output) = std::process::Command::new("/usr/bin/defaults")
            .args(["read", &identifier, "serverAddress"])
            .output()
        {
            if output.status.success() {
                let value = String::from_utf8_lossy(&output.stdout).trim().to_string();
                if !value.is_empty() {
                    return Some(value);
                }
            }
        }
    }
    let _ = app;
    None
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let mut context = tauri::generate_context!();
    let isolated = isolated();
    if isolated {
        for window in &mut context.config_mut().app.windows {
            window.incognito = true;
            window.focus = false;
        }
    }
    let builder = tauri::Builder::default();
    #[cfg(target_os = "macos")]
    let builder = if isolated {
        builder.activate_ignoring_other_apps(false)
    } else {
        builder
    };
    builder
        .setup(|app| {
            let settings = MenuItemBuilder::with_id("settings", "Settings…")
                .accelerator("CmdOrCtrl+,")
                .build(app)?;
            let updates =
                MenuItemBuilder::with_id("check-updates", "Check for Updates…").build(app)?;
            let app_menu = SubmenuBuilder::new(app, "Polymux")
                .item(&PredefinedMenuItem::about(app, None, None)?)
                .separator()
                .item(&settings)
                .item(&updates)
                .separator()
                .item(&PredefinedMenuItem::services(app, None)?)
                .separator()
                .item(&PredefinedMenuItem::hide(app, None)?)
                .item(&PredefinedMenuItem::hide_others(app, None)?)
                .item(&PredefinedMenuItem::show_all(app, None)?)
                .separator()
                .item(&PredefinedMenuItem::quit(app, None)?)
                .build()?;
            let edit_menu = SubmenuBuilder::new(app, "Edit")
                .item(&PredefinedMenuItem::undo(app, None)?)
                .item(&PredefinedMenuItem::redo(app, None)?)
                .separator()
                .item(&PredefinedMenuItem::cut(app, None)?)
                .item(&PredefinedMenuItem::copy(app, None)?)
                .item(&PredefinedMenuItem::paste(app, None)?)
                .item(&PredefinedMenuItem::select_all(app, None)?)
                .build()?;
            let window_menu = SubmenuBuilder::new(app, "Window")
                .item(&PredefinedMenuItem::minimize(app, None)?)
                .item(&PredefinedMenuItem::maximize(app, None)?)
                .separator()
                .item(&PredefinedMenuItem::close_window(app, None)?)
                .build()?;
            let menu = MenuBuilder::new(app)
                .item(&app_menu)
                .item(&edit_menu)
                .item(&window_menu)
                .build()?;
            app.set_menu(menu)?;
            if let Some((width, height)) = test_window_size() {
                if let Some(window) = app.get_webview_window("main") {
                    let _ = window.set_size(tauri::LogicalSize::new(width, height));
                }
            }
            Ok(())
        })
        .on_menu_event(|app, event| match event.id().as_ref() {
            "settings" => {
                let _ = app.emit("open-settings", ());
            }
            "check-updates" => {
                let _ = app.emit("check-updates", ());
            }
            _ => {}
        })
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_notification::init())
        .plugin(tauri_plugin_updater::Builder::new().build())
        .plugin(tauri_plugin_process::init())
        .plugin(tauri_plugin_http::init())
        .on_page_load(|webview, payload| {
            #[cfg(target_os = "macos")]
            if payload.event() == tauri::webview::PageLoadEvent::Started {
                dictation::close(webview.label());
            }
            #[cfg(not(target_os = "macos"))]
            let _ = (webview, payload);
        })
        .on_window_event(|window, event| {
            #[cfg(target_os = "macos")]
            if matches!(event, tauri::WindowEvent::Destroyed) {
                dictation::close(window.label());
            }
            #[cfg(not(target_os = "macos"))]
            let _ = (window, event);
        })
        .invoke_handler(tauri::generate_handler![
            local_token,
            machine_name,
            launch_address,
            start_save,
            save_chunk,
            finish_save,
            start_preview,
            preview_chunk,
            finish_preview,
            start_dictation,
            stop_dictation,
            location::current_location
        ])
        .run(context)
        .expect("error while running tauri application");
}

#[tauri::command]
fn start_dictation(
    window: tauri::WebviewWindow,
    on_event: tauri::ipc::Channel<serde_json::Value>,
) -> Result<u64, String> {
    #[cfg(target_os = "macos")]
    return dictation::start(window, on_event);
    #[cfg(not(target_os = "macos"))]
    {
        let _ = (window, on_event);
        Err("Dictation isn’t available right now.".into())
    }
}

#[tauri::command]
fn stop_dictation(window: tauri::WebviewWindow, id: u64, finalize: bool) {
    #[cfg(target_os = "macos")]
    dictation::stop(window.label(), id, finalize);
    #[cfg(not(target_os = "macos"))]
    let _ = (window, id, finalize);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[cfg(target_os = "macos")]
    #[test]
    fn owner_token_is_available_only_for_local_hosts() {
        assert!(local_host("localhost"));
        assert!(local_host("127.0.0.1"));
        assert!(local_host("[::1]"));
        assert!(!local_host("example.invalid"));
        assert!(!local_host("192.0.2.123"));
    }

    #[test]
    fn save_uses_a_safe_suggested_name() {
        assert_eq!(save_name("../report.txt"), "report.txt");
        assert_eq!(save_name("C:\\folder\\résumé.txt"), "résumé.txt");
        assert_eq!(save_name(".."), "Attachment");
    }

    #[test]
    fn cancelled_and_failed_saves_leave_no_partial_file() {
        let directory = tempfile::tempdir().unwrap();
        let destination = directory.path().join("result.txt");
        std::fs::write(&destination, b"original").unwrap();
        let mut pending = SaveFile::new(destination.clone()).unwrap();
        pending.file.write_all(b"partial").unwrap();
        drop(pending);
        assert_eq!(std::fs::read(&destination).unwrap(), b"original");
        assert_eq!(std::fs::read_dir(directory.path()).unwrap().count(), 1);

        let mut pending = SaveFile::new(destination.clone()).unwrap();
        pending.file.write_all(b"replacement").unwrap();
        pending.commit().unwrap();
        assert_eq!(std::fs::read(&destination).unwrap(), b"replacement");
        assert_eq!(std::fs::read_dir(directory.path()).unwrap().count(), 1);

        let blocked = directory.path().join("folder");
        std::fs::create_dir(&blocked).unwrap();
        let mut pending = SaveFile::new(blocked.clone()).unwrap();
        pending.file.write_all(b"partial").unwrap();
        assert!(pending.commit().is_err());
        assert!(blocked.is_dir());
        assert_eq!(std::fs::read_dir(directory.path()).unwrap().count(), 2);
    }
}
