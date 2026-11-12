//! Filesystem-Operationen — 1:1 Port von internal/fs (Go).
//! Path safety: Resolve() follows the same rules (absolute path, no symlinks, prefix check).

use rand_core::Rng;
use crate::model::{FileInfo, ListOptions, ListResult};
use std::collections::HashMap;
use std::io::{Read, Write};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Mutex;

#[derive(Debug)]
pub struct FsError(pub String);
impl std::fmt::Display for FsError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.0)
    }
}
impl std::error::Error for FsError {}
impl From<std::io::Error> for FsError {
    fn from(e: std::io::Error) -> Self {
        FsError(e.to_string())
    }
}
pub type FsResult<T> = Result<T, FsError>;

fn ferr<T>(msg: &str) -> FsResult<T> {
    Err(FsError(msg.to_string()))
}

/// Go filepath.Clean (Linux-Variante, '/'-getrennt).
pub fn go_clean(path: &str) -> String {
    if path.is_empty() {
        return ".".to_string();
    }
    let rooted = path.starts_with('/');
    let mut out: Vec<&str> = Vec::new();
    for comp in path.split('/') {
        match comp {
            "" | "." => {}
            ".." => {
                if !out.is_empty() && *out.last().unwrap() != ".." {
                    out.pop();
                } else if !rooted {
                    out.push("..");
                }
            }
            c => out.push(c),
        }
    }
    let mut s = out.join("/");
    if rooted {
        s = format!("/{}", s);
    }
    if s.is_empty() {
        if rooted {
            "/".to_string()
        } else {
            ".".to_string()
        }
    } else {
        s
    }
}

pub fn is_abs(p: &str) -> bool {
    p.starts_with('/')
}

pub fn clean_relative_path(path: &str) -> FsResult<String> {
    if path.is_empty() || is_abs(path) {
        return ferr("path outside root");
    }
    let clean = go_clean(path);
    if clean == "." || clean == ".." || clean.starts_with("../") {
        return ferr("path outside root");
    }
    Ok(clean)
}

fn join_clean(root: &str, rel: &str) -> String {
    if rel.is_empty() {
        return go_clean(root);
    }
    if root.is_empty() {
        return go_clean(rel);
    }
    go_clean(&format!("{}/{}", root.trim_end_matches('/'), rel))
}

/// Resolve returns the absolute, safe path or ErrForbidden.
pub fn resolve(root: &str, rel: &str) -> FsResult<String> {
    let root_abs = std::fs::canonicalize(root).map_err(|_| FsError("path outside root".into()))?;
    let root_str = root_abs.to_string_lossy().to_string();
    let abs = join_clean(&root_str, rel);
    let prefix = format!("{}/", root_str);
    if abs != root_str && !abs.starts_with(&prefix) {
        return ferr("path outside root");
    }
    // Symlink check: every existing component from abs up to root (excl. root)
    let mut cur = abs.clone();
    while cur != root_str {
        match std::fs::symlink_metadata(&cur) {
            Ok(md) => {
                if md.file_type().is_symlink() {
                    return ferr("path outside root");
                }
            }
            Err(_) => {}
        }
        let parent = parent_of(&cur);
        if parent == cur {
            break;
        }
        cur = parent;
    }
    Ok(abs)
}

fn parent_of(p: &str) -> String {
    match p.rfind('/') {
        Some(0) => "/".to_string(),
        Some(i) => p[..i].to_string(),
        None => ".".to_string(),
    }
}

fn file_name_of(p: &str) -> String {
    p.rsplit('/').next().unwrap_or(p).to_string()
}

fn to_slash(p: &Path) -> String {
    p.to_string_lossy().replace('\\', "/")
}

pub fn list(root: &str, rel: &str) -> FsResult<Vec<FileInfo>> {
    let abs = resolve(root, rel)?;
    let mut files = Vec::new();
    for e in std::fs::read_dir(&abs)? {
        let e = e?;
        let md = match e.metadata() {
            Ok(m) => m,
            Err(_) => continue,
        };
        let name = e.file_name().to_string_lossy().to_string();
        let mut path = if rel.is_empty() { name.clone() } else { format!("{}/{}", rel, name) };
        path = go_clean(&path);
        let is_dir = e.file_type().map(|t| t.is_dir()).unwrap_or(false);
        let mime = if !is_dir { mime_type(&name) } else { String::new() };
        files.push(FileInfo {
            name,
            path,
            size: md.len() as i64,
            is_dir,
            mod_time: md.modified().map(|t| chrono::DateTime::<chrono::Local>::from(t)).unwrap_or_else(|_| chrono::Local::now()),
            mime_type: mime,
            dir_size: 0,
            starred: false,
        });
    }
    // os.ReadDir sortiert nach Namen (Go)
    files.sort_by(|a, b| a.name.cmp(&b.name));
    Ok(files)
}

pub fn list_paged_filtered<F: Fn(&FileInfo) -> bool>(
    root: &str,
    opts: &ListOptions,
    allow: Option<&F>,
) -> FsResult<ListResult> {
    let abs = resolve(root, &opts.path)?;
    let mut files: Vec<FileInfo> = Vec::new();
    let search = opts.search.to_lowercase();
    for e in std::fs::read_dir(&abs)? {
        let e = e?;
        let name = e.file_name().to_string_lossy().to_string();
        if !search.is_empty() && !name.to_lowercase().contains(&search) {
            continue;
        }
        let md = match e.metadata() {
            Ok(m) => m,
            Err(_) => continue,
        };
        let is_dir = e.file_type().map(|t| t.is_dir()).unwrap_or(false);
        let fi = FileInfo {
            path: go_clean(&format!("{}/{}", opts.path, name)),
            name: name.clone(),
            size: md.len() as i64,
            is_dir,
            mod_time: md.modified().map(|t| chrono::DateTime::<chrono::Local>::from(t)).unwrap_or_else(|_| chrono::Local::now()),
            mime_type: if !is_dir { mime_type(&name) } else { String::new() },
            dir_size: 0,
            starred: false,
        };
        if let Some(f) = allow {
            if !f(&fi) {
                continue;
            }
        }
        files.push(fi);
    }
    sort_files(&mut files, &opts.sort_by, opts.sort_asc);
    let total = files.len() as i64;
    let per_page = if opts.per_page <= 0 { 100 } else { opts.per_page };
    let page = if opts.page <= 0 { 1 } else { opts.page };
    let page_files = if total == 0 || page > (total - 1) / per_page + 1 {
        Vec::new()
    } else {
        let start = ((page - 1) * per_page) as usize;
        let end = std::cmp::min(total as usize, start + per_page as usize);
        files[start..end].to_vec()
    };
    Ok(ListResult { files: page_files, total, page, per_page })
}

