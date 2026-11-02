use std::ffi::{c_char, CStr};
use std::sync::mpsc::{self, Sender};
use std::sync::{Mutex, OnceLock};
use std::time::Duration;

use serde_json::Value;

static PENDING: OnceLock<Mutex<Option<Sender<Value>>>> = OnceLock::new();

fn pending() -> &'static Mutex<Option<Sender<Value>>> {
    PENDING.get_or_init(|| Mutex::new(None))
}

#[cfg(target_os = "macos")]
extern "C" {
    fn polymux_location_start(callback: extern "C" fn(*const c_char));
}

#[cfg(target_os = "macos")]
extern "C" fn event(json: *const c_char) {
    if json.is_null() {
        return;
    }
    let Ok(data) = unsafe { CStr::from_ptr(json) }.to_str() else {
        return;
    };
    let Ok(value) = serde_json::from_str::<Value>(data) else {
        return;
    };
    if let Some(sender) = pending().lock().ok().and_then(|mut guard| guard.take()) {
        let _ = sender.send(value);
    }
}

#[tauri::command]
pub async fn current_location() -> Result<Value, String> {
    #[cfg(target_os = "macos")]
    {
        return tauri::async_runtime::spawn_blocking(|| {
            let (sender, receiver) = mpsc::channel();
            *pending()
                .lock()
                .map_err(|_| "Location is unavailable.".to_string())? = Some(sender);
            unsafe { polymux_location_start(event) };
            match receiver.recv_timeout(Duration::from_secs(25)) {
                Ok(value) => match value.get("error").and_then(Value::as_str) {
                    Some(error) => Err(error.to_string()),
                    None => Ok(value),
                },
                Err(_) => Err("Couldn’t get a location fix.".into()),
            }
        })
        .await
        .map_err(|_| "Location is unavailable.".to_string())?;
    }
    #[cfg(not(target_os = "macos"))]
    Err("Location isn’t available on this platform.".into())
}
