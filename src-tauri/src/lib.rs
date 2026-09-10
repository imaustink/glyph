use serde::{Deserialize, Serialize};
use tauri::menu::{Menu, MenuItem, PredefinedMenuItem, Submenu};
use tauri::{AppHandle, Manager, WebviewUrl, WebviewWindowBuilder};
use tauri_plugin_updater::UpdaterExt;

#[derive(Serialize, Deserialize, Default)]
struct AppConfig {
    server_url: Option<String>,
}

fn config_path(app: &AppHandle) -> std::path::PathBuf {
    let dir = app
        .path()
        .app_config_dir()
        .expect("no app config dir available");
    std::fs::create_dir_all(&dir).ok();
    dir.join("config.json")
}

fn load_config(app: &AppHandle) -> AppConfig {
    std::fs::read_to_string(config_path(app))
        .ok()
        .and_then(|raw| serde_json::from_str(&raw).ok())
        .unwrap_or_default()
}

fn save_config(app: &AppHandle, config: &AppConfig) {
    if let Ok(raw) = serde_json::to_string_pretty(config) {
        let _ = std::fs::write(config_path(app), raw);
    }
}

fn initial_webview_url(app: &AppHandle) -> WebviewUrl {
    match load_config(app).server_url {
        Some(url) => match url::Url::parse(&url) {
            Ok(parsed) => WebviewUrl::External(parsed),
            Err(_) => WebviewUrl::App("index.html".into()),
        },
        None => WebviewUrl::App("index.html".into()),
    }
}

#[tauri::command]
fn set_server_url(app: AppHandle, url: String) -> Result<(), String> {
    let trimmed = url.trim();
    if !(trimmed.starts_with("http://") || trimmed.starts_with("https://")) {
        return Err("Server URL must start with http:// or https://".into());
    }
    let parsed = url::Url::parse(trimmed).map_err(|e| e.to_string())?;

    save_config(
        &app,
        &AppConfig {
            server_url: Some(trimmed.to_string()),
        },
    );

    if let Some(window) = app.get_webview_window("main") {
        window.navigate(parsed).map_err(|e| e.to_string())?;
    }
    Ok(())
}

fn reset_server(app: &AppHandle) {
    save_config(app, &AppConfig::default());
    let _ = app.restart();
}

async fn check_for_updates(app: &AppHandle) {
    let updater = match app.updater() {
        Ok(updater) => updater,
        Err(e) => {
            log::error!("updater unavailable: {e}");
            return;
        }
    };

    match updater.check().await {
        Ok(Some(update)) => {
            log::info!("update {} available, downloading", update.version);
            if let Err(e) = update.download_and_install(|_, _| {}, || {}).await {
                log::error!("update install failed: {e}");
                return;
            }
            let _ = app.restart();
        }
        Ok(None) => log::info!("no update available"),
        Err(e) => log::error!("update check failed: {e}"),
    }
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_updater::Builder::new().build())
        .plugin(tauri_plugin_process::init())
        .invoke_handler(tauri::generate_handler![set_server_url])
        .setup(|app| {
            if cfg!(debug_assertions) {
                app.handle().plugin(
                    tauri_plugin_log::Builder::default()
                        .level(log::LevelFilter::Info)
                        .build(),
                )?;
            }

            let handle = app.handle().clone();
            let url = initial_webview_url(&handle);
            WebviewWindowBuilder::new(app, "main", url)
                .title("Glyph")
                .inner_size(1200.0, 800.0)
                .min_inner_size(600.0, 400.0)
                .build()?;

            let change_server =
                MenuItem::with_id(app, "change_server", "Change Server URL…", true, None::<&str>)?;
            let check_updates = MenuItem::with_id(
                app,
                "check_updates",
                "Check for Updates…",
                true,
                None::<&str>,
            )?;
            let quit = PredefinedMenuItem::quit(app, None)?;
            let app_menu = Submenu::with_items(
                app,
                "Glyph",
                true,
                &[&change_server, &check_updates, &quit],
            )?;
            let menu = Menu::with_items(app, &[&app_menu])?;
            app.set_menu(menu)?;

            let update_handle = app.handle().clone();
            tauri::async_runtime::spawn(async move {
                check_for_updates(&update_handle).await;
            });

            Ok(())
        })
        .on_menu_event(|app, event| match event.id().as_ref() {
            "change_server" => reset_server(app),
            "check_updates" => {
                let handle = app.clone();
                tauri::async_runtime::spawn(async move {
                    check_for_updates(&handle).await;
                });
            }
            _ => {}
        })
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