fn sort_files(files: &mut [FileInfo], by: &str, asc: bool) {
    files.sort_by(|a, b| {
        // Ordner immer zuerst
        if a.is_dir != b.is_dir {
            return if a.is_dir { std::cmp::Ordering::Less } else { std::cmp::Ordering::Greater };
        }
        let cmp = match by {
            "size" => a.size.cmp(&b.size),
            "mod_time" => a.mod_time.cmp(&b.mod_time),
            _ => a.name.to_lowercase().cmp(&b.name.to_lowercase()),
        };
        if asc { cmp } else { cmp.reverse() }
    });
}

pub fn search(root: &str, query: &str) -> FsResult<Vec<FileInfo>> {
    let abs = go_clean(root);
    let query = query.to_lowercase();
    let mut results = Vec::new();
    walk_recursive(Path::new(&abs), &abs, &mut |path, md| {
        let name = path.file_name().map(|n| n.to_string_lossy().to_string()).unwrap_or_default();
        if name.to_lowercase().contains(&query) {
            let rel = path.strip_prefix(&abs).map(|p| to_slash(p)).unwrap_or_default();
            let is_dir = md.is_dir();
            results.push(FileInfo {
                name: name.clone(),
                path: rel,
                size: md.len() as i64,
                is_dir,
                mod_time: md.modified().map(|t| chrono::DateTime::<chrono::Local>::from(t)).unwrap_or_else(|_| chrono::Local::now()),
                mime_type: if !is_dir { mime_type(&name) } else { String::new() },
                dir_size: 0,
                starred: false,
            });
        }
    })?;
    Ok(results)
}

/// Walk like filepath.Walk: errors reading a dir are skipped
/// (Go callbacks in Search never return errors; Walk does not skip unreadable entries —
/// but for Search/Indexer, skip semantics are correct).
fn walk_recursive<F: FnMut(&Path, &std::fs::Metadata)>(dir: &Path, _base: &str, f: &mut F) -> FsResult<()> {
    let entries = match std::fs::read_dir(dir) {
        Ok(e) => e,
        Err(_) => return Ok(()),
    };
    for e in entries {
        let e = match e {
            Ok(e) => e,
            Err(_) => continue,
        };
        let path = e.path();
        let md = match std::fs::symlink_metadata(&path) {
            Ok(m) => m,
            Err(_) => continue,
        };
        f(&path, &md);
        if md.is_dir() {
            walk_recursive(&path, _base, f)?;
        }
    }
    Ok(())
}

/// Go filepath.Dir
pub fn go_dir(p: &str) -> String {
    if p.is_empty() {
        return ".".to_string();
    }
    let trimmed = p.trim_end_matches('/');
    if trimmed.is_empty() {
        return "/".to_string();
    }
    match trimmed.rfind('/') {
        Some(0) => "/".to_string(),
        Some(i) => trimmed[..i].to_string(),
        None => ".".to_string(),
    }
}

pub fn dir_size(path: &str) -> FsResult<i64> {
    // Walk like filepath.Walk, but skip pseudo-filesystems via their statfs
    // fstype: /proc, /sys, /dev, cgroupfs, debugfs. Their virtual files
    // report gigantic apparent sizes (/proc/kcore alone is 128 TB), so
    // counting them makes any bind-mounted host root show hundreds of TB.
    // Real filesystems on other mounts (e.g. a second disk under /mnt) are
    // fully counted. Unreadable subtrees are skipped instead of discarding
    // the whole sum.
    let mut size: i64 = 0;
    fn rec(p: &Path, size: &mut i64) {
        let Ok(entries) = std::fs::read_dir(p) else {
            return;
        };
        for e in entries.flatten() {
            let Ok(md) = e.metadata() else {
                continue;
            };
            let ft = md.file_type();
            if ft.is_symlink() || !ft.is_file() && !ft.is_dir() {
                continue;
            }
            if ft.is_dir() {
                if is_pseudo_fs(&e.path()) {
                    continue;
                }
                rec(&e.path(), size);
            } else {
                *size += md.len() as i64;
            }
        }
    }
    rec(Path::new(path), &mut size);
    Ok(size)
}

/// True for pseudo-filesystems whose files have virtual (non-disk) sizes.
pub fn is_pseudo_fs(path: &Path) -> bool {
    use std::ffi::CString;
    let Ok(cpath) = CString::new(path.to_string_lossy().into_owned()) else {
        return false;
    };
    unsafe {
        let mut st: libc::statfs = std::mem::zeroed();
        if libc::statfs(cpath.as_ptr(), &mut st) != 0 {
            return false; // cannot tell — count it rather than skip it
        }
        const PROC: i64 = 0x9fa0;
        const SYS: i64 = 0x6265_6572;
        const DEVTMPFS: i64 = 0x7371_7374;
        const CGROUP: i64 = 0x27e0eb;
        const CGROUP2: i64 = 0x6367_7270;
        const DEBUGFS: i64 = 0x6462_6773;
        matches!(st.f_type as i64, PROC | SYS | DEVTMPFS | CGROUP | CGROUP2 | DEBUGFS)
    }
}

pub fn mkdir(root: &str, rel: &str) -> FsResult<()> {
    let abs = resolve(root, rel)?;
    std::fs::create_dir_all(&abs)?;
    Ok(())
}

pub fn create_file(root: &str, rel: &str) -> FsResult<()> {
    let abs = resolve(root, rel)?;
    if let Some(parent) = Path::new(&abs).parent() {
        std::fs::create_dir_all(parent)?;
    }
    std::fs::OpenOptions::new().write(true).create_new(true).open(&abs)?;
    Ok(())
}

pub fn delete(root: &str, rel: &str) -> FsResult<()> {
    clean_relative_path(rel)?;
    let abs = resolve(root, rel)?;
    let p = Path::new(&abs);
    if p.is_dir() && !p.is_symlink() {
        std::fs::remove_dir_all(p)?;
    } else {
        std::fs::remove_file(p).or_else(|_| std::fs::remove_dir_all(p))?;
    }
    Ok(())
}

pub fn rename(root: &str, old_rel: &str, new_rel: &str) -> FsResult<()> {
    clean_relative_path(old_rel)?;
    clean_relative_path(new_rel)?;
    let old_abs = resolve(root, old_rel)?;
    let new_abs = resolve(root, new_rel)?;
    if old_abs == new_abs {
        return Ok(());
    }
    match std::fs::symlink_metadata(&new_abs) {
        Ok(_) => return ferr("file already exists"),
        Err(e) if e.kind() != std::io::ErrorKind::NotFound => return Err(e.into()),
        _ => {}
    }
    std::fs::rename(&old_abs, &new_abs)?;
    Ok(())
}

static TMP_COUNTER: AtomicU64 = AtomicU64::new(0);

