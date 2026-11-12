//! DB-Layer: rusqlite, Schema identisch zum Go-Original.
//! Eine Connection + Mutex (SQLite serialisiert ohnehin), WAL, FK an.
//! DATETIME als Go-Format gespeichert — DBs sind mit dem Original austauschbar.

use rand_core::Rng;
use crate::model::*;
use chrono::{DateTime, Local};
use rusqlite::{params, Connection};
use std::collections::HashSet;
use std::sync::Mutex;

pub struct Db {
    conn: Mutex<Connection>,
}

fn b(v: bool) -> i64 {
    v as i64
}
fn bb(v: i64) -> bool {
    v != 0
}

fn opt_time(s: Option<String>) -> Option<DateTime<Local>> {
    s.and_then(|x| parse_db_time(&x))
}

fn req_time(s: String) -> DateTime<Local> {
    parse_db_time(&s).unwrap_or_else(|| Local::now())
}

impl Db {
    pub fn open(path: &str) -> Result<Db, rusqlite::Error> {
        let conn = Connection::open(path)?;
        conn.execute_batch(
            "PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA synchronous=NORMAL; PRAGMA cache_size=-64000; PRAGMA temp_store=MEMORY;",
        )?;
        let db = Db { conn: Mutex::new(conn) };
        db.migrate()?;
        Ok(db)
    }

    fn exec(&self, sql: &str, p: impl rusqlite::Params) -> Result<usize, rusqlite::Error> {
        self.conn.lock().unwrap().execute(sql, p)
    }

