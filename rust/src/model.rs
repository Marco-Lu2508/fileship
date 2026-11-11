//! Datenmodelle — JSON-kompatibel zum Go-Original (gleiche Feldnamen).

use chrono::{DateTime, Local};
use serde::{Serialize, Serializer};

/// Serialisiert DateTime<Local> wie Go time.Time (RFC3339Nano mit Offset).
pub mod ts {
    use super::*;
    pub fn serialize<S: Serializer>(v: &DateTime<Local>, s: S) -> Result<S::Ok, S::Error> {
        s.serialize_str(&fmt_rfc3339(v))
    }
    pub fn opt_serialize<S: Serializer>(v: &Option<DateTime<Local>>, s: S) -> Result<S::Ok, S::Error> {
        match v {
            Some(t) => s.serialize_some(&fmt_rfc3339(t)),
            None => s.serialize_none(),
        }
    }
}

pub fn fmt_rfc3339(t: &DateTime<Local>) -> String {
    use chrono::SecondsFormat;
    // Wie Go RFC3339Nano: minimale Nachkommastellen, Offset +01:00 (Z nur bei UTC ist hier ok).
    t.to_rfc3339_opts(SecondsFormat::AutoSi, false)
}

/// Parst DATETIME-Werte aus Go-generierten SQLite-DBs (mattn/go-sqlite3 speichert
/// "2006-01-02 15:04:05.999999999-07:00") sowie RFC3339 und naive Formate.
pub fn parse_db_time(s: &str) -> Option<DateTime<Local>> {
    use chrono::{FixedOffset, NaiveDateTime, TimeZone};
    let s = s.trim();
    // RFC3339
    if let Ok(dt) = DateTime::parse_from_rfc3339(s) {
        return Some(dt.with_timezone(&Local));
    }
    // Go sqlite: "2006-01-02 15:04:05.999999999-07:00" / ohne Zone / ohne Fraction
    for fmt in [
        "%Y-%m-%d %H:%M:%S%.f%:z",
        "%Y-%m-%d %H:%M:%S%:z",
        "%Y-%m-%d %H:%M:%S%.f %:z",
    ] {
        if let Ok(dt) = chrono::DateTime::parse_from_str(s, fmt) {
            return Some(dt.with_timezone(&Local));
        }
    }
    for fmt in ["%Y-%m-%d %H:%M:%S%.f", "%Y-%m-%d %H:%M:%S", "%Y-%m-%dT%H:%M:%S%.f", "%Y-%m-%dT%H:%M:%S"] {
        if let Ok(nd) = NaiveDateTime::parse_from_str(s, fmt) {
            return Some(Local.from_local_datetime(&nd).earliest().unwrap_or_else(|| {
                let off = FixedOffset::east_opt(0).unwrap();
                off.from_utc_datetime(&nd).with_timezone(&Local)
            }));
        }
    }
    None
}

/// Format für SQLite — wie Go (mit Offset), damit DBs austauschbar bleiben.
pub fn fmt_db_time(t: &DateTime<Local>) -> String {
    t.format("%Y-%m-%d %H:%M:%S%.f%:z").to_string()
}

pub fn now() -> DateTime<Local> {
    Local::now()
}

#[derive(Debug, Clone, Serialize)]
pub struct User {
    pub id: i64,
    pub username: String,
    #[serde(skip_serializing)]
    pub password_hash: String,
    pub is_admin: bool,
    pub root_path: String,
    #[serde(with = "ts")]
    pub created_at: DateTime<Local>,
}

#[derive(Debug, Clone, Serialize)]
pub struct UserWithQuota {
    pub id: i64,
    pub username: String,
    pub is_admin: bool,
    pub root_path: String,
    pub quota_bytes: i64,
    pub allowed_types: String,
    #[serde(with = "ts")]
    pub created_at: DateTime<Local>,
    #[serde(skip_serializing_if = "is_zero")]
    pub disk_usage: i64,
}

fn is_zero(v: &i64) -> bool {
    *v == 0
}
fn is_false(v: &bool) -> bool {
    !*v
}