pub fn save_upload<R: Read>(root: &str, rel: &str, reader: &mut R) -> FsResult<()> {
    let abs = resolve(root, rel)?;
    let parent = Path::new(&abs).parent().map(|p| p.to_path_buf()).unwrap_or_else(|| PathBuf::from("."));
    std::fs::create_dir_all(&parent)?;
    let n = TMP_COUNTER.fetch_add(1, Ordering::Relaxed);
    let tmp_name = format!(".fileship-upload-{}-{}-{}", std::process::id(), n, rand_hex(4));
    let tmp_path = parent.join(&tmp_name);
    let result = (|| -> FsResult<()> {
        let mut f = std::fs::OpenOptions::new().write(true).create_new(true).open(&tmp_path)?;
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            std::fs::set_permissions(&tmp_path, std::fs::Permissions::from_mode(0o644))?;
        }
        let mut buf = [0u8; 64 * 1024];
        loop {
            let n = reader.read(&mut buf)?;
            if n == 0 {
                break;
            }
            f.write_all(&buf[..n])?;
        }
        f.sync_all()?;
        drop(f);
        std::fs::rename(&tmp_path, &abs)?;
        Ok(())
    })();
    if result.is_err() {
        let _ = std::fs::remove_file(&tmp_path);
    }
    result
}

fn rand_hex(bytes: usize) -> String {
    let mut b = vec![0u8; bytes];
    rand::rng().fill_bytes( &mut b);
    hex::encode(b)
}

pub fn copy(root: &str, src_rel: &str, dst_rel: &str) -> FsResult<()> {
    clean_relative_path(src_rel)?;
    clean_relative_path(dst_rel)?;
    let src_abs = resolve(root, src_rel)?;
    let dst_abs = resolve(root, dst_rel)?;
    if src_abs == dst_abs || dst_abs.starts_with(&format!("{}/", src_abs)) {
        return ferr("destination is inside source");
    }
    match std::fs::symlink_metadata(&dst_abs) {
        Ok(_) => return ferr("file already exists"),
        Err(e) if e.kind() != std::io::ErrorKind::NotFound => return Err(e.into()),
        _ => {}
    }
    // Preflight: kein Symlink / keine Specials im Baum
    let mut bad: Option<String> = None;
    walk_all(Path::new(&src_abs), &mut |_p, md| {
        let ft = md.file_type();
        if !md.is_dir() && !ft.is_file() {
            bad = Some("non-regular".into());
        }
    });
    if bad.is_some() {
        return ferr("cannot copy non-regular file");
    }
    let md = std::fs::metadata(&src_abs)?;
    if md.is_dir() {
        std::fs::create_dir(&dst_abs)?;
        let res = copy_tree(Path::new(&src_abs), Path::new(&dst_abs));
        if res.is_err() {
            let _ = std::fs::remove_dir_all(&dst_abs);
        }
        return res;
    }
    copy_exclusive(Path::new(&src_abs), Path::new(&dst_abs))
}

fn walk_all<F: FnMut(&Path, &std::fs::Metadata)>(dir: &Path, f: &mut F) {
    if let Ok(entries) = std::fs::read_dir(dir) {
        for e in entries.flatten() {
            let p = e.path();
            if let Ok(md) = std::fs::symlink_metadata(&p) {
                f(&p, &md);
                if md.is_dir() {
                    walk_all(&p, f);
                }
            }
        }
    }
}

fn copy_tree(src: &Path, dst: &Path) -> FsResult<()> {
    for e in std::fs::read_dir(src)? {
        let e = e?;
        let target = dst.join(e.file_name());
        let md = e.metadata()?;
        if md.is_dir() {
            std::fs::create_dir(&target)?;
            copy_tree(&e.path(), &target)?;
        } else {
            copy_exclusive(&e.path(), &target)?;
        }
    }
    Ok(())
}

fn copy_exclusive(src: &Path, dst: &Path) -> FsResult<()> {
    let md = std::fs::metadata(src)?;
    if !md.file_type().is_file() {
        return ferr("cannot copy non-regular file");
    }
    let mut inp = std::fs::File::open(src)?;
    let mut out = std::fs::OpenOptions::new().write(true).create_new(true).open(dst)?;
    std::io::copy(&mut inp, &mut out)?;
    drop(out);
    // preserve mtime (Go Chtimes)
    if let Ok(mtime) = md.modified() {
        let _ = filetime_set(dst, mtime);
    }
    Ok(())
}

// preserve mtime (Go Chtimes) via utimensat FFI (no libc dep needed).
#[repr(C)]
struct Timespec {
    tv_sec: i64,
    tv_nsec: i64,
}
extern "C" {
    fn utimensat(dirfd: i32, pathname: *const u8, times: *const Timespec, flags: i32) -> i32;
}
fn filetime_set(p: &Path, t: std::time::SystemTime) -> std::io::Result<()> {
    let dur = t.duration_since(std::time::UNIX_EPOCH).unwrap_or_default();
    let ts = Timespec { tv_sec: dur.as_secs() as i64, tv_nsec: dur.subsec_nanos() as i64 };
    let times = [ts, Timespec { tv_sec: dur.as_secs() as i64, tv_nsec: dur.subsec_nanos() as i64 }];
    let cpath = std::ffi::CString::new(p.as_os_str().as_encoded_bytes()).unwrap_or_default();
    let rc = unsafe { utimensat(-100, cpath.as_ptr() as *const u8, times.as_ptr(), 0) };
    if rc == 0 {
        Ok(())
    } else {
        Err(std::io::Error::last_os_error())
    }
}

/// Multipart spool: temp file in the system temp dir (Go: os.CreateTemp).
/// Returns (path, file); caller writes chunks and reads size via Metadata.
pub fn spool_file() -> FsResult<(PathBuf, std::fs::File)> {
    let dir = std::env::temp_dir();
    let n = TMP_COUNTER.fetch_add(1, Ordering::Relaxed);
    let name = format!("fileship-upload-{}-{}-{}", std::process::id(), n, rand_hex(4));
    let path = dir.join(&name);
    let f = std::fs::OpenOptions::new().write(true).create_new(true).open(&path)?;
    Ok((path, f))
}

pub fn copy_file_overwrite(src: &Path, dst: &Path) -> FsResult<()> {
    if let Some(parent) = dst.parent() {
        std::fs::create_dir_all(parent)?;
    }
    std::fs::copy(src, dst)?;
    Ok(())
}

pub fn copy_dir(src: &Path, dst: &Path) -> FsResult<()> {
    std::fs::create_dir_all(dst)?;
    for e in std::fs::read_dir(src)? {
        let e = e?;
        let target = dst.join(e.file_name());
        if e.file_type()?.is_dir() {
            copy_dir(&e.path(), &target)?;
        } else {
            copy_file_overwrite(&e.path(), &target)?;
        }
    }
    Ok(())
}

// ── MIME ────────────────────────────────────────────────

