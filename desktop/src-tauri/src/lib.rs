use std::path::PathBuf;
use std::process::Child;
use std::sync::{Arc, Mutex};
use std::time::Duration;
use percent_encoding::{utf8_percent_encode, NON_ALPHANUMERIC};
use tauri::menu::{Menu, MenuItem};
use tauri::tray::{MouseButton, TrayIconBuilder, TrayIconEvent};
use tauri::{AppHandle, Manager, WebviewUrl, WebviewWindowBuilder, WindowEvent};

struct AppState {
    go_child: Arc<Mutex<Option<Child>>>,
    server_base_url: String,
    http_client: reqwest::blocking::Client,
}

/// Finds the HushDrop Go binary across common paths.
fn locate_go_binary() -> Option<PathBuf> {
    let mut candidates = Vec::new();

    // 1. Next to current executable
    if let Ok(current_exe) = std::env::current_exe() {
        if let Some(parent) = current_exe.parent() {
            candidates.push(parent.join("HushDrop.exe"));
            candidates.push(parent.join("HushDrop"));
        }
    }

    // 2. Cwd and relative dist
    candidates.push(PathBuf::from("HushDrop.exe"));
    candidates.push(PathBuf::from("dist/HushDrop.exe"));
    candidates.push(PathBuf::from("../dist/HushDrop.exe"));
    candidates.push(PathBuf::from("../../dist/HushDrop.exe"));
    candidates.push(PathBuf::from("C:/dev/hushdrop/dist/HushDrop.exe"));

    for candidate in candidates {
        if candidate.exists() && candidate.is_file() {
            return Some(candidate);
        }
    }
    None
}

/// Spawns the Go binary in --portable mode.
fn start_go_backend(child_holder: Arc<Mutex<Option<Child>>>) -> Result<(), String> {
    let binary_path = locate_go_binary()
        .ok_or_else(|| "Не удалось найти исполняемый файл HushDrop.exe".to_string())?;

    let work_dir = binary_path
        .parent()
        .map(|p| p.to_path_buf())
        .unwrap_or_else(|| PathBuf::from("."));

    let child = std::process::Command::new(&binary_path)
        .arg("--portable")
        .current_dir(&work_dir)
        .spawn()
        .map_err(|e| format!("Ошибка запуска {}: {}", binary_path.display(), e))?;

    let mut lock = child_holder.lock().unwrap();
    *lock = Some(child);
    Ok(())
}

/// Gracefully kills the Go backend.
fn stop_go_backend(child_holder: &Arc<Mutex<Option<Child>>>) {
    if let Ok(mut lock) = child_holder.lock() {
        if let Some(mut child) = lock.take() {
            let _ = child.kill();
        }
    }
}

/// Opens the downloads directory in system file explorer.
fn open_downloads_folder() {
    let mut downloads_dir = PathBuf::from("data/downloads");

    if let Ok(current_exe) = std::env::current_exe() {
        if let Some(parent) = current_exe.parent() {
            let candidate = parent.join("data/downloads");
            if candidate.exists() {
                downloads_dir = candidate;
            }
        }
    }

    if !downloads_dir.exists() {
        let _ = std::fs::create_dir_all(&downloads_dir);
    }

    #[cfg(target_os = "windows")]
    {
        let _ = std::process::Command::new("explorer")
            .arg(&downloads_dir)
            .spawn();
    }

    #[cfg(target_os = "macos")]
    {
        let _ = std::process::Command::new("open")
            .arg(&downloads_dir)
            .spawn();
    }

    #[cfg(target_os = "linux")]
    {
        let _ = std::process::Command::new("xdg-open")
            .arg(&downloads_dir)
            .spawn();
    }
}

/// Revokes all active sessions on the server.
fn revoke_sessions(client: &reqwest::blocking::Client, server_url: &str) {
    let revoke_url = format!("{}/api/revoke", server_url);
    let _ = client.post(&revoke_url).send();
}

