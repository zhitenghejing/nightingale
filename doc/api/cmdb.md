# CMDB API

The center serves `/api/cmdb/*` using the same authentication and disabled-user checks
as `/api/n9e/*`. No separate process, fixed port or identity HTTP callback is needed.
Responses follow `{ "dat": ..., "err": "" }`. Validation failures return 400;
missing identity returns 401; missing CMDB permission returns 403.
The integrated health endpoint also requires authentication.

In the UI, Infrastructure has three direct child links: Devices (`/targets`),
CMDB (`/targets/cmdb`) and Rack Views (`/targets/racks`). They do not use the
old page-header device tabs. Inventory links remain visible independently of
n9e menu operations; their server ACL still controls access to asset data.
Rack Views keeps its cabinet canvas within the available page height, with
scrolling inside the cabinet and rack selector. Usage instructions are available
from the information button on hover or focus; unplaced devices are collapsible.
Room and rack edit/delete actions are available directly in the hierarchy. Device
details expose a confirmed, permission-controlled soft delete in both CMDB and Rack
Views. Soft deletion retains the rack reference for restoration and releases U positions.
Room renaming preserves IDs and bindings; inventory/export resolves current room names.
The rack selector starts with collapsed rooms. Search expands matching rooms.
Cabinets initially fit the viewport and continue fitting on resize until manually
zoomed; Fit Window restores this mode. Up to four cabinets can be selected in
one horizontally scrollable row, with cross-cabinet dragging.

Administrators have every inventory permission. Other users default to no inventory
access; administrators grant read/create/edit/delete/import/export by user ID from
the CMDB permission dialog. These grants cover the entire inventory, not business-group
rows. Rack changes and restoration use edit permission.

| Method | Path below /api/cmdb | Data / result | Permission |
|---|---|---|---|
| GET | health | DB health, no asset data | authenticated |
| GET | access | effective permissions and admin flag | authenticated |
| GET | devices | active asset documents | read |
| GET | devices?deleted=true | recycle bin | read |
| GET | export | exportable active documents | export |
| POST | devices | complete asset, server allocates ID | create |
| PUT | devices/:id | complete asset including version | edit |
| POST | import | array of 1–1000 new assets and missing named cabinets, atomic | import |
| POST | import?mode=placement | fills cabinet and U positions on existing active assets by serial, atomic | import and edit |
| POST | delete | array of id/version documents, atomic soft deletion | delete |
| POST | restore | array of id/version documents, rechecks placement | edit |
| POST | purge | nonempty array of id/version snapshots; permanent removal from recycle bin, atomic | delete |
| GET | catalog | rooms and racks | read |
| GET | rack-alerts | active event summaries keyed by CMDB device ID | read and event visibility |
| POST | rooms | name | edit |
| PUT | rooms | id, name, previousName (optimistic rename check) | edit |
| POST | racks | roomId, name, units | edit |
| PUT | racks | id, version, roomId, name, units | edit |
| DELETE | rooms/:id | delete empty room | delete |
| DELETE | racks/:id | version; delete empty rack | delete |
| GET | changes[?assetId=...] | latest 200 audit records | read |
| GET / PUT | acl | list / userId + boolean permissions | Nightingale admin |

Device fields are defined in `frontend/src/pages/cmdb/model.ts`. `rackId` is authoritative.
During import, a named rack without an ID requires a room name. The import reuses
existing room/rack names and creates missing rooms and cabinets in the same transaction
as the devices. New cabinets start at 42U or the highest ending U in the batch for that
cabinet, whichever is larger, up to 60U. Existing cabinet capacity stays fixed.
The import permission covers this catalog creation, and catalog changes are audited
against the importing user. Manual device creation and editing resolve existing racks.

Use the CMDB **补充机柜位置** upload for devices already in inventory. It matches
active devices by serial number and applies only the location fields. Rows without a
cabinet and devices already occupying U positions are skipped. Devices missing from
active inventory and conflicting U positions reject the batch.

U positions must be paired integers within capacity. Leaving both empty keeps a device
waiting for installation, with its cabinet binding if specified. Successful placements
appear in Rack Views. CSV imports keep the recorded U height and use a generic panel
when an automatically matched product image has a different height. Dragging is a
versioned PUT that preserves the existing U height. Nightingale Target records are
independent of CMDB assets.

Rack Views refreshes active alerts with the inventory. It associates exact IP addresses
from the business and out-of-band management fields. Device endpoint labels such as
`snmp_target` take priority over the collector IP. Target identifiers can resolve through
the monitoring object's host IP, and address labels also support IPs with ports. Alert
summaries honor business-group access to event details and datasource visibility.
The most severe active event determines a placed device's light. Severity 1 is red,
severity 2 orange and severity 3 yellow. The affected-cabinet filter stays available
with lights switched off. The device dialog lists current events and links to their
active event detail pages. Lights clear after recovery on the next successful refresh.