pub fn mime_type(name: &str) -> String {
    let ext = name.rsplit('.').next().map(|e| format!(".{}", e.to_lowercase())).unwrap_or_default();
    let m = match ext.as_str() {
        ".mp4" | ".m4v" => "video/mp4",
        ".webm" => "video/webm",
        ".mov" => "video/quicktime",
        ".mkv" => "video/x-matroska",
        ".avi" => "video/x-msvideo",
        ".wmv" => "video/x-ms-wmv",
        ".flv" => "video/x-flv",
        ".ogv" => "video/ogg",
        ".ogg" | ".oga" => "audio/ogg",
        ".3gp" => "video/3gpp",
        ".mts" | ".m2ts" => "video/mp2t",
        ".mp3" => "audio/mpeg",
        ".wav" => "audio/wav",
        ".flac" => "audio/flac",
        ".m4a" => "audio/mp4",
        ".jpg" | ".jpeg" => "image/jpeg",
        ".png" => "image/png",
        ".gif" => "image/gif",
        ".webp" => "image/webp",
        ".svg" => "image/svg+xml",
        ".pdf" => "application/pdf",
        _ => "",
    };
    if !m.is_empty() {
        return m.to_string();
    }
    let e = ext.trim_start_matches('.');
    if let Some(mt) = mime_guess::from_ext(e).first() {
        return mt.to_string();
    }
    "application/octet-stream".to_string()
}

pub fn type_allowed(filename: &str, allowed_types: &str) -> bool {
    if allowed_types.is_empty() {
        return true;
    }
    let mt = mime_type(filename);
    for allowed in allowed_types.split(',') {
        let allowed = allowed.trim();
        if allowed.ends_with('/') {
            if mt.starts_with(allowed) {
                return true;
            }
        } else if mt == allowed {
            return true;
        }
    }
    false
}

pub fn detect_mime(root: &str, rel: &str) -> FsResult<String> {
    let abs = resolve(root, rel)?;
    let mut f = std::fs::File::open(&abs)?;
    let mut buf = vec![0u8; 512];
    let n = f.read(&mut buf).unwrap_or(0);
    buf.truncate(n);
    let detected = detect_content_type(&buf);
    if detected == "application/octet-stream" {
        return Ok(mime_type(rel));
    }
    Ok(detected.to_string())
}

/// Port of Go net/http.DetectContentType (sniff.go) — subset with identical results
/// for all common types.
pub fn detect_content_type(data: &[u8]) -> &'static str {
    // XML/HTML patterns (Go trims leading whitespace, case-insensitive)
    let mut s = data;
    while !s.is_empty() && matches!(s[0], b' ' | b'\t' | b'\r' | b'\n') {
        s = &s[1..];
    }
    let lower: Vec<u8> = s.iter().map(|b| b.to_ascii_lowercase()).collect();
    for pat in [
        "<?xml", "<!doctype", "<html", "<head", "<script", "<iframe", "<h1", "<div", "<font", "<table", "<a", "<style",
        "<title", "<b", "<body", "<br", "<p", "<!--",
    ] {
        if lower.starts_with(pat.as_bytes()) {
            if pat == "<?xml" || pat == "<!doctype" {
                return "text/xml; charset=utf-8";
            }
            return "text/html; charset=utf-8";
        }
    }
    // Signaturen
    let sig = |pat: &[u8]| data.starts_with(pat);
    if sig(&[0xFF, 0xD8, 0xFF]) {
        return "image/jpeg";
    }
    if sig(b"\x89PNG\r\n\x1a\n") {
        return "image/png";
    }
    if sig(b"GIF87a") || sig(b"GIF89a") {
        return "image/gif";
    }
    if sig(b"%PDF-") {
        return "application/pdf";
    }
    if sig(b"PK\x03\x04") || sig(b"PK\x05\x06") || sig(b"PK\x07\x08") {
        return "application/zip";
    }
    if sig(&[0x1F, 0x8B, 0x08]) {
        return "application/x-gzip";
    }
    if sig(b"Rar!\x1A\x07\x00") || sig(b"Rar!\x1A\x07\x01") {
        return "application/x-rar-compressed";
    }
    if sig(&[0x00, 0x00, 0x01, 0x00]) {
        return "image/vnd.microsoft.icon";
    }
    if data.len() >= 12 && &data[0..4] == b"RIFF" {
        if &data[8..12] == b"WEBP" {
            return "image/webp";
        }
        if &data[8..12] == b"AVI " {
            return "video/x-msvideo";
        }
    }
    if data.len() >= 3 && &data[0..3] == b"ID3" {
        return "audio/mpeg";
    }
    if sig(&[0xFF, 0xFB]) || sig(&[0xFF, 0xF3]) || sig(&[0xFF, 0xF2]) {
        return "audio/mpeg";
    }
    if sig(b"OggS\x00") {
        return "application/ogg";
    }
    if sig(b"fLaC") {
        return "audio/flac";
    }
    if data.len() >= 12 && &data[4..8] == b"ftyp" {
        return "video/mp4";
    }
    if sig(&[0x1A, 0x45, 0xDF, 0xA3]) {
        return "video/webm";
    }
    if sig(b"TrueType") || sig(b"OTTO") || sig(b"ttcf") {
        return "font/ttf";
    }
    if sig(b"wOFF") {
        return "font/woff";
    }
    if sig(b"wOF2") {
        return "font/woff2";
    }
    if sig(b"-----BEGIN ") {
        return "application/x-pem-file";
    }
    if sig(&[0x00, 0x01, 0x00, 0x00]) {
        return "application/octet-stream";
    }
    // Text detection like Go: no control chars except \t \n \f \r
    let is_text = data.iter().all(|&b| {
        b == b'\t' || b == b'\n' || b == b'\x0C' || b == b'\r' || (0x20..=0x7E).contains(&b) || b >= 0x80
    });
    if is_text {
        return "text/plain; charset=utf-8";
    }
    "application/octet-stream"
}

// ── Thumbnails / Video ──────────────────────────────────

pub fn is_video(filename: &str) -> bool {
    let ext = filename.rsplit('.').next().unwrap_or("").to_lowercase();
    matches!(
        ext.as_str(),
        "mp4" | "m4v" | "webm" | "mov" | "mkv" | "avi" | "wmv" | "flv" | "ogv" | "3gp" | "mts" | "m2ts"
    )
}

pub fn ext_of(filename: &str) -> String {
    filename.rsplit('.').next().unwrap_or("").to_lowercase()
}

pub fn is_image(filename: &str) -> bool {
    let ext = ext_of(filename);
    matches!(ext.as_str(), "jpg" | "jpeg" | "png" | "gif" | "webp" | "bmp" | "tiff" | "heic" | "heif" | "hif" | "avif")
}

pub fn is_pdf(filename: &str) -> bool {
    ext_of(filename) == "pdf"
}

pub fn is_heic(filename: &str) -> bool {
    matches!(ext_of(filename).as_str(), "heic" | "heif" | "hif")
}

/// Find an external tool in PATH (no shell toolchain assumed).
pub fn find_binary(name: &str) -> Option<String> {
    let path_var = std::env::var("PATH").unwrap_or_default();
    for dir in path_var.split(':') {
        let cand = format!("{}/{}", dir, name);
        if Path::new(&cand).is_file() {
            return Some(cand);
        }
    }
    None
}

