# Archive management UI prototype

This throwaway frontend answers which archive list, reader and editor layout fits
main's current UI. It does not implement document storage, upload, conversion,
PDF annotation, export, locking, autosave or a business API.

Run from `frontend/` with the pinned Node 24 and pnpm toolchain:

```sh
pnpm prototype:archives
```

This builds the mock page using the existing theme and Ant Design configuration,
then refreshes this worktree's isolated preview using its explicit file-assets
mode. The prototype is the preview's root UI; main's real UI source is untouched.
To build without restarting it:

```sh
pnpm prototype:archives --build-only
```

Open `/prototype/archive-management?variant=A` in that preview.
Variants A (table), B (master/detail) and C (document cards) keep the same visual
system. Use the floating bottom bar or arrow keys to switch. Editable inputs do
not consume variant-switching arrow keys.

The app shell is a visual facsimile of main, with an archive entry for evaluation;
it does not modify the real sidebar, router, permissions or other main pages.
The page uses actual main theme files, Ant Design controls and shared row actions.
The standalone HTML in ignored `public/prototype/` is self-contained and makes no
business API requests. Mock changes live only in memory and reset on reload.
The switcher is enabled only by this dedicated prototype build configuration.
The served copy is in ignored `dist/archive-prototype-publish/`. Preview status
identifies it as file assets with the prototype revision, not a normal app build.

The fourth tab, two-state status and document import surface illustrate pending
requirements, rather than recording a final product decision. Confirm style and
scope with the user before rewriting any of this as production code. Do not
commit/push or promote the prototype under the current task authorization.