## Persistence and migration

The seven `cmdb_*` tables use the center's configured database (PostgreSQL, MySQL or
SQLite). `models/migrate` and center startup create missing tables and validate required
columns and unique indexes; existing-table schema upgrades must use reviewed SQL.
The placement lock row is required for writes and is checked inside every transaction.
Room/rack deletion also takes this lock, records the deleted object in audit, and
rejects stale rack versions. Rooms containing racks cannot be deleted. Racks bound
to any device (including unplaced and recycled devices) cannot be deleted; restore
and relocate recycled devices, or permanently remove retired devices from the recycle
bin first. Catalog deletion never cascades into devices.
Asset/disks/audit are committed atomically. Imports are all-or-nothing; stale versions
and overlapping U ranges are rejected. Soft deletion releases positions; restoration
checks them again. Serial uniqueness includes deleted assets and uses a normalized key.

The recycle bin offers per-device Permanent Delete and Clear All with an irreversible
confirmation showing the number of devices. Both require delete permission; restore
continues to require edit permission. Clear All submits every loaded recycle-bin
id/version, including other table pages, as an explicit snapshot. Devices deleted
after the snapshot are retained. A missing, duplicate, restored or stale member rejects
the whole operation without partial removal. Refresh the recycle bin before retrying.
Purge removes the device and disk rows in the shared write transaction, freeing serial
uniqueness and rack references. Existing audit history is retained and a `purge` action
is recorded; audit failure rolls the deletion back. Purged devices cannot be restored.
Active inventory, Nightingale Targets and report files are unaffected. The usual 6 MiB
request-body limit applies; purge sends only ids and versions, never full asset payloads.

Reviewed initial schemas are in `doc/sql/cmdb.{mysql,postgres,sqlite}.sql` and the
matching Docker initialization scripts. MySQL upgrades include the schema in
`docker/migratesql/migrate.sql`; PostgreSQL has `docker/migratesql/cmdb.postgres.sql`.
Large payload/audit columns use LONGTEXT on MySQL and TEXT on PostgreSQL/SQLite.
Do not run the old standalone MySQL provisioning scripts against this deployment.

The first version loads inventory for client-side filtering/pagination and refreshes
every ten seconds and on focus. Large fleet search and business-group row isolation
remain future work. Monitor/alarm switches are inventory flags only; they do not
create collectors, rules, or Target associations.

## Source and existing data

The source import includes the two violent-dragonfly commits through `ac1f2bbe`, its
working-tree vector materials, and the minimal external rack viewport dependencies.
Details and validation are recorded in `doc/cmdb-migration-validation.md`.
No existing MySQL assets, account records or report archives are imported automatically.
For an authorized data cutover, back up the source, preserve UUIDs/versions and audit
history, map ACL/report user IDs to the destination identity database, and verify
counts, original-file hashes and ownership before switching traffic.

## Inventory search

The CMDB search panel uses one case-insensitive keyword input across room, rack,
hostname, vendor, serial, business, model and business IP. Whitespace-separated
keywords must all match, and may match different fields. Add Condition keeps
field-specific text matching available without showing eight permanent inputs;
conditions intersect and can be removed individually. Repeating a text field adds
another required substring; duplicate conditions are suppressed.
Device type, warranty type and warranty status support multiple alternatives
within each category. Categories intersect with keywords, text conditions and the
inclusive expiry date range. Either date endpoint may be omitted; assets without
a valid expiry date do not match a bounded range. The date picker uses explicit
start/end placeholders instead of browser-native date input masks.
Filtering updates immediately, resets pagination and clears selected rows so batch
actions cannot retain hidden selections. Export continues to use the filtered rows.
All filtering still consumes the existing ACL-protected inventory read, without
new server endpoints or database changes.

The built-in `search_cmdb_devices` agent tool applies the same CMDB read ACL
to the current chat user. It searches active assets by keyword, room, device
type, warranty type and inclusive `expire_from` / `expire_to` bounds on the
`expire` field. Results are ordered by expiry date then asset ID, with undated
records last for searches without a date range. Responses contain a device
summary, total matches, a count of assets with missing or invalid expiry dates
under the other filters, and a `next_page` when further matches are available.
Bound cabinets include their current `rack_units` capacity. Installed devices
include the recorded `u_start`, `u_end` and `occupied_units`, with a display-ready
`u_range` such as `29U-30U` or `29U`. A device waiting
for installation can have a cabinet capacity without an assigned U range.