/// Convert a PDF (first page) or HEIC/HEIF image to a JPEG file that the
/// image crate can decode. Returns the path of the temp file.
fn convert_to_jpeg(abs: &str, rel: &str) -> FsResult<String> {
    use std::sync::atomic::{AtomicU64, Ordering};
    static CTR: AtomicU64 = AtomicU64::new(0);
    let n = CTR.fetch_add(1, Ordering::Relaxed);
    let tmp_base = std::env::temp_dir().join(format!("fileship_conv_{}_{}", std::process::id(), n));
    let tmp_base = tmp_base.to_string_lossy().to_string();
    let ok = if is_pdf(rel) {
        // pdftoppm -png -singlefile renders page 1 -> "<base>.png"
        let tool = find_binary("pdftoppm").ok_or_else(|| FsError("pdftoppm not available (install poppler-utils)".into()))?;
        std::process::Command::new(&tool)
            .args(["-png", "-singlefile", "-f", "1", "-l", "1", "-r", "110", abs, &tmp_base])
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .status()
            .map(|s| s.success())
            .unwrap_or(false)
    } else if is_heic(rel) {
        let tool = find_binary("heif-convert").ok_or_else(|| FsError("heif-convert not available (install libheif-examples)".into()))?;
        std::process::Command::new(&tool)
            .args([abs, &format!("{}.jpg", tmp_base)])
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .status()
            .map(|s| s.success())
            .unwrap_or(false)
    } else {
        return ferr("unsupported conversion");
    };
    if !ok {
        return ferr("conversion failed");
    }
    Ok(if is_pdf(rel) { format!("{}.png", tmp_base) } else { format!("{}.jpg", tmp_base) })
}

fn ffmpeg_path() -> Option<String> {
    use std::sync::OnceLock;
    static P: OnceLock<Option<String>> = OnceLock::new();
    P.get_or_init(|| {
        // `which ffmpeg` without a shell toolchain: search PATH
        let path_var = std::env::var("PATH").unwrap_or_default();
        for dir in path_var.split(':') {
            let cand = format!("{}/ffmpeg", dir);
            if Path::new(&cand).exists() {
                return Some(cand);
            }
        }
        None
    })
    .clone()
}

pub fn ffmpeg_binary() -> Option<String> {
    ffmpeg_path()
}

pub fn ffmpeg_available() -> bool {
    ffmpeg_path().is_some()
}

pub fn thumbnail_name(root: &str, rel: &str) -> FsResult<String> {
    let abs = resolve(root, rel)?;
    let md = std::fs::metadata(&abs)?;
    if !md.file_type().is_file() {
        return ferr("thumbnail source is not a regular file");
    }
    let mtime_nanos = md
        .modified()
        .ok()
        .and_then(|t| t.duration_since(std::time::UNIX_EPOCH).ok())
        .map(|d| d.as_nanos() as i64)
        .unwrap_or(0);
    let key = format!("{}\x00{}\x00{}", abs, md.len(), mtime_nanos);
    use sha2::{Digest, Sha256};
    let mut h = Sha256::new();
    h.update(key.as_bytes());
    Ok(format!("{}.jpg", hex::encode(h.finalize())))
}

pub fn thumbnail_exists(root: &str, thumb_dir: &str, rel: &str) -> Option<String> {
    let name = thumbnail_name(root, rel).ok()?;
    if Path::new(thumb_dir).join(&name).exists() {
        Some(name)
    } else {
        None
    }
}

pub fn generate_thumbnail(root: &str, rel: &str, thumb_dir: &str) -> FsResult<String> {
    let abs = resolve(root, rel)?;
    let conv_tmp: Option<String> = if is_pdf(rel) || is_heic(rel) {
        Some(convert_to_jpeg(&abs, rel)?)
    } else {
        None
    };
    let src = conv_tmp.as_deref().unwrap_or(&abs);
    let decoded = image::ImageReader::open(src).map_err(|e| FsError(e.to_string()))?.decode().map_err(|e| FsError(e.to_string()));
    if let Some(tmp) = &conv_tmp {
        let _ = std::fs::remove_file(tmp);
    }
    let img = decoded?;
    // imaging.Thumbnail(..., Lanczos) — Einpassen in 256x256, Aspect bleibt
    let (w, h) = (img.width(), img.height());
    let scale = (256f64 / w as f64).min(256f64 / h as f64).min(1.0);
    let nw = ((w as f64 * scale).round() as u32).max(1);
    let nh = ((h as f64 * scale).round() as u32).max(1);
    let thumb = img.resize_exact(nw, nh, image::imageops::FilterType::Lanczos3);
    let name = thumbnail_name(root, rel)?;
    std::fs::create_dir_all(thumb_dir)?;
    let thumb_path = Path::new(thumb_dir).join(&name);
    let mut out = std::fs::File::create(&thumb_path)?;
    let rgb = thumb.to_rgb8();
    let mut enc = image::codecs::jpeg::JpegEncoder::new_with_quality(&mut out, 80);
    enc.encode_image(&rgb).map_err(|e| FsError(e.to_string()))?;
    Ok(name)
}

pub fn generate_video_thumbnail(root: &str, rel: &str, thumb_dir: &str) -> FsResult<String> {
    let ffmpeg = ffmpeg_path().ok_or_else(|| FsError("ffmpeg not available".into()))?;
    let abs = resolve(root, rel)?;
    let name = thumbnail_name(root, rel)?;
    std::fs::create_dir_all(thumb_dir)?;
    let thumb_path = Path::new(thumb_dir).join(&name);
    let thumb_str = thumb_path.to_string_lossy().to_string();
    let abs_str = abs.clone();
    let run = |args: &[&str]| -> bool {
        std::process::Command::new(&ffmpeg)
            .args(args)
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .status()
            .map(|s| s.success())
            .unwrap_or(false)
    };
    let ok = run(&[
        "-ss", "00:00:05", "-i", &abs_str, "-vframes", "1", "-vf",
        "thumbnail,scale=256:256:force_original_aspect_ratio=increase,crop=256:256", "-q:v", "3", "-y", &thumb_str,
    ]);
    if !ok {
        let ok2 = run(&[
            "-i", &abs_str, "-vframes", "1", "-vf",
            "scale=256:256:force_original_aspect_ratio=increase,crop=256:256", "-q:v", "3", "-y", &thumb_str,
        ]);
        if !ok2 {
            return ferr("ffmpeg failed");
        }
    }
    Ok(name)
}

pub fn read_text_file(root: &str, rel: &str) -> FsResult<String> {
    let abs = resolve(root, rel)?;
    let md = std::fs::metadata(&abs)?;
    if md.len() > (1 << 20) {
        return ferr("file too large for editor (max 1MB)");
    }
    Ok(std::fs::read_to_string(&abs)?)
}

pub fn write_text_file(root: &str, rel: &str, content: &str) -> FsResult<()> {
    if content.len() > (1 << 20) {
        return ferr("file too large for editor (max 1MB)");
    }
    let mut r: &[u8] = content.as_bytes();
    save_upload(root, rel, &mut r)
}

