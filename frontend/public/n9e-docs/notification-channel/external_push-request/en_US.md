# Platform 1.0 push

## Current event-label mapping

Only the dedicated channel parameters in notification-rule section 1 change. Section 2 (basic configuration), section 3 (event workflows), filters, mutes and recovery switches keep their existing behavior.

New mappings require explicit event-label keys for deviceType and deviceName. companyId keeps its fixed-text-or-label choice. level still uses event Severity from the alert rule and an explicit level_map (confirmed: 1→灾难, 2→严重, 3→警告). Automatic fields remain problem/ok, FirstTriggerTime, RecoverTime, RuleName and the center-file signature.

deviceTags uses an ordered list of label keys, each explicitly required or optional. Values R650 and HIS become the single JSON string `"R650,HIS"`: ASCII commas, no brackets or keys, no splitting or deduplication of values. Missing/empty optional sources are omitted with visible reasons. Required missing values, conflicting nonempty values or an entirely empty result block delivery.

Sources are immutable persisted event labels, including merged alert-rule appended labels. Workflow changes to a temporary notification copy are not mapping inputs. Enter keys, not values or {variable} templates. Existing CMDB, literal and appended-tag configurations remain available until explicitly converted; conversion preserves company and level settings and leaves unknown label keys blank.

The administrator-only draft preview reads a saved historical event without creating tasks, running workflows, reading keys or sending requests. Recovery business values are taken from the accepted/confirmed original problem when the worker claims the task.

Alert rules control fault counts and repeat intervals: max 0 is unlimited, 1 includes the initial fault, and 3 includes the initial fault plus two reminders. The channel no longer adds a per-incident fault cap or manual reminder approval. Retired repeat_policy metadata remains compatible but does not gate faults. Destination + DispatchID deduplicates the same dispatch; sign is never a deduplication key. Recovery does not consume fault counts, and duplicate recovery protection remains. Unknown results hold later reminders and recovery pending reconciliation. Historical suppressed/cancelled tasks are not automatically requeued. No synthetic problem is created for an orphan recovery.

The following CMDB notes describe retained legacy configurations only.

A saved “1.0平台推送” preset appears in the media list, disabled by default and selectable from existing notification rules. Its persisted identifier stays external_push. User edits survive restart.

Real mode accepts only https://admin.ztservice.cn/messageWarn!acceptWarn.act. Requests are POST forms with Content-Type application/x-www-form-urlencoded; charset=UTF-8. Form field json contains one serialized JSON object. Timeout defaults to 10 seconds; redirects and automatic retries are disabled. A successful call may trigger real account/SMS/phone notifications in platform 1.0. This round prepares disabled configuration only.

Business mappings belong to notification rules. Device type/name can use CMDB.type/hostname with an explicitly configured exact event-label match to hostname, businessIp or managementIp. Missing, ambiguous, deleted or incomplete assets block delivery. There is no implicit Target association or port stripping. Legacy literal/event-label mappings remain available.

deviceTags may use the saved alert-rule append_tags field verbatim, as one JSON string of space-separated key=value entries, not an array or all metric labels. Values are frozen at first task registration; recovery retains the accepted original problem. This is registration-time data, not a historical rule revision. Explicit level_map is required; the confirmed test mapping is 1→灾难, 2→严重, 3→警告. Real mode accepts only the four confirmed receiver levels.

Event types are problem/ok. Recovery preserves original problemTime and adds recoveryTime. The configured timezone is Asia/Shanghai. The center computes uppercase MD5 of UTF-8(timestamp without ASCII spaces + the file key). sign is never an incident or deduplication identifier.

The default key reference platform10-v1 resolves to /etc/n9e/secrets/platform10.key, overridable on center. Provision separately outside Git; previews use an instance-private path. Only a reference and a read-only readiness check appear in the UI. Real mode rejects literal test signatures. Loopback simulation remains separate, and simulated problems cannot anchor real recoveries.

Tasks push events, not person-level messages. Only center workers send. Unknown results, timeouts and expired sending leases are never blindly retried. Explicit reconciliation/resend actions are audited. Existing mute/filter/recovery switches remain effective. Interface acceptance does not prove downstream notifications.


For deviceTags, select CMDB model + business. Both values come from the same
matched asset, model first: `"['R650', 'HIS']"`. This is a JSON string, not an
array. Missing model or business blocks delivery. Values freeze at registration;
recovery retains the accepted problem values. Existing appended-rule-label,
literal and event-label sources remain compatible.

The event label key used for CMDB lookup is not an output JSON field. For
example, matching hostname with label key ident means looking up CMDB.hostname
using the event label ident. The label key must be configured explicitly.