#[derive(Debug, Clone, Serialize)]
pub struct FileInfo {
    pub name: String,
    pub path: String,
    pub size: i64,
    pub is_dir: bool,
    #[serde(with = "ts")]
    pub mod_time: DateTime<Local>,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub mime_type: String,
    #[serde(skip_serializing_if = "is_zero")]
    pub dir_size: i64,
    #[serde(skip_serializing_if = "is_false")]
    pub starred: bool,
}

#[derive(Debug, Clone)]
pub struct ListOptions {
    pub path: String,
    pub search: String,
    pub sort_by: String,
    pub sort_asc: bool,
    pub page: i64,
    pub per_page: i64,
}

#[derive(Debug, Clone, Serialize)]
pub struct ListResult {
    pub files: Vec<FileInfo>,
    pub total: i64,
    pub page: i64,
    pub per_page: i64,
}

#[derive(Debug, Clone, Serialize)]
pub struct AuditEntry {
    pub id: i64,
    pub user_id: i64,
    pub username: String,
    pub action: String,
    pub path: String,
    pub ip: String,
    #[serde(with = "ts")]
    pub created_at: DateTime<Local>,
}

#[derive(Debug, Clone, Serialize)]
pub struct ShareLink {
    pub token: String,
    pub path: String,
    pub is_dir: bool,
    pub created_by: i64,
    #[serde(serialize_with = "ts::opt_serialize", skip_serializing_if = "Option::is_none")]
    pub expires_at: Option<DateTime<Local>>,
    #[serde(with = "ts")]
    pub created_at: DateTime<Local>,
    #[serde(skip_serializing)]
    pub password_hash: String,
    pub has_password: bool,
    pub download_limit: i64,
    pub download_count: i64,
    pub allow_upload: bool,
    pub allow_edit: bool,
}

#[derive(Debug, Clone, Serialize)]
pub struct ApiToken {
    pub id: i64,
    pub user_id: i64,
    pub name: String,
    pub prefix: String,
    #[serde(serialize_with = "ts::opt_serialize", skip_serializing_if = "Option::is_none")]
    pub last_used: Option<DateTime<Local>>,
    #[serde(serialize_with = "ts::opt_serialize", skip_serializing_if = "Option::is_none")]
    pub expires_at: Option<DateTime<Local>>,
    #[serde(with = "ts")]
    pub created_at: DateTime<Local>,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub token: String,
}

#[derive(Debug, Clone, Serialize)]
pub struct Favorite {
    pub id: i64,
    pub user_id: i64,
    pub path: String,
    pub name: String,
    pub is_dir: bool,
    #[serde(with = "ts")]
    pub created_at: DateTime<Local>,
}

#[derive(Debug, Clone, serde::Deserialize, Serialize)]
pub struct PathPermission {
    #[serde(default)]
    pub id: i64,
    pub user_id: i64,
    pub path: String,
    pub can_read: bool,
    pub can_write: bool,
    pub can_delete: bool,
    pub can_share: bool,
}

#[derive(Debug, Clone, Serialize)]
pub struct TrashItem {
    pub id: i64,
    pub user_id: i64,
    pub orig_path: String,
    pub trash_name: String,
    pub name: String,
    pub is_dir: bool,
    pub size: i64,
    #[serde(with = "ts")]
    pub deleted_at: DateTime<Local>,
    #[serde(with = "ts")]
    pub expires_at: DateTime<Local>,
}

#[derive(Debug, Clone, Serialize)]
pub struct UserSource {
    pub id: i64,
    pub user_id: i64,
    pub name: String,
    pub root_path: String,
    pub is_default: bool,
    pub sort_order: i64,
}

#[derive(Debug, Clone, Serialize)]
pub struct AgentDevice {
    pub id: i64,
    pub user_id: i64,
    pub name: String,
    pub prefix: String,
    pub root_path: String,
    pub hostname: String,
    pub os_info: String,
    pub disk_usage: i64,
    pub online: bool,
    #[serde(serialize_with = "ts::opt_serialize", skip_serializing_if = "Option::is_none")]
    pub last_seen: Option<DateTime<Local>>,
    #[serde(with = "ts")]
    pub created_at: DateTime<Local>,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub token: String,
}

#[derive(Debug, Clone, Serialize)]
pub struct WsEvent {
    #[serde(rename = "type")]
    pub typ: String,
    pub path: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub payload: Option<serde_json::Value>,
}