// ── Archive ─────────────────────────────────────────────

pub struct ArchiveEntry {
    pub abs_path: String,
    pub name: String,
    pub is_dir: bool,
    pub size: i64,
    pub mode: u32,
    pub mtime: i64,
}

pub fn prepare_archive(root: &str, paths: &[String]) -> FsResult<Vec<ArchiveEntry>> {
    let root_abs = resolve(root, "")?;
    let mut entries: Vec<ArchiveEntry> = Vec::new();
    let mut seen: std::collections::HashSet<String> = std::collections::HashSet::new();
    for path in paths {
        let clean = clean_relative_path(path)?;
        let abs = resolve(root, &clean)?;
        collect_archive_entries(Path::new(&abs), Path::new(&root_abs), &mut entries, &mut seen)?;
    }
    Ok(entries)
}

fn collect_archive_entries(
    abs: &Path,
    root_abs: &Path,
    entries: &mut Vec<ArchiveEntry>,
    seen: &mut std::collections::HashSet<String>,
) -> FsResult<()> {
    let md = std::fs::symlink_metadata(abs)?;
    let name = abs
        .strip_prefix(root_abs)
        .map(|p| to_slash(p))
        .unwrap_or_default();
    if !md.is_dir() && !md.file_type().is_file() {
        return ferr("cannot archive non-regular file");
    }
    if !seen.contains(&name) {
        if entries.len() >= 100000 {
            return ferr("too many archive entries");
        }
        #[cfg(unix)]
        let mode = {
            use std::os::unix::fs::PermissionsExt;
            md.permissions().mode()
        };
        #[cfg(not(unix))]
        let mode = 0o644u32;
        entries.push(ArchiveEntry {
            abs_path: abs.to_string_lossy().to_string(),
            name: name.clone(),
            is_dir: md.is_dir(),
            size: md.len() as i64,
            mode,
            mtime: md.modified().ok().and_then(|t| t.duration_since(std::time::UNIX_EPOCH).ok()).map(|d| d.as_secs() as i64).unwrap_or(0),
        });
        seen.insert(name);
    }
    if md.is_dir() {
        for e in std::fs::read_dir(abs)? {
            let e = e?;
            collect_archive_entries(&e.path(), root_abs, entries, seen)?;
        }
    }
    Ok(())
}

pub fn write_archive<W: Write>(entries: &[ArchiveEntry], format: &str, mut output: W) -> FsResult<()> {
    match format {
        "zip" => write_zip(entries, output),
        "tar" => write_tar(entries, &mut output, false),
        "targz" => write_tar(entries, &mut output, true),
        _ => ferr("unsupported archive format"),
    }
}

/// Streaming ZIP writer over a non-seekable sink (no temp file, no RAM
/// buffering of whole archives). Entries use the data-descriptor flag
/// (bit 3): CRC/sizes are written after each file's data, then the central
/// directory is emitted at the end with the real offsets we tracked.
fn write_zip<W: Write>(entries: &[ArchiveEntry], mut out: W) -> FsResult<()> {
    struct CentralDir {
        name: Vec<u8>,
        is_dir: bool,
        mode: u32,
        method: u16,
        mod_time: u16,
        mod_date: u16,
        crc: u32,
        comp_size: u32,
        uncomp_size: u32,
        local_offset: u64,
    }
    let mut central: Vec<CentralDir> = Vec::new();
    let mut offset: u64 = 0;

    macro_rules! w {
        ($buf:expr) => {{
            out.write_all($buf)?;
            offset += $buf.len() as u64;
        }};
    }

    for entry in entries {
        let name = if entry.is_dir {
            format!("{}/", entry.name.trim_end_matches('/'))
        } else {
            entry.name.clone()
        };
        let name_bytes = name.as_bytes();
        let method: u16 = if entry.is_dir { 0 } else { 8 }; // stored / deflated
        let flags: u16 = 0x08 | 0x800; // data descriptor + UTF-8 names
        let zip_time = dos_datetime(entry.mtime);
        let local_offset = offset;

        let mut header = Vec::with_capacity(30);
        header.extend_from_slice(b"PK\x03\x04");
        header.extend_from_slice(&20u16.to_le_bytes()); // version needed
        header.extend_from_slice(&flags.to_le_bytes());
        header.extend_from_slice(&method.to_le_bytes());
        header.extend_from_slice(&((zip_time & 0xFFFF) as u16).to_le_bytes()); // mod time
        header.extend_from_slice(&((zip_time >> 16) as u16).to_le_bytes()); // mod date
        header.extend_from_slice(&0u32.to_le_bytes()); // crc (data descriptor)
        header.extend_from_slice(&0u32.to_le_bytes()); // comp size
        header.extend_from_slice(&0u32.to_le_bytes()); // uncomp size
        header.extend_from_slice(&(name_bytes.len() as u16).to_le_bytes());
        header.extend_from_slice(&0u16.to_le_bytes()); // extra len
        header.extend_from_slice(name_bytes);
        w!(&header);

        let mut crc = crc32fast::Hasher::new();
        let mut comp_size: u64 = 0;
        let mut uncomp_size: u64 = 0;
        if !entry.is_dir {
            let f = std::fs::File::open(&entry.abs_path)?;
            let mut br = std::io::BufReader::with_capacity(1 << 20, f);
            // Write compressed bytes to a small counting sink so each chunk
            // lands in the output (and offsets) immediately.
            let mut sink = CountingWriter { inner: &mut out, written: 0 };
            {
                let mut enc = flate2::write::DeflateEncoder::new(&mut sink, flate2::Compression::fast());
                let mut buf = vec![0u8; 1 << 20];
                loop {
                    let n = std::io::Read::read(&mut br, &mut buf)?;
                    if n == 0 {
                        break;
                    }
                    crc.update(&buf[..n]);
                    enc.write_all(&buf[..n])?;
                    uncomp_size += n as u64;
                }
                enc.finish()?;
            }
            comp_size = sink.written;
            offset += comp_size;
        }
        let crc = crc.finalize();

        // Data descriptor: signature + crc + comp + uncomp sizes.
        let mut dd = Vec::with_capacity(16);
        dd.extend_from_slice(b"PK\x07\x08");
        dd.extend_from_slice(&crc.to_le_bytes());
        dd.extend_from_slice(&(comp_size as u32).to_le_bytes());
        dd.extend_from_slice(&(uncomp_size as u32).to_le_bytes());
        w!(&dd);

        central.push(CentralDir {
            name: name_bytes.to_vec(),
            is_dir: entry.is_dir,
            mode: entry.mode,
            method,
            mod_time: (zip_time & 0xFFFF) as u16,
            mod_date: (zip_time >> 16) as u16,
            crc,
            comp_size: comp_size as u32,
            uncomp_size: uncomp_size as u32,
            local_offset,
        });
    }

    // Central directory.
    let cd_offset = offset;
    let mut cd_size: u64 = 0;
    for e in &central {
        let mut cd = Vec::with_capacity(46 + e.name.len());
        cd.extend_from_slice(b"PK\x01\x02");
        cd.extend_from_slice(&20u16.to_le_bytes()); // version made by
        cd.extend_from_slice(&20u16.to_le_bytes()); // version needed
        cd.extend_from_slice(&(0x08u16 | 0x800).to_le_bytes()); // flags
        cd.extend_from_slice(&e.method.to_le_bytes());
        cd.extend_from_slice(&e.mod_time.to_le_bytes());
        cd.extend_from_slice(&e.mod_date.to_le_bytes());
        cd.extend_from_slice(&e.crc.to_le_bytes());
        cd.extend_from_slice(&e.comp_size.to_le_bytes());
        cd.extend_from_slice(&e.uncomp_size.to_le_bytes());
        cd.extend_from_slice(&(e.name.len() as u16).to_le_bytes());
        cd.extend_from_slice(&0u16.to_le_bytes()); // extra
        cd.extend_from_slice(&0u16.to_le_bytes()); // comment
        cd.extend_from_slice(&0u16.to_le_bytes()); // disk number
        cd.extend_from_slice(&0u16.to_le_bytes()); // internal attrs
        cd.extend_from_slice(&e.mode.wrapping_shl(16).to_le_bytes()); // external attrs (unix mode)
        cd.extend_from_slice(&(e.local_offset as u32).to_le_bytes());
        cd.extend_from_slice(&e.name);
        w!(&cd);
        cd_size += cd.len() as u64;
    }

    // End of central directory.
    let mut eocd = Vec::with_capacity(22);
    eocd.extend_from_slice(b"PK\x05\x06");
    eocd.extend_from_slice(&0u16.to_le_bytes()); // disk
    eocd.extend_from_slice(&0u16.to_le_bytes()); // cd start disk
    eocd.extend_from_slice(&(central.len() as u16).to_le_bytes());
    eocd.extend_from_slice(&(central.len() as u16).to_le_bytes());
    eocd.extend_from_slice(&(cd_size as u32).to_le_bytes());
    eocd.extend_from_slice(&(cd_offset as u32).to_le_bytes());
    eocd.extend_from_slice(&0u16.to_le_bytes()); // comment len
    w!(&eocd);
    Ok(())
}