    fn migrate(&self) -> Result<(), rusqlite::Error> {
        self.migrate_extra()?;
        self.exec_batch(
            "
            CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT UNIQUE NOT NULL, password_hash TEXT NOT NULL, is_admin BOOLEAN NOT NULL DEFAULT 0, root_path TEXT NOT NULL DEFAULT '/', created_at DATETIME NOT NULL);
            CREATE TABLE IF NOT EXISTS refresh_tokens (token TEXT PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, expires_at DATETIME NOT NULL);
            ",
        )?;
        self.migrate_quotas()?;
        self.migrate_index()?;
        self.migrate_tokens()?;
        self.migrate_favorites()?;
        self.migrate_permissions()?;
        self.migrate_trash()?;
        self.migrate_sources()?;
        self.migrate_agents()?;
        self.migrate_totp()?;
        Ok(())
    }

    fn exec_batch(&self, sql: &str) -> Result<(), rusqlite::Error> {
        self.conn.lock().unwrap().execute_batch(sql)
    }

    fn is_dup_col(e: &rusqlite::Error) -> bool {
        e.to_string().to_lowercase().contains("duplicate column")
    }

    fn migrate_extra(&self) -> Result<(), rusqlite::Error> {
        self.exec_batch(
            "
            CREATE TABLE IF NOT EXISTS audit_log (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL, username TEXT NOT NULL, action TEXT NOT NULL, path TEXT NOT NULL, ip TEXT NOT NULL, created_at DATETIME NOT NULL);
            CREATE TABLE IF NOT EXISTS share_links (token TEXT PRIMARY KEY, path TEXT NOT NULL, is_dir BOOLEAN NOT NULL DEFAULT 0, created_by INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, expires_at DATETIME, created_at DATETIME NOT NULL, password_hash TEXT NOT NULL DEFAULT '', download_limit INTEGER NOT NULL DEFAULT 0, download_count INTEGER NOT NULL DEFAULT 0, allow_upload BOOLEAN NOT NULL DEFAULT 0, allow_edit BOOLEAN NOT NULL DEFAULT 0);
            ",
        )?;
        for stmt in [
            "ALTER TABLE share_links ADD COLUMN password_hash TEXT NOT NULL DEFAULT ''",
            "ALTER TABLE share_links ADD COLUMN download_limit INTEGER NOT NULL DEFAULT 0",
            "ALTER TABLE share_links ADD COLUMN download_count INTEGER NOT NULL DEFAULT 0",
            "ALTER TABLE share_links ADD COLUMN allow_upload BOOLEAN NOT NULL DEFAULT 0",
            "ALTER TABLE share_links ADD COLUMN allow_edit BOOLEAN NOT NULL DEFAULT 0",
        ] {
            if let Err(e) = self.exec(stmt, []) {
                if !Self::is_dup_col(&e) {
                    return Err(e);
                }
            }
        }
        Ok(())
    }

    fn migrate_quotas(&self) -> Result<(), rusqlite::Error> {
        for stmt in [
            "ALTER TABLE users ADD COLUMN quota_bytes INTEGER NOT NULL DEFAULT 0",
            "ALTER TABLE users ADD COLUMN allowed_types TEXT NOT NULL DEFAULT ''",
        ] {
            if let Err(e) = self.exec(stmt, []) {
                if !Self::is_dup_col(&e) {
                    return Err(e);
                }
            }
        }
        Ok(())
    }

    fn migrate_index(&self) -> Result<(), rusqlite::Error> {
        self.exec_batch(
            "
            CREATE TABLE IF NOT EXISTS file_index (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, root_path TEXT NOT NULL, rel_path TEXT NOT NULL, name TEXT NOT NULL, is_dir BOOLEAN NOT NULL DEFAULT 0, size INTEGER NOT NULL DEFAULT 0, mod_time DATETIME NOT NULL, mime_type TEXT NOT NULL DEFAULT '', indexed_at DATETIME NOT NULL, UNIQUE(user_id, rel_path));
            CREATE INDEX IF NOT EXISTS idx_file_index_user ON file_index(user_id);
            CREATE INDEX IF NOT EXISTS idx_file_index_name ON file_index(user_id, name);
            CREATE INDEX IF NOT EXISTS idx_file_index_dir ON file_index(user_id, is_dir);
            CREATE TABLE IF NOT EXISTS dir_size_cache (root_path TEXT PRIMARY KEY, size_bytes INTEGER NOT NULL DEFAULT 0, updated_at DATETIME NOT NULL);
            ",
        )
    }

    fn migrate_tokens(&self) -> Result<(), rusqlite::Error> {
        self.exec_batch(
            "
            CREATE TABLE IF NOT EXISTS api_tokens (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, name TEXT NOT NULL, token_hash TEXT NOT NULL UNIQUE, token_prefix TEXT NOT NULL, last_used DATETIME, expires_at DATETIME, created_at DATETIME NOT NULL);
            CREATE INDEX IF NOT EXISTS idx_api_tokens_user ON api_tokens(user_id);
            CREATE INDEX IF NOT EXISTS idx_api_tokens_hash ON api_tokens(token_hash);
            ",
        )
    }

    fn migrate_favorites(&self) -> Result<(), rusqlite::Error> {
        self.exec_batch(
            "
            CREATE TABLE IF NOT EXISTS favorites (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, path TEXT NOT NULL, is_dir BOOLEAN NOT NULL DEFAULT 0, name TEXT NOT NULL, created_at DATETIME NOT NULL, UNIQUE(user_id, path));
            CREATE INDEX IF NOT EXISTS idx_favorites_user ON favorites(user_id);
            ",
        )
    }

    fn migrate_permissions(&self) -> Result<(), rusqlite::Error> {
        self.exec_batch(
            "
            CREATE TABLE IF NOT EXISTS path_permissions (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, path TEXT NOT NULL, can_read BOOLEAN NOT NULL DEFAULT 1, can_write BOOLEAN NOT NULL DEFAULT 1, can_delete BOOLEAN NOT NULL DEFAULT 1, can_share BOOLEAN NOT NULL DEFAULT 1, UNIQUE(user_id, path));
            CREATE INDEX IF NOT EXISTS idx_perms_user ON path_permissions(user_id);
            ",
        )
    }

    fn migrate_trash(&self) -> Result<(), rusqlite::Error> {
        self.exec_batch(
            "
            CREATE TABLE IF NOT EXISTS trash (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, orig_path TEXT NOT NULL, trash_name TEXT NOT NULL UNIQUE, name TEXT NOT NULL, is_dir BOOLEAN NOT NULL DEFAULT 0, size INTEGER NOT NULL DEFAULT 0, deleted_at DATETIME NOT NULL, expires_at DATETIME NOT NULL);
            CREATE INDEX IF NOT EXISTS idx_trash_user ON trash(user_id);
            CREATE INDEX IF NOT EXISTS idx_trash_expires ON trash(expires_at);
            ",
        )
    }

    fn migrate_sources(&self) -> Result<(), rusqlite::Error> {
        self.exec_batch(
            "
            CREATE TABLE IF NOT EXISTS user_sources (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, name TEXT NOT NULL, root_path TEXT NOT NULL, is_default BOOLEAN NOT NULL DEFAULT 0, sort_order INTEGER NOT NULL DEFAULT 0, UNIQUE(user_id, name));
            CREATE INDEX IF NOT EXISTS idx_sources_user ON user_sources(user_id);
            ",
        )
    }

    fn migrate_agents(&self) -> Result<(), rusqlite::Error> {
        self.exec_batch(
            "
            CREATE TABLE IF NOT EXISTS agent_devices (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, name TEXT NOT NULL, token_hash TEXT NOT NULL UNIQUE, token_prefix TEXT NOT NULL, root_path TEXT NOT NULL DEFAULT '/', hostname TEXT NOT NULL DEFAULT '', os_info TEXT NOT NULL DEFAULT '', disk_usage INTEGER NOT NULL DEFAULT 0, last_seen DATETIME, created_at DATETIME NOT NULL);
            CREATE INDEX IF NOT EXISTS idx_agent_devices_user ON agent_devices(user_id);
            ",
        )
    }

    fn migrate_totp(&self) -> Result<(), rusqlite::Error> {
        self.exec_batch(
            "
            CREATE TABLE IF NOT EXISTS totp_secrets (user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE, secret TEXT NOT NULL, enabled BOOLEAN NOT NULL DEFAULT 0, backup_codes TEXT NOT NULL DEFAULT '', created_at DATETIME NOT NULL);
            ",
        )
    }

    // ── Users ──────────────────────────────────────────

    pub fn create_admin_if_not_exists(&self, username: &str, password: &str, root_path: &str) -> Result<(), String> {
        let count: i64 = self
            .conn
            .lock()
            .unwrap()
            .query_row("SELECT COUNT(*) FROM users WHERE is_admin = 1", [], |r| r.get(0))
            .unwrap_or(0);
        if count > 0 {
            return Ok(());
        }
        let hash = crate::auth::hash_password(password).map_err(|e| e.to_string())?;
        self.exec(
            "INSERT INTO users (username, password_hash, is_admin, root_path, created_at) VALUES (?,?,1,?,?)",
            params![username, hash, root_path, fmt_db_time(&Local::now())],
        )
        .map_err(|e| e.to_string())?;
        Ok(())
    }

    fn scan_user(r: &rusqlite::Row) -> rusqlite::Result<User> {
        Ok(User {
            id: r.get(0)?,
            username: r.get(1)?,
            password_hash: r.get(2)?,
            is_admin: bb(r.get::<_, i64>(3)?),
            root_path: r.get(4)?,
            created_at: req_time(r.get::<_, String>(5)?),
        })
    }

    pub fn get_user_by_username(&self, username: &str) -> Option<User> {
        self.conn
            .lock()
            .unwrap()
            .query_row(
                "SELECT id, username, password_hash, is_admin, root_path, created_at FROM users WHERE username = ?",
                params![username],
                Self::scan_user,
            )
            .ok()
    }

    pub fn get_user_by_id(&self, id: i64) -> Option<User> {
        self.conn
            .lock()
            .unwrap()
            .query_row(
                "SELECT id, username, password_hash, is_admin, root_path, created_at FROM users WHERE id = ?",
                params![id],
                Self::scan_user,
            )
            .ok()
    }

    pub fn list_users_with_quota(&self) -> Vec<UserWithQuota> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn
            .prepare_cached("SELECT id, username, is_admin, root_path, quota_bytes, allowed_types, created_at FROM users")
            .unwrap();
        let rows = stmt
            .query_map([], |r| {
                Ok(UserWithQuota {
                    id: r.get(0)?,
                    username: r.get(1)?,
                    is_admin: bb(r.get::<_, i64>(2)?),
                    root_path: r.get(3)?,
                    quota_bytes: r.get(4)?,
                    allowed_types: r.get(5)?,
                    created_at: req_time(r.get::<_, String>(6)?),
                    disk_usage: 0,
                })
            })
            .unwrap();
        rows.filter_map(|r| r.ok()).collect()
    }

    pub fn create_user(&self, username: &str, password: &str, is_admin: bool, root_path: &str) -> Result<(), String> {
        let hash = crate::auth::hash_password(password).map_err(|e| e.to_string())?;
        self.exec(
            "INSERT INTO users (username, password_hash, is_admin, root_path, created_at) VALUES (?,?,?,?,?)",
            params![username, hash, b(is_admin), root_path, fmt_db_time(&Local::now())],
        )
        .map(|_| ())
        .map_err(|e| e.to_string())
    }

    pub fn delete_user(&self, id: i64) {
        let _ = self.exec("DELETE FROM users WHERE id = ?", params![id]);
    }

    pub fn admin_count(&self) -> i64 {
        self.conn
            .lock()
            .unwrap()
            .query_row("SELECT COUNT(*) FROM users WHERE is_admin = 1", [], |r| r.get(0))
            .unwrap_or(0)
    }

    pub fn user_is_admin(&self, id: i64) -> bool {
        self.conn
            .lock()
            .unwrap()
            .query_row("SELECT is_admin FROM users WHERE id = ?", params![id], |r| r.get::<_, i64>(0))
            .map(bb)
            .unwrap_or(false)
    }

    pub fn update_user_password(&self, id: i64, hash: &str) {
        let _ = self.exec("UPDATE users SET password_hash = ? WHERE id = ?", params![hash, id]);
    }

    pub fn update_user_root_path(&self, id: i64, root_path: &str) {
        let _ = self.exec("UPDATE users SET root_path = ? WHERE id = ?", params![root_path, id]);
    }

    pub fn get_user_quota_and_types(&self, id: i64) -> (i64, String) {
        self.conn
            .lock()
            .unwrap()
            .query_row("SELECT quota_bytes, allowed_types FROM users WHERE id = ?", params![id], |r| {
                Ok((r.get::<_, i64>(0)?, r.get::<_, String>(1)?))
            })
            .unwrap_or((0, String::new()))
    }

    pub fn set_user_quota(&self, id: i64, quota: i64) {
        let _ = self.exec("UPDATE users SET quota_bytes = ? WHERE id = ?", params![quota, id]);
    }

    pub fn set_user_allowed_types(&self, id: i64, types: &str) {
        let _ = self.exec("UPDATE users SET allowed_types = ? WHERE id = ?", params![types, id]);
    }

    // ── Refresh tokens ─────────────────────────────────

    pub fn save_refresh_token(&self, token: &str, user_id: i64, expires: DateTime<Local>) {
        let _ = self.exec(
            "INSERT INTO refresh_tokens (token, user_id, expires_at) VALUES (?,?,?)",
            params![token, user_id, fmt_db_time(&expires)],
        );
    }

    pub fn get_refresh_token(&self, token: &str) -> Option<i64> {
        let conn = self.conn.lock().unwrap();
        let res: Result<(i64, String), _> = conn.query_row(
            "SELECT user_id, expires_at FROM refresh_tokens WHERE token = ?",
            params![token],
            |r| Ok((r.get(0)?, r.get(1)?)),
        );
        match res {
            Ok((uid, exp)) => {
                if let Some(e) = parse_db_time(&exp) {
                    if Local::now() > e {
                        drop(conn);
                        let _ = self.exec("DELETE FROM refresh_tokens WHERE token = ?", params![token]);
                        return None;
                    }
                }
                Some(uid)
            }
            Err(_) => None,
        }
    }

    pub fn delete_refresh_token(&self, token: &str) {
        let _ = self.exec("DELETE FROM refresh_tokens WHERE token = ?", params![token]);
    }

    // ── Audit ──────────────────────────────────────────

    pub fn log_action(&self, user_id: i64, username: &str, action: &str, path: &str, ip: &str) {
        let _ = self.exec(
            "INSERT INTO audit_log (user_id, username, action, path, ip, created_at) VALUES (?,?,?,?,?,?)",
            params![user_id, username, action, path, ip, fmt_db_time(&Local::now())],
        );
    }

    pub fn get_audit_log(&self, limit: i64) -> Vec<AuditEntry> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn
            .prepare_cached("SELECT id, user_id, username, action, path, ip, created_at FROM audit_log ORDER BY created_at DESC LIMIT ?")
            .unwrap();
        let rows = stmt
            .query_map(params![limit], |r| {
                Ok(AuditEntry {
                    id: r.get(0)?,
                    user_id: r.get(1)?,
                    username: r.get(2)?,
                    action: r.get(3)?,
                    path: r.get(4)?,
                    ip: r.get(5)?,
                    created_at: req_time(r.get::<_, String>(6)?),
                })
            })
            .unwrap();
        rows.filter_map(|r| r.ok()).collect()
    }

    // ── Shares ─────────────────────────────────────────

    pub fn create_share_link(
        &self,
        path: &str,
        is_dir: bool,
        created_by: i64,
        expires_at: &Option<DateTime<Local>>,
        password_hash: &str,
        download_limit: i64,
        allow_upload: bool,
        allow_edit: bool,
    ) -> Result<String, rusqlite::Error> {
        let mut raw = [0u8; 16];
        rand::rng().fill_bytes( &mut raw);
        let token = hex::encode(raw);
        let exp = expires_at.as_ref().map(fmt_db_time);
        self.exec(
            "INSERT INTO share_links (token, path, is_dir, created_by, expires_at, created_at, password_hash, download_limit, download_count, allow_upload, allow_edit) VALUES (?,?,?,?,?,?,?,?,0,?,?)",
            params![token, path, b(is_dir), created_by, exp, fmt_db_time(&Local::now()), password_hash, download_limit, b(allow_upload), b(allow_edit)],
        )?;
        Ok(token)
    }

    fn scan_share(r: &rusqlite::Row) -> rusqlite::Result<ShareLink> {
        Ok(ShareLink {
            token: r.get(0)?,
            path: r.get(1)?,
            is_dir: bb(r.get::<_, i64>(2)?),
            created_by: r.get(3)?,
            expires_at: opt_time(r.get::<_, Option<String>>(4)?),
            created_at: req_time(r.get::<_, String>(5)?),
            password_hash: r.get(6)?,
            has_password: false,
            download_limit: r.get(7)?,
            download_count: r.get(8)?,
            allow_upload: bb(r.get::<_, i64>(9)?),
            allow_edit: bb(r.get::<_, i64>(10)?),
        })
    }

    pub fn get_share_link(&self, token: &str) -> Option<ShareLink> {
        let s: ShareLink = self
            .conn
            .lock()
            .unwrap()
            .query_row(
                "SELECT token, path, is_dir, created_by, expires_at, created_at, password_hash, download_limit, download_count, allow_upload, allow_edit FROM share_links WHERE token = ?",
                params![token],
                Self::scan_share,
            )
            .ok()?;
        if let Some(e) = s.expires_at {
            if Local::now() > e {
                let _ = self.exec("DELETE FROM share_links WHERE token = ?", params![token]);
                return None;
            }
        }
        if s.download_limit > 0 && s.download_count >= s.download_limit {
            return None;
        }
        Some(s)
    }

    pub fn consume_share_download(&self, token: &str) -> bool {
        match self.exec(
            "UPDATE share_links SET download_count = download_count + 1 WHERE token = ? AND (download_limit = 0 OR download_count < download_limit)",
            params![token],
        ) {
            Ok(n) => n == 1,
            Err(_) => false,
        }
    }

    pub fn list_share_links(&self, user_id: i64) -> Vec<ShareLink> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn
            .prepare_cached(
                "SELECT token, path, is_dir, created_by, expires_at, created_at, password_hash, download_limit, download_count, allow_upload, allow_edit FROM share_links WHERE created_by = ? ORDER BY created_at DESC",
            )
            .unwrap();
        let rows = stmt.query_map(params![user_id], Self::scan_share).unwrap();
        rows.filter_map(|r| r.ok()).collect()
    }

    pub fn delete_share_link(&self, token: &str, user_id: i64) {
        let _ = self.exec("DELETE FROM share_links WHERE token = ? AND created_by = ?", params![token, user_id]);
    }

    // ── API tokens ─────────────────────────────────────

    pub fn create_api_token(
        &self,
        user_id: i64,
        name: &str,
        expires_at: &Option<DateTime<Local>>,
    ) -> Result<(String, ApiToken), rusqlite::Error> {
        let mut raw = [0u8; 32];
        rand::rng().fill_bytes( &mut raw);
        let plain = format!("fsk_{}", hex::encode(raw));
        let prefix = format!("{}…", &plain[..12]);
        let hash = sha256_hex(&plain);
        let exp = expires_at.as_ref().map(fmt_db_time);
        let now = Local::now();
        let id = self.exec(
            "INSERT INTO api_tokens (user_id, name, token_hash, token_prefix, expires_at, created_at) VALUES (?,?,?,?,?,?)",
            params![user_id, name, hash, prefix, exp, fmt_db_time(&now)],
        )?;
        // last_insert_rowid via query (exec returns rows affected = 1)
        let _ = id;
        let conn = self.conn.lock().unwrap();
        let new_id: i64 = conn.last_insert_rowid();
        Ok((
            plain.clone(),
            ApiToken {
                id: new_id,
                user_id,
                name: name.to_string(),
                prefix,
                last_used: None,
                expires_at: *expires_at,
                created_at: now,
                token: plain,
            },
        ))
    }

    /// Ok((uid, admin)) | Err("expired") | Err("notfound")
    pub fn validate_api_token(&self, plain: &str) -> Result<(i64, bool), String> {
        let hash = sha256_hex(plain);
        let conn = self.conn.lock().unwrap();
        let res: Result<(i64, bool, Option<String>), _> = conn.query_row(
            "SELECT at.user_id, u.is_admin, at.expires_at FROM api_tokens at JOIN users u ON u.id = at.user_id WHERE at.token_hash = ?",
            params![hash],
            |r| Ok((r.get(0)?, bb(r.get::<_, i64>(1)?), r.get(2)?)),
        );
        match res {
            Ok((uid, admin, exp)) => {
                if let Some(e) = opt_time(exp) {
                    if Local::now() > e {
                        drop(conn);
                        let _ = self.exec("DELETE FROM api_tokens WHERE token_hash = ?", params![hash]);
                        return Err("expired".into());
                    }
                }
                drop(conn);
                let _ = self.exec(
                    "UPDATE api_tokens SET last_used = ? WHERE token_hash = ?",
                    params![fmt_db_time(&Local::now()), hash],
                );
                Ok((uid, admin))
            }
            Err(_) => Err("notfound".into()),
        }
    }

    pub fn list_api_tokens(&self, user_id: i64) -> Vec<ApiToken> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn
            .prepare_cached("SELECT id, user_id, name, token_prefix, last_used, expires_at, created_at FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC")
            .unwrap();
        let rows = stmt
            .query_map(params![user_id], |r| {
                Ok(ApiToken {
                    id: r.get(0)?,
                    user_id: r.get(1)?,
                    name: r.get(2)?,
                    prefix: r.get(3)?,
                    last_used: opt_time(r.get::<_, Option<String>>(4)?),
                    expires_at: opt_time(r.get::<_, Option<String>>(5)?),
                    created_at: req_time(r.get::<_, String>(6)?),
                    token: String::new(),
                })
            })
            .unwrap();
        rows.filter_map(|r| r.ok()).collect()
    }

    pub fn delete_api_token(&self, id: i64, user_id: i64) {
        let _ = self.exec("DELETE FROM api_tokens WHERE id = ? AND user_id = ?", params![id, user_id]);
    }

    // ── Favorites ──────────────────────────────────────

    pub fn add_favorite(&self, user_id: i64, path: &str, name: &str, is_dir: bool) {
        let _ = self.exec(
            "INSERT OR IGNORE INTO favorites (user_id, path, name, is_dir, created_at) VALUES (?,?,?,?,?)",
            params![user_id, path, name, b(is_dir), fmt_db_time(&Local::now())],
        );
    }

    pub fn remove_favorite(&self, user_id: i64, path: &str) {
        let _ = self.exec("DELETE FROM favorites WHERE user_id = ? AND path = ?", params![user_id, path]);
    }

    pub fn list_favorites(&self, user_id: i64) -> Vec<Favorite> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn
            .prepare_cached("SELECT id, user_id, path, name, is_dir, created_at FROM favorites WHERE user_id = ? ORDER BY created_at DESC")
            .unwrap();
        let rows = stmt
            .query_map(params![user_id], |r| {
                Ok(Favorite {
                    id: r.get(0)?,
                    user_id: r.get(1)?,
                    path: r.get(2)?,
                    name: r.get(3)?,
                    is_dir: bb(r.get::<_, i64>(4)?),
                    created_at: req_time(r.get::<_, String>(5)?),
                })
            })
            .unwrap();
        rows.filter_map(|r| r.ok()).collect()
    }

    pub fn favorite_path_set(&self, user_id: i64) -> HashSet<String> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare_cached("SELECT path FROM favorites WHERE user_id = ?").unwrap();
        let rows = stmt.query_map(params![user_id], |r| r.get::<_, String>(0)).unwrap();
        rows.filter_map(|r| r.ok()).collect()
    }

    // ── Permissions ────────────────────────────────────

    pub fn set_path_permission(&self, p: &PathPermission) {
        let _ = self.exec(
            "INSERT INTO path_permissions (user_id, path, can_read, can_write, can_delete, can_share) VALUES (?,?,?,?,?,?) ON CONFLICT(user_id, path) DO UPDATE SET can_read=excluded.can_read, can_write=excluded.can_write, can_delete=excluded.can_delete, can_share=excluded.can_share",
            params![p.user_id, p.path, b(p.can_read), b(p.can_write), b(p.can_delete), b(p.can_share)],
        );
    }

    pub fn delete_path_permission(&self, id: i64) {
        let _ = self.exec("DELETE FROM path_permissions WHERE id = ?", params![id]);
    }

    pub fn list_path_permissions(&self, user_id: i64) -> Vec<PathPermission> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn
            .prepare_cached("SELECT id, user_id, path, can_read, can_write, can_delete, can_share FROM path_permissions WHERE user_id = ? ORDER BY path ASC")
            .unwrap();
        let rows = stmt
            .query_map(params![user_id], |r| {
                Ok(PathPermission {
                    id: r.get(0)?,
                    user_id: r.get(1)?,
                    path: r.get(2)?,
                    can_read: bb(r.get::<_, i64>(3)?),
                    can_write: bb(r.get::<_, i64>(4)?),
                    can_delete: bb(r.get::<_, i64>(5)?),
                    can_share: bb(r.get::<_, i64>(6)?),
                })
            })
            .unwrap();
        rows.filter_map(|r| r.ok()).collect()
    }

    pub fn check_permission(&self, user_id: i64, path: &str, action: &str) -> bool {
        let perms = self.list_path_permissions(user_id);
        path_permission_allowed(&perms, path, action)
    }

    // ── Trash ──────────────────────────────────────────

    pub fn trash_put(&self, user_id: i64, orig_path: &str, trash_name: &str, name: &str, is_dir: bool, size: i64, retention_days: i64) {
        let now = Local::now();
        let expires = now + chrono::Duration::days(retention_days);
        let _ = self.exec(
            "INSERT INTO trash (user_id, orig_path, trash_name, name, is_dir, size, deleted_at, expires_at) VALUES (?,?,?,?,?,?,?,?)",
            params![user_id, orig_path, trash_name, name, b(is_dir), size, fmt_db_time(&now), fmt_db_time(&expires)],
        );
    }

    pub fn trash_list(&self, user_id: i64) -> Vec<TrashItem> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn
            .prepare_cached("SELECT id, user_id, orig_path, trash_name, name, is_dir, size, deleted_at, expires_at FROM trash WHERE user_id = ? ORDER BY deleted_at DESC")
            .unwrap();
        let rows = stmt.query_map(params![user_id], Self::scan_trash).unwrap();
        rows.filter_map(|r| r.ok()).collect()
    }

    fn scan_trash(r: &rusqlite::Row) -> rusqlite::Result<TrashItem> {
        Ok(TrashItem {
            id: r.get(0)?,
            user_id: r.get(1)?,
            orig_path: r.get(2)?,
            trash_name: r.get(3)?,
            name: r.get(4)?,
            is_dir: bb(r.get::<_, i64>(5)?),
            size: r.get(6)?,
            deleted_at: req_time(r.get::<_, String>(7)?),
            expires_at: req_time(r.get::<_, String>(8)?),
        })
    }

    pub fn trash_get(&self, user_id: i64, trash_name: &str) -> Option<TrashItem> {
        self.conn
            .lock()
            .unwrap()
            .query_row(
                "SELECT id, user_id, orig_path, trash_name, name, is_dir, size, deleted_at, expires_at FROM trash WHERE user_id = ? AND trash_name = ?",
                params![user_id, trash_name],
                Self::scan_trash,
            )
            .ok()
    }

    pub fn trash_delete(&self, user_id: i64, trash_name: &str) {
        let _ = self.exec("DELETE FROM trash WHERE user_id = ? AND trash_name = ?", params![user_id, trash_name]);
    }

    pub fn trash_purge_expired(&self, user_id: i64) -> Vec<String> {
        let now = fmt_db_time(&Local::now());
        // Go vergleicht expires_at < time.Now() — Go speichert mit Offset; Stringvergleich
        // funktioniert hier nicht zuverlässig, daher laden + filtern.
        let items = self.trash_list(user_id);
        let expired: Vec<String> = items
            .into_iter()
            .filter(|t| t.expires_at < Local::now())
            .map(|t| t.trash_name)
            .collect();
        if !expired.is_empty() {
            let _ = self.exec(
                "DELETE FROM trash WHERE user_id = ? AND expires_at < ?",
                params![user_id, now],
            );
            // Falls Stringvergleich nichts traf (Format-Differenzen), einzeln löschen:
            for n in &expired {
                let _ = self.exec("DELETE FROM trash WHERE user_id = ? AND trash_name = ?", params![user_id, n]);
            }
        }
        expired
    }

    // ── Sources ────────────────────────────────────────

    pub fn list_sources(&self, user_id: i64) -> Vec<UserSource> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn
            .prepare_cached("SELECT id, user_id, name, root_path, is_default, sort_order FROM user_sources WHERE user_id = ? ORDER BY sort_order ASC, name ASC")
            .unwrap();
        let rows = stmt.query_map(params![user_id], Self::scan_source).unwrap();
        rows.filter_map(|r| r.ok()).collect()
    }

    fn scan_source(r: &rusqlite::Row) -> rusqlite::Result<UserSource> {
        Ok(UserSource {
            id: r.get(0)?,
            user_id: r.get(1)?,
            name: r.get(2)?,
            root_path: r.get(3)?,
            is_default: bb(r.get::<_, i64>(4)?),
            sort_order: r.get(5)?,
        })
    }

    pub fn get_source_by_id(&self, user_id: i64, source_id: i64) -> Option<UserSource> {
        self.conn
            .lock()
            .unwrap()
            .query_row(
                "SELECT id, user_id, name, root_path, is_default, sort_order FROM user_sources WHERE user_id = ? AND id = ?",
                params![user_id, source_id],
                Self::scan_source,
            )
            .ok()
    }

    pub fn create_source(&self, user_id: i64, name: &str, root_path: &str, is_default: bool) -> Result<UserSource, rusqlite::Error> {
        if is_default {
            let _ = self.exec("UPDATE user_sources SET is_default = 0 WHERE user_id = ?", params![user_id]);
        }
        self.exec(
            "INSERT INTO user_sources (user_id, name, root_path, is_default, sort_order) VALUES (?,?,?,?, (SELECT COALESCE(MAX(sort_order),0)+1 FROM user_sources WHERE user_id = ?))",
            params![user_id, name, root_path, b(is_default), user_id],
        )?;
        let id = self.conn.lock().unwrap().last_insert_rowid();
        Ok(UserSource { id, user_id, name: name.to_string(), root_path: root_path.to_string(), is_default, sort_order: 0 })
    }

    pub fn update_source(&self, user_id: i64, source_id: i64, name: &str, root_path: &str, is_default: bool) {
        if is_default {
            let _ = self.exec("UPDATE user_sources SET is_default = 0 WHERE user_id = ?", params![user_id]);
        }
        let _ = self.exec(
            "UPDATE user_sources SET name=?, root_path=?, is_default=? WHERE user_id=? AND id=?",
            params![name, root_path, b(is_default), user_id, source_id],
        );
    }

    pub fn delete_source(&self, user_id: i64, source_id: i64) {
        let _ = self.exec("DELETE FROM user_sources WHERE user_id = ? AND id = ?", params![user_id, source_id]);
    }

    // ── Agents ─────────────────────────────────────────

    pub fn create_agent_device(&self, user_id: i64, name: &str, root_path: &str) -> Result<AgentDevice, rusqlite::Error> {
        let root_path = if root_path.is_empty() { "/" } else { root_path };
        let mut raw = [0u8; 24];
        rand::rng().fill_bytes( &mut raw);
        let plain = format!("fsa_{}", hex::encode(raw));
        let prefix = plain[..12].to_string();
        let hash = sha256_hex(&plain);
        let now = Local::now();
        self.exec(
            "INSERT INTO agent_devices (user_id, name, token_hash, token_prefix, root_path, created_at) VALUES (?,?,?,?,?,?)",
            params![user_id, name, hash, prefix, root_path, fmt_db_time(&now)],
        )?;
        let id = self.conn.lock().unwrap().last_insert_rowid();
        Ok(AgentDevice {
            id,
            user_id,
            name: name.to_string(),
            prefix,
            root_path: root_path.to_string(),
            hostname: String::new(),
            os_info: String::new(),
            disk_usage: 0,
            online: false,
            last_seen: None,
            created_at: now,
            token: plain,
        })
    }

    pub fn list_agent_devices(&self, user_id: i64) -> Vec<AgentDevice> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn
            .prepare_cached(
                "SELECT id, user_id, name, token_prefix, root_path, hostname, os_info, disk_usage, last_seen, created_at FROM agent_devices WHERE user_id = ? ORDER BY created_at DESC",
            )
            .unwrap();
        let rows = stmt.query_map(params![user_id], Self::scan_agent).unwrap();
        rows.filter_map(|r| r.ok()).collect()
    }

    fn scan_agent(r: &rusqlite::Row) -> rusqlite::Result<AgentDevice> {
        Ok(AgentDevice {
            id: r.get(0)?,
            user_id: r.get(1)?,
            name: r.get(2)?,
            prefix: r.get(3)?,
            root_path: r.get(4)?,
            hostname: r.get(5)?,
            os_info: r.get(6)?,
            disk_usage: r.get(7)?,
            online: false,
            last_seen: opt_time(r.get::<_, Option<String>>(8)?),
            created_at: req_time(r.get::<_, String>(9)?),
            token: String::new(),
        })
    }

    pub fn get_agent_device_by_id(&self, user_id: i64, agent_id: i64) -> Option<AgentDevice> {
        self.conn
            .lock()
            .unwrap()
            .query_row(
                "SELECT id, user_id, name, token_prefix, root_path, hostname, os_info, disk_usage, last_seen, created_at FROM agent_devices WHERE user_id = ? AND id = ?",
                params![user_id, agent_id],
                Self::scan_agent,
            )
            .ok()
    }

    pub fn delete_agent_device(&self, user_id: i64, agent_id: i64) {
        let _ = self.exec("DELETE FROM agent_devices WHERE user_id = ? AND id = ?", params![user_id, agent_id]);
    }

    pub fn validate_agent_token(&self, raw: &str) -> Option<AgentDevice> {
        let hash = sha256_hex(raw);
        let mut dev: AgentDevice = self
            .conn
            .lock()
            .unwrap()
            .query_row(
                "SELECT id, user_id, name, token_prefix, root_path, hostname, os_info, disk_usage, last_seen, created_at FROM agent_devices WHERE token_hash = ?",
                params![hash],
                Self::scan_agent,
            )
            .ok()?;
        let now = Local::now();
        dev.last_seen = Some(now);
        let _ = self.exec("UPDATE agent_devices SET last_seen = ? WHERE id = ?", params![fmt_db_time(&now), dev.id]);
        Some(dev)
    }

    pub fn update_agent_heartbeat(&self, agent_id: i64, hostname: &str, os_info: &str, root_path: &str, disk_usage: i64) {
        let _ = self.exec(
            "UPDATE agent_devices SET hostname = ?, os_info = ?, root_path = ?, disk_usage = ?, last_seen = ? WHERE id = ?",
            params![hostname, os_info, root_path, disk_usage, fmt_db_time(&Local::now()), agent_id],
        );
    }

    // ── TOTP ───────────────────────────────────────────

    pub fn totp_get_secret(&self, user_id: i64) -> Option<(String, bool)> {
        self.conn
            .lock()
            .unwrap()
            .query_row("SELECT secret, enabled FROM totp_secrets WHERE user_id = ?", params![user_id], |r| {
                Ok((r.get::<_, String>(0)?, bb(r.get::<_, i64>(1)?)))
            })
            .ok()
    }

    pub fn totp_set_secret(&self, user_id: i64, secret: &str) {
        let _ = self.exec(
            "INSERT INTO totp_secrets (user_id, secret, enabled, backup_codes, created_at) VALUES (?,?,0,'',?) ON CONFLICT(user_id) DO UPDATE SET secret=excluded.secret, enabled=0, backup_codes=''",
            params![user_id, secret, fmt_db_time(&Local::now())],
        );
    }

    pub fn totp_enable(&self, user_id: i64, backup_codes: &[String]) {
        let _ = self.exec(
            "UPDATE totp_secrets SET enabled=1, backup_codes=? WHERE user_id=?",
            params![backup_codes.join(","), user_id],
        );
    }

    pub fn totp_disable(&self, user_id: i64) {
        let _ = self.exec("DELETE FROM totp_secrets WHERE user_id = ?", params![user_id]);
    }

    pub fn totp_use_backup_code(&self, user_id: i64, code: &str) -> bool {
        let stored: String = match self
            .conn
            .lock()
            .unwrap()
            .query_row("SELECT backup_codes FROM totp_secrets WHERE user_id=? AND enabled=1", params![user_id], |r| r.get(0))
        {
            Ok(s) => s,
            Err(_) => return false,
        };
        if stored.is_empty() {
            return false;
        }
        let mut found = false;
        let remaining: Vec<String> = stored
            .split(',')
            .filter(|c| {
                if c.trim() == code.trim() {
                    found = true;
                    false
                } else {
                    true
                }
            })
            .map(|s| s.to_string())
            .collect();
        if found {
            let _ = self.exec(
                "UPDATE totp_secrets SET backup_codes=? WHERE user_id=?",
                params![remaining.join(","), user_id],
            );
        }
        found
    }

    pub fn totp_is_enabled(&self, user_id: i64) -> bool {
        self.conn
            .lock()
            .unwrap()
            .query_row("SELECT enabled FROM totp_secrets WHERE user_id = ?", params![user_id], |r| r.get::<_, i64>(0))
            .map(bb)
            .unwrap_or(false)
    }

    // ── Index ──────────────────────────────────────────

    pub fn index_search(&self, user_id: i64, query: &str, limit: i64) -> Vec<FileInfo> {
        let limit = if limit <= 0 { 200 } else { limit };
        let pattern = format!("%{}%", query.to_lowercase());
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn
            .prepare_cached(
                "SELECT rel_path, name, is_dir, size, mod_time, mime_type FROM file_index WHERE user_id = ? AND LOWER(name) LIKE ? ORDER BY is_dir DESC, name ASC LIMIT ?",
            )
            .unwrap();
        let rows = stmt
            .query_map(params![user_id, pattern, limit], |r| {
                Ok(FileInfo {
                    path: r.get(0)?,
                    name: r.get(1)?,
                    is_dir: bb(r.get::<_, i64>(2)?),
                    size: r.get(3)?,
                    mod_time: req_time(r.get::<_, String>(4)?),
                    mime_type: r.get(5)?,
                    dir_size: 0,
                    starred: false,
                })
            })
            .unwrap();
        rows.filter_map(|r| r.ok()).collect()
    }

    pub fn index_upsert(&self, user_id: i64, root_path: &str, files: &[FileInfo]) {
        let mut conn = self.conn.lock().unwrap();
        let tx = conn.transaction().unwrap();
        {
            let mut stmt = tx
                .prepare_cached(
                    "INSERT INTO file_index (user_id, root_path, rel_path, name, is_dir, size, mod_time, mime_type, indexed_at) VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT(user_id, rel_path) DO UPDATE SET name=excluded.name, is_dir=excluded.is_dir, size=excluded.size, mod_time=excluded.mod_time, mime_type=excluded.mime_type, indexed_at=excluded.indexed_at",
                )
                .unwrap();
            let now_str = fmt_db_time(&Local::now());
            for f in files {
                let _ = stmt.execute(params![
                    user_id,
                    root_path,
                    f.path,
                    f.name,
                    b(f.is_dir),
                    f.size,
                    fmt_db_time(&f.mod_time),
                    f.mime_type,
                    now_str
                ]);
            }
        }
        let _ = tx.commit();
    }

    pub fn index_delete(&self, user_id: i64, rel_path: &str) {
        let _ = self.exec(
            "DELETE FROM file_index WHERE user_id = ? AND (rel_path = ? OR rel_path LIKE ?)",
            params![user_id, rel_path, format!("{}/%", rel_path)],
        );
    }

    pub fn index_rename(&self, user_id: i64, old_rel: &str, new_rel: &str) {
        let _ = self.exec(
            "UPDATE file_index SET rel_path = ? || substr(rel_path, length(?) + 1) WHERE user_id = ? AND rel_path LIKE ?",
            params![new_rel, old_rel, user_id, format!("{}/%", old_rel)],
        );
        let base = new_rel.rsplit('/').next().unwrap_or(new_rel);
        let _ = self.exec(
            "UPDATE file_index SET rel_path = ?, name = ? WHERE user_id = ? AND rel_path = ?",
            params![new_rel, base, user_id, old_rel],
        );
    }

    pub fn index_count_for_user(&self, user_id: i64) -> i64 {
        self.conn
            .lock()
            .unwrap()
            .query_row("SELECT COUNT(*) FROM file_index WHERE user_id = ?", params![user_id], |r| r.get(0))
            .unwrap_or(0)
    }

    pub fn dir_size_cache_get(&self, root_path: &str, max_age: chrono::Duration) -> Option<i64> {
        let res: Result<(i64, String), _> = self
            .conn
            .lock()
            .unwrap()
            .query_row("SELECT size_bytes, updated_at FROM dir_size_cache WHERE root_path = ?", params![root_path], |r| {
                Ok((r.get(0)?, r.get(1)?))
            });
        match res {
            Ok((size, upd)) => {
                let t = parse_db_time(&upd)?;
                if Local::now() - t > max_age {
                    None
                } else {
                    Some(size)
                }
            }
            Err(_) => None,
        }
    }

    pub fn dir_size_cache_set(&self, root_path: &str, size: i64) {
        let _ = self.exec(
            "INSERT INTO dir_size_cache (root_path, size_bytes, updated_at) VALUES (?,?,?) ON CONFLICT(root_path) DO UPDATE SET size_bytes=excluded.size_bytes, updated_at=excluded.updated_at",
            params![root_path, size, fmt_db_time(&Local::now())],
        );
    }

    pub fn index_prune_old(&self, user_id: i64, root_abs: &str, walk_start: &str) {
        let _ = self.exec(
            "DELETE FROM file_index WHERE user_id = ? AND root_path = ? AND indexed_at < ?",
            params![user_id, root_abs, walk_start],
        );
    }
}