/// Displays the pairing QR and PIN window.
fn open_qr_window(app: &AppHandle, client: &reqwest::blocking::Client, server_url: &str) {
    if let Some(window) = app.get_webview_window("qr_window") {
        let _ = window.show();
        let _ = window.set_focus();
        return;
    }

    // Query /api/pair/info
    let info_url = format!("{}/api/pair/info", server_url);
    let (fingerprint, token) = match client.get(&info_url).send() {
        Ok(resp) => {
            if let Ok(json) = resp.json::<serde_json::Value>() {
                (
                    json["fingerprint"].as_str().unwrap_or("").to_string(),
                    json["token"].as_str().unwrap_or("").to_string(),
                )
            } else {
                ("".to_string(), "".to_string())
            }
        }
        Err(_) => ("".to_string(), "".to_string()),
    };

    let html = format!(
        r#"<!DOCTYPE html>
<html lang="ru">
<head>
  <meta charset="UTF-8">
  <title>HushDrop — QR и PIN</title>
  <style>
    body {{
      background: #090d16;
      color: #f1f5f9;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      margin: 0;
      padding: 24px;
      display: flex;
      flex-direction: column;
      align-items: center;
      text-align: center;
    }}
    .badge {{
      display: inline-block;
      background: rgba(16, 185, 129, 0.15);
      color: #10b981;
      border: 1px solid rgba(16, 185, 129, 0.3);
      padding: 4px 12px;
      border-radius: 999px;
      font-size: 11px;
      font-weight: 600;
      margin-bottom: 12px;
    }}
    h1 {{ font-size: 18px; margin: 0 0 6px 0; color: #fff; }}
    p.sub {{ font-size: 12px; color: #94a3b8; margin: 0 0 20px 0; }}
    .card {{
      background: #0f172a;
      border: 1px solid #1e293b;
      border-radius: 16px;
      padding: 16px;
      width: 100%;
      box-sizing: border-box;
      margin-bottom: 16px;
    }}
    .pin-label {{ font-size: 11px; text-transform: uppercase; color: #94a3b8; font-weight: 700; margin-bottom: 6px; }}
    .pin-value {{ font-family: monospace; font-size: 24px; color: #10b981; letter-spacing: 0.25em; }}
    .fp-label {{ font-size: 11px; color: #64748b; margin-top: 10px; margin-bottom: 4px; }}
    .fp-value {{ font-family: monospace; font-size: 10px; color: #94a3b8; word-break: break-all; background: #090d16; padding: 8px; border-radius: 8px; border: 1px solid #1e293b; }}
    .btn {{
      background: #10b981;
      color: #fff;
      border: none;
      padding: 10px 20px;
      border-radius: 10px;
      font-size: 12px;
      font-weight: 600;
      cursor: pointer;
      text-decoration: none;
      display: inline-block;
      margin-top: 10px;
    }}
    .btn:hover {{ background: #059669; }}
  </style>
</head>
<body>
  <div class="badge">LAN ONLY</div>
  <h1>Сопряжение с мобильным устройством</h1>
  <p class="sub">Отсканируйте камерой или введите параметры в приложении</p>
  <div class="card">
    <div class="pin-label">Отпечаток TLS 1.3 (SHA-256)</div>
    <div class="fp-value">{}</div>
    <div class="fp-label">Одноразовый токен:</div>
    <div class="fp-value">{}</div>
  </div>
  <a class="btn" href="http://127.0.0.1:8080/cert" target="_blank">Скачать сертификат (:8080/cert)</a>
</body>
</html>"#,
        fingerprint, token
    );

    let encoded = utf8_percent_encode(&html, NON_ALPHANUMERIC).to_string();
    let data_url = format!("data:text/html;charset=utf-8,{}", encoded);

    if let Ok(parsed_url) = url::Url::parse(&data_url) {
        let _ = WebviewWindowBuilder::new(
            app,
            "qr_window",
            WebviewUrl::External(parsed_url),
        )
        .title("HushDrop — QR и PIN для сопряжения")
        .inner_size(460.0, 540.0)
        .resizable(false)
        .always_on_top(true)
        .build();
    }
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    // 1. Ignore self-signed certificates for local loopback in WebView2
    std::env::set_var(
        "WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS",
        "--ignore-certificate-errors",
    );

    let go_child_holder = Arc::new(Mutex::new(None));
    let server_url = "https://127.0.0.1:8443".to_string();

    // Configure resilient HTTP client for loopback self-signed cert
    let http_client = reqwest::blocking::Client::builder()
        .danger_accept_invalid_certs(true)
        .timeout(Duration::from_secs(5))
        .build()
        .unwrap_or_else(|_| reqwest::blocking::Client::new());

    // 2. Start Go backend
    let _ = start_go_backend(go_child_holder.clone());

    let state = AppState {
        go_child: go_child_holder.clone(),
        server_base_url: server_url.clone(),
        http_client: http_client.clone(),
    };

    let go_child_exit = go_child_holder.clone();

    tauri::Builder::default()
        .manage(state)
        .setup(move |app| {
            let app_handle = app.handle().clone();

            // Setup system tray menu
            let show_hide = MenuItem::with_id(app, "toggle_visible", "Показать/Скрыть", true, None::<&str>)?;
            let open_qr = MenuItem::with_id(app, "open_qr", "Открыть QR / PIN", true, None::<&str>)?;
            let open_downloads = MenuItem::with_id(app, "open_downloads", "Папка downloads", true, None::<&str>)?;
            let forget_all = MenuItem::with_id(app, "forget_all", "Забыть всё", true, None::<&str>)?;
            let quit = MenuItem::with_id(app, "quit", "Выход", true, None::<&str>)?;

            let tray_menu = Menu::with_items(
                app,
                &[
                    &show_hide,
                    &open_qr,
                    &open_downloads,
                    &forget_all,
                    &quit,
                ],
            )?;

            let icon = app.default_window_icon().cloned().unwrap();
            let _tray = TrayIconBuilder::with_id("main-tray")
                .icon(icon)
                .menu(&tray_menu)
                .show_menu_on_left_click(false)
                .tooltip("HushDrop — Твои файлы. Твоя сеть.")
                .on_menu_event(move |app, event| {
                    let state = app.state::<AppState>();
                    match event.id.as_ref() {
                        "toggle_visible" => {
                            if let Some(window) = app.get_webview_window("main") {
                                if window.is_visible().unwrap_or(false) {
                                    let _ = window.hide();
                                } else {
                                    let _ = window.show();
                                    let _ = window.unminimize();
                                    let _ = window.set_focus();
                                }
                            }
                        }
                        "open_qr" => {
                            open_qr_window(app, &state.http_client, &state.server_base_url);
                        }
                        "open_downloads" => {
                            open_downloads_folder();
                        }
                        "forget_all" => {
                            revoke_sessions(&state.http_client, &state.server_base_url);
                            if let Some(window) = app.get_webview_window("main") {
                                let _ = window.eval("window.location.reload()");
                            }
                        }
                        "quit" => {
                            revoke_sessions(&state.http_client, &state.server_base_url);
                            stop_go_backend(&state.go_child);
                            app.exit(0);
                        }
                        _ => {}
                    }
                })
                .on_tray_icon_event(|tray, event| {
                    if let TrayIconEvent::Click { button: MouseButton::Left, .. } = event {
                        let app = tray.app_handle();
                        if let Some(window) = app.get_webview_window("main") {
                            if window.is_visible().unwrap_or(false) {
                                let _ = window.hide();
                            } else {
                                let _ = window.show();
                                let _ = window.unminimize();
                                let _ = window.set_focus();
                            }
                        }
                    }
                })
                .build(app)?;

            // Open splashscreen window (400x400) with animated SVG logo
            let splash_html = include_str!("../../../frontend/public/splashscreen.html");
            let splash_encoded = utf8_percent_encode(splash_html, NON_ALPHANUMERIC).to_string();
            let splash_data_url = format!("data:text/html;charset=utf-8,{}", splash_encoded);
            if let Ok(parsed_splash_url) = url::Url::parse(&splash_data_url) {
                let _ = WebviewWindowBuilder::new(
                    app,
                    "splashscreen",
                    WebviewUrl::External(parsed_splash_url),
                )
                .title("HushDrop")
                .inner_size(400.0, 400.0)
                .resizable(false)
                .decorations(false)
                .center()
                .always_on_top(true)
                .build();
            }

            // Poll Go server readiness before revealing main window
            let check_client = http_client.clone();
            let check_url = format!("{}/health", server_url);
            let handle_clone = app_handle.clone();

            std::thread::spawn(move || {
                let start = std::time::Instant::now();
                for _ in 0..50 {
                    if check_client.get(&check_url).send().is_ok() {
                        break;
                    }
                    std::thread::sleep(Duration::from_millis(100));
                }

                // Minimum 1.6s display to let the intro animation finish cleanly
                let elapsed = start.elapsed();
                if elapsed < Duration::from_millis(1600) {
                    std::thread::sleep(Duration::from_millis(1600) - elapsed);
                }

                if let Some(window) = handle_clone.get_webview_window("main") {
                    let _ = window.show();
                    let _ = window.set_focus();
                }
                if let Some(splash_window) = handle_clone.get_webview_window("splashscreen") {
                    let _ = splash_window.close();
                }
            });

            Ok(())
        })
        .on_window_event(|window, event| {
            // Close-to-tray pattern
            if let WindowEvent::CloseRequested { api, .. } = event {
                if window.label() == "main" {
                    api.prevent_close();
                    let _ = window.hide();
                }
            }
        })
        .build(tauri::generate_context!())
        .expect("error while building tauri application")
        .run(move |_app_handle, event| {
            if let tauri::RunEvent::Exit = event {
                stop_go_backend(&go_child_exit);
            }
        });
}
