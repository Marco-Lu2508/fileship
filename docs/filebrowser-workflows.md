# Classic File Browser workflows in Fileship

The reference for this update is the original [File Browser v2.32.0](https://github.com/filebrowser/filebrowser/tree/v2.32.0), specifically its sidebar, file listing, copy/move prompts, information dialog, preview, help and profile views. Fileship implements these workflows in its existing Svelte/Go application. No upstream implementation or translation files were copied.

| Classic workflow | Fileship behavior |
| --- | --- |
| Sidebar, breadcrumbs, search and sorting | Compact file workspace, quick new-file/new-folder actions, favorites and storage usage. Search also has Ctrl/Cmd+F and Ctrl/Cmd+K shortcuts. |
| List, mosaic and image gallery | Three persistent views; gallery images load authenticated thumbnails lazily. |
| Select several files | Checkboxes, Ctrl/Cmd click, Shift range selection, select-all and a selection action bar. |
| Copy and move | Browse destination folders, enter a path, create a destination folder and optionally change one item's destination name. Partial failures remain selected for retry; existing files are never overwritten. |
| Clipboard | Ctrl/Cmd+C, X and V operate on selected workspace items. Paste opens the destination explorer. |
| File information | Size, modification time, MIME type, filesystem mode, recursive folder size/counts, PNG/JPEG/GIF dimensions; SHA-256, SHA-512, SHA-1 and MD5 calculated on request. |
| Upload/download | File/folder uploads with progress and retries, desktop drop targets, selected files/folders archived as ZIP, TAR or TAR.GZ. |
| Preview and edit | Images, video, audio, PDF and text; formatted Markdown/source switch; previous/next navigation; original-size image toggle; text editor with Ctrl/Cmd+S. |
| Shares | Password, expiration and download limits for download links; list/copy/revoke existing links. |
| Profile and language | Hidden-file preference, single/double click, exact dates, password/2FA settings; six complete dictionaries (en, de, fr, es, it, pt). Language changes immediately and persists on the current device. |
| Help | F1 help, F2 rename, Delete to trash, Escape, parent navigation and workspace clipboard shortcuts. |

Fileship additionally retains recoverable trash, WebDAV, API tokens, themes and live updates.

## Scope differences

This update targets everyday file management; it does not claim complete parity with every original File Browser configuration option. EPUB reading, resumable/chunked uploads, shell commands and event hooks, server branding, public folder browsing, and additional archive codecs (BZIP2/XZ/LZ4/Snappy) are not implemented. Public shares currently serve a file or a folder ZIP for download; upload/edit permissions are therefore not offered in the share dialog. Browser preferences are stored per device.

## Validation

Frontend tests also validate theme persistence, invalid saved-theme recovery and text contrast across all seven palettes. Frontend tests cover locale coverage/placeholders, immediate language changes, partial transfer failures, path validation, Markdown sanitization and existing session/navigation regressions. Go tests cover copy conflicts, symlink rejection without partial copies, archive contents/deduplication, destination and nested permissions, quotas, metadata/checksums and permission-rule boundaries. The local browser walkthrough checks copying, moving, checksums, Markdown navigation, archive selection, language switching and image gallery rendering.

Translations are independently authored in `scripts/translations.txt`. Run `npm run locales` in `frontend` to regenerate JSON dictionaries; production builds do this automatically.
