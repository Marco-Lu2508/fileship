# Fileship interface design

The interaction reference is the original File Browser: a persistent navigation rail, breadcrumb/search strip and one direct file workspace. Files remain the visual focus. This update follows the installed Frontend Design and Web Design Guidelines skills, adapted to an existing file manager rather than a landing page.

## Tokens and direction

- Canopy `#234232`: navigation and brand anchor.
- Leaf `#D2E5C3`: active navigation, readable against the canopy.
- Ink `#20382C`: primary text on light surfaces.
- Paper `#FFFFFF`: the file workspace and dialogs.
- Mist `#F4F7F5`: secondary surfaces and page background.
- Rule `#DDE5DF`: boundaries that communicate grouping.

IBM Plex Sans provides clear filenames and controls; system monospace is reserved for source code and checksums. The original font binaries and SIL Open Font License are bundled locally, with no runtime font-service requests. Body/control text follows a 14px base, 12px metadata, 20px dialog headings and 26px folder headings.

```text
Navigation rail | Breadcrumbs                         Search
               | Folder name              File information / Upload
               | File filters                   Refresh / View
               | Selection actions                 Sort
               | Name                         Size / Modified / Actions
               | Files and folders
               | Page controls
```

Everything aligns to the file list. The rail is the identifying visual element; the remaining surfaces use quiet borders and restrained rounding. Seven complete designs change the navigation, workspace, selection and form colors together: Light, Ocean, Violet, Dark, Nord, Solarized and Gruvbox. Light retains the forest palette; the other designs offer blue, purple, graphite, cool gray, teal and warm earth tones. A preview picker applies and saves the choice immediately while preserving the familiar file-manager layout. Decorative hero blocks, repeated cards around unrelated controls and extra statistics would compete with the user's files and are excluded.

## Interaction quality

Dialogs use native modal focus handling, restore focus when dismissed, constrain scrolling and fit small viewports. Controls share consistent sizing and visible keyboard focus. Long names remain readable through truncation, tooltips and accessible names. Dense metadata uses tabular numerals. Touch controls receive larger targets; dark palettes define appropriate foreground colors and native color schemes. Empty states identify the folder state and offer a concrete next action.