struct CountingWriter<'a, W: Write> {
    inner: &'a mut W,
    written: u64,
}

impl<W: Write> Write for CountingWriter<'_, W> {
    fn write(&mut self, buf: &[u8]) -> std::io::Result<usize> {
        let n = self.inner.write(buf)?;
        self.written += n as u64;
        Ok(n)
    }
    fn flush(&mut self) -> std::io::Result<()> {
        self.inner.flush()
    }
}

/// DOS date/time pair packed into one u32 (time in low 16, date in high
/// 16) — the ZIP local/central headers store them as two u16s.
fn dos_datetime(unix_secs: i64) -> u32 {
    use chrono::Datelike;
    use chrono::TimeZone;
    use chrono::Timelike;
    let dt = chrono::Local.timestamp_opt(unix_secs, 0).single()
        .unwrap_or_else(|| chrono::Local.timestamp_opt(0, 0).unwrap());
    let time: u16 = ((dt.hour() as u16) << 11) | ((dt.minute() as u16) << 5) | ((dt.second() as u16) >> 1);
    let year = dt.year().max(1980).min(2107) as u16;
    let date: u16 = ((year - 1980) << 9) | ((dt.month() as u16) << 5) | (dt.day() as u16);
    ((date as u32) << 16) | (time as u32)
}

fn write_tar<W: Write>(entries: &[ArchiveEntry], output: &mut W, gzip: bool) -> FsResult<()> {
    if gzip {
        let enc = flate2::write::GzEncoder::new(&mut *output, flate2::Compression::default());
        return write_tar_plain(entries, enc);
    }
    write_tar_plain(entries, output)
}

fn write_tar_plain<W: Write>(entries: &[ArchiveEntry], output: W) -> FsResult<()> {
    let mut builder = tar::Builder::new(output);
    for entry in entries {
        let mut header = tar::Header::new_gnu();
        header.set_size(entry.size as u64);
        header.set_mode(entry.mode & 0o7777); // Go FileInfoHeader: nur Perm-Bits
        header.set_mtime(entry.mtime as u64);
        header.set_entry_type(if entry.is_dir { tar::EntryType::Directory } else { tar::EntryType::Regular });
        header.set_cksum();
        let name = if entry.is_dir && !entry.name.ends_with('/') {
            format!("{}/", entry.name)
        } else {
            entry.name.clone()
        };
        if entry.is_dir {
            builder.append_data(&mut header, &name, std::io::empty())?;
        } else {
            let f = std::fs::File::open(&entry.abs_path)?;
            builder.append_data(&mut header, &name, f)?;
        }
    }
    builder.finish()?;
    Ok(())
}

// ── Transfer (EXDEV-capable) ──────────────────────────────

pub fn transfer(src: &str, dst: &str) -> FsResult<()> {
    match std::fs::symlink_metadata(dst) {
        Ok(_) => return ferr("file already exists"),
        Err(e) if e.kind() != std::io::ErrorKind::NotFound => return Err(e.into()),
        _ => {}
    }
    match std::fs::rename(src, dst) {
        Ok(()) => return Ok(()),
        Err(e) if e.raw_os_error() == Some(18) => {}
        Err(e) => return Err(e.into()),
    }
    // EXDEV: copy without following symlinks
    let md = std::fs::metadata(src)?;
    let mut bad = false;
    walk_all(Path::new(src), &mut |_p, m| {
        if !m.is_dir() && !m.file_type().is_file() {
            bad = true;
        }
    });
    if bad {
        return ferr("cannot transfer non-regular file");
    }
    let res = if md.is_dir() { copy_dir(Path::new(src), Path::new(dst)) } else { copy_file_overwrite(Path::new(src), Path::new(dst)) };
    if res.is_err() {
        let _ = std::fs::remove_dir_all(dst);
        let _ = std::fs::remove_file(dst);
        return res;
    }
    let p = Path::new(src);
    if p.is_dir() {
        std::fs::remove_dir_all(p)?;
    } else {
        std::fs::remove_file(p)?;
    }
    Ok(())
}

// ── Unzip ───────────────────────────────────────────────

pub const MAX_UNZIP_SIZE: i64 = 2 << 30;

pub struct UnzipEntry {
    pub path: String,
    pub is_dir: bool,
    pub size: i64,
    pub index: usize,
}

pub struct UnzipPlan {
    pub entries: Vec<UnzipEntry>,
    pub zip_path: String,
}