pub fn sha256_hex(s: &str) -> String {
    use sha2::{Digest, Sha256};
    let mut h = Sha256::new();
    h.update(s.as_bytes());
    hex::encode(h.finalize())
}

pub fn path_permission_allowed(perms: &[PathPermission], rel_path: &str, action: &str) -> bool {
    if perms.is_empty() {
        return true;
    }
    let rel = clean_rule_path(rel_path);
    let mut best: Option<&PathPermission> = None;
    let mut best_len: i64 = -1;
    for p in perms {
        let rule = clean_rule_path(&p.path);
        if rule.is_empty() || rel == rule || rel.starts_with(&format!("{}/", rule)) {
            if rule.len() as i64 > best_len {
                best_len = rule.len() as i64;
                best = Some(p);
            }
        }
    }
    let Some(best) = best else { return true };
    match action {
        "read" => best.can_read,
        "write" => best.can_write,
        "delete" => best.can_delete,
        "share" => best.can_share,
        _ => true,
    }
}

fn clean_rule_path(p: &str) -> String {
    // Wie Go filepath.Clean + ToSlash + Trim "/" + "." -> ""
    let c = crate::fsops::go_clean(p);
    let t = c.trim_matches('/').to_string();
    if t == "." {
        String::new()
    } else {
        t
    }
}

pub fn trash_dir(db_path: &str) -> std::path::PathBuf {
    let p = std::path::Path::new(db_path);
    let parent = p.parent().map(|x| x.to_path_buf()).unwrap_or_else(|| std::path::PathBuf::from("."));
    parent.join(".fileship_trash")
}
