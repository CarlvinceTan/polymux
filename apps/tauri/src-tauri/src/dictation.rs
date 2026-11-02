use std::ffi::{c_char, CStr};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Mutex, OnceLock};
use tauri::{ipc::Channel, WebviewWindow};

type Owner = (u64, String, Channel<serde_json::Value>);
static OWNER: OnceLock<Mutex<Option<Owner>>> = OnceLock::new();
static NEXT: AtomicU64 = AtomicU64::new(1);

extern "C" {
    fn polymux_dictation_start(id: u64, callback: extern "C" fn(*const c_char));
    fn polymux_dictation_stop(id: u64, finalize: bool);
}

fn owner() -> &'static Mutex<Option<Owner>> {
    OWNER.get_or_init(|| Mutex::new(None))
}

extern "C" fn event(json: *const c_char) {
    if json.is_null() {
        return;
    }
    let Ok(data) = unsafe { CStr::from_ptr(json) }.to_str() else {
        return;
    };
    let Ok(value) = serde_json::from_str::<serde_json::Value>(data) else {
        return;
    };
    let Some(id) = value.get("id").and_then(|id| id.as_u64()) else {
        return;
    };
    let Ok(mut guard) = owner().lock() else {
        return;
    };
    let Some((owned, _, channel)) = guard.as_ref() else {
        return;
    };
    if *owned != id {
        return;
    }
    let channel = channel.clone();
    if value.get("kind").and_then(|kind| kind.as_str()) == Some("ended") {
        *guard = None;
    }
    drop(guard);
    if channel.send(value).is_err() {
        unsafe {
            polymux_dictation_stop(id, false);
        }
    }
}

pub fn start(window: WebviewWindow, channel: Channel<serde_json::Value>) -> Result<u64, String> {
    let mut guard = owner()
        .lock()
        .map_err(|_| "Dictation isn’t available right now.")?;
    if guard.is_some() {
        return Err("Dictation is already active.".into());
    }
    let id = NEXT.fetch_add(1, Ordering::Relaxed);
    *guard = Some((id, window.label().to_owned(), channel));
    unsafe {
        polymux_dictation_start(id, event);
    }
    Ok(id)
}

pub fn stop(window: &str, id: u64, finalize: bool) {
    let Ok(guard) = owner().lock() else {
        return;
    };
    if guard
        .as_ref()
        .is_some_and(|(owned, owner, _)| *owned == id && owner == window)
    {
        unsafe {
            polymux_dictation_stop(id, finalize);
        }
    }
}

pub fn close(window: &str) {
    close_with(window, |id| unsafe { polymux_dictation_stop(id, false) });
}

fn close_with(window: &str, stop: impl FnOnce(u64)) {
    let Ok(guard) = owner().lock() else {
        return;
    };
    let id = guard
        .as_ref()
        .filter(|(_, owner, _)| owner == window)
        .map(|(id, _, _)| *id);
    drop(guard);
    if let Some(id) = id {
        stop(id);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::ffi::CString;
    use std::sync::Arc;
    use tauri::ipc::InvokeResponseBody;

    #[test]
    fn events_are_private_to_the_owner_and_navigation_stops_only_its_capture() {
        let received = Arc::new(Mutex::new(Vec::new()));
        let output = received.clone();
        let channel = Channel::new(move |body| {
            if let InvokeResponseBody::Json(value) = body {
                output.lock().unwrap().push(value);
            }
            Ok(())
        });
        *owner().lock().unwrap() = Some((42, "main".into(), channel));
        let mut stops = Vec::new();
        close_with("other", |id| stops.push(id));
        close_with("main", |id| stops.push(id));
        assert_eq!(stops, vec![42]);
        for value in [
            r#"{"id":43,"kind":"transcript","text":"other"}"#,
            r#"{"id":42,"kind":"transcript","text":"private"}"#,
            r#"{"id":42,"kind":"ended"}"#,
            r#"{"id":42,"kind":"transcript","text":"late"}"#,
        ] {
            event(CString::new(value).unwrap().as_ptr());
        }
        let received = received.lock().unwrap();
        assert_eq!(received.len(), 2);
        assert!(received[0].contains("private"));
        assert!(owner().lock().unwrap().is_none());
    }
}