pub fn prepare_unzip(root: &str, zip_rel: &str, dest_rel: &str) -> FsResult<UnzipPlan> {
    let zip_abs = resolve(root, zip_rel)?;
    let _ = resolve(root, dest_rel)?;
    let file = std::fs::File::open(&zip_abs)?;
    let mut archive = zip::ZipArchive::new(file).map_err(|e| FsError(e.to_string()))?;
    if archive.len() > 100000 {
        return ferr("too many archive entries");
    }
    let mut seen: HashMap<String, bool> = HashMap::new();
    let mut total: i64 = 0;
    let mut entries: Vec<UnzipEntry> = Vec::new();
    for i in 0..archive.len() {
        let zf = archive.by_index(i).map_err(|e| FsError(e.to_string()))?;
        let raw_name = zf.name().unwrap_or_default().to_string();
        let is_dir = zf.is_dir();
        let name = raw_name.trim_end_matches('/').to_string();
        if name.contains('\\') || name.contains("..") || name.starts_with('/') {
            return ferr(&format!("illegal path in zip: {}", raw_name));
        }
        let clean = clean_relative_path(&name).map_err(|_| FsError(format!("illegal path in zip: {}", raw_name)))?;
        if clean != name {
            return ferr(&format!("illegal path in zip: {}", raw_name));
        }
        // Reject non-regular files (symlink etc.) — check unix_mode if available
        if let Some(mode) = zf.unix_mode() {
            let fmt = mode & 0o170000;
            if fmt != 0 && fmt != 0o100000 && fmt != 0o040000 {
                return ferr(&format!("cannot extract non-regular file: {}", raw_name));
            }
        }
        let rel = go_clean(&format!("{}/{}", dest_rel, clean));
        if seen.contains_key(&rel) {
            return ferr(&format!("duplicate path in zip: {}", raw_name));
        }
        seen.insert(rel.clone(), is_dir);
        let abs = resolve(root, &rel)?;
        if let Ok(info) = std::fs::symlink_metadata(&abs) {
            if !is_dir || !info.is_dir() {
                return ferr(&format!("destination exists: {}", rel));
            }
        }
        let size = zf.size() as i64;
        if size > MAX_UNZIP_SIZE - total {
            return ferr("zip extraction limit exceeded (max 2GB)");
        }
        total += size;
        entries.push(UnzipEntry { path: rel, is_dir, size, index: i });
    }
    // File-as-folder check across all entries
    for entry in &entries {
        let mut parent = parent_of(&entry.path);
        while parent != "." && !parent.is_empty() {
            let key = parent.clone();
            if let Some(&is_dir) = seen.get(&key) {
                if !is_dir {
                    return ferr(&format!("file used as directory in zip: {}", parent));
                }
            }
            let abs = resolve(root, &parent)?;
            if let Ok(info) = std::fs::metadata(&abs) {
                if !info.is_dir() {
                    return ferr(&format!("destination is not a directory: {}", parent));
                }
            }
            parent = parent_of(&parent);
        }
    }
    Ok(UnzipPlan { entries, zip_path: zip_abs })
}

pub fn extract_unzip(root: &str, plan: &UnzipPlan) -> FsResult<()> {
    let file = std::fs::File::open(&plan.zip_path)?;
    let mut archive = zip::ZipArchive::new(file).map_err(|e| FsError(e.to_string()))?;
    for entry in &plan.entries {
        let abs = resolve(root, &entry.path)?;
        if entry.is_dir {
            std::fs::create_dir_all(&abs)?;
            continue;
        }
        if let Some(parent) = Path::new(&abs).parent() {
            std::fs::create_dir_all(parent)?;
        }
        let mut zf = archive.by_index(entry.index).map_err(|e| FsError(e.to_string()))?;
        let mut out = std::fs::OpenOptions::new().write(true).create_new(true).open(&abs)?;
        let mut limited = std::io::Read::by_ref(&mut zf).take(entry.size as u64 + 1);
        let written = std::io::copy(&mut limited, &mut out)?;
        drop(out);
        if written != entry.size as u64 {
            let _ = std::fs::remove_file(&abs);
            return ferr(&format!("invalid uncompressed size: {}", entry.path));
        }
    }
    Ok(())
}

// ── Indexer ─────────────────────────────────────────────

#[derive(Clone)]
pub struct Indexer {
    db: std::sync::Arc<crate::db::Db>,
    running: std::sync::Arc<Mutex<std::collections::HashSet<i64>>>,
}

impl Indexer {
    pub fn new(db: std::sync::Arc<crate::db::Db>) -> Self {
        Indexer { db, running: std::sync::Arc::new(Mutex::new(std::collections::HashSet::new())) }
    }

    pub fn trigger_reindex(&self, user_id: i64, root_path: String) {
        {
            let mut g = self.running.lock().unwrap();
            if g.contains(&user_id) {
                return;
            }
            g.insert(user_id);
        }
        let db = self.db.clone();
        let running = self.running.clone();
        std::thread::spawn(move || {
            let _ = walk_and_index(&db, user_id, &root_path);
            running.lock().unwrap().remove(&user_id);
        });
    }
}

fn walk_and_index(db: &crate::db::Db, user_id: i64, root_path: &str) -> FsResult<()> {
    let root_abs = std::fs::canonicalize(root_path).map_err(|e| FsError(e.to_string()))?;
    let root_abs_str = root_abs.to_string_lossy().to_string();
    let walk_start = crate::model::fmt_db_time(&chrono::Local::now());
    let mut batch: Vec<FileInfo> = Vec::new();
    fn rec(
        db: &crate::db::Db,
        user_id: i64,
        root_abs: &Path,
        root_abs_str: &str,
        dir: &Path,
        batch: &mut Vec<FileInfo>,
    ) -> FsResult<()> {
        let entries = match std::fs::read_dir(dir) {
            Ok(e) => e,
            Err(_) => return Ok(()),
        };
        for e in entries {
            let e = match e {
                Ok(e) => e,
                Err(_) => continue,
            };
            let path = e.path();
            let name = e.file_name().to_string_lossy().to_string();
            if name.starts_with(".fileship-upload-") {
                continue;
            }
            let md = match e.metadata() { Ok(m) => m, Err(_) => continue };
            let rel = path.strip_prefix(root_abs).map(|p| to_slash(p)).unwrap_or_default();
            let is_dir = md.is_dir();
            batch.push(FileInfo {
                name: name.clone(),
                path: rel,
                size: md.len() as i64,
                is_dir,
                mod_time: md.modified().map(|t| chrono::DateTime::<chrono::Local>::from(t)).unwrap_or_else(|_| chrono::Local::now()),
                mime_type: if !is_dir { mime_type(&name) } else { String::new() },
                dir_size: 0,
                starred: false,
            });
            if batch.len() >= 500 {
                db.index_upsert(user_id, root_abs_str, batch);
                batch.clear();
            }
            if is_dir {
                rec(db, user_id, root_abs, root_abs_str, &path, batch)?;
            }
        }
        Ok(())
    }
    rec(db, user_id, &root_abs, &root_abs_str, &root_abs, &mut batch)?;
    if !batch.is_empty() {
        db.index_upsert(user_id, &root_abs_str, &batch);
    }
    db.index_prune_old(user_id, &root_abs_str, &walk_start);
    Ok(())
}
