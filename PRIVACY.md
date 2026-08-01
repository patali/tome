# Tome — Privacy Policy

_Last updated: 1 August 2026_

Tome is self-hosted. The browser extension talks to **one server: the one you
enter in its settings.** There is no Tome company, no hosted service, and no
account with anybody but the operator of that server — usually a friend, or
you. Nothing in this document describes data going to the authors of Tome,
because none does.

Two parties can hold your data, and it's worth being clear about which is
which:

- **The extension**, running in your browser on your machine.
- **The server you point it at**, run by whoever invited you. If that isn't
  you, they can see what's described under "What the server receives".

## What the extension stores

All of it stays in your browser's extension storage. None is transmitted
anywhere except to your configured server, and only as described below.

| Data | Where | Why |
|---|---|---|
| Your API key | `chrome.storage.local` | Authenticates you to your server. Deliberately **not** synced, so it never travels through your browser vendor's sync service. |
| Server URL | `chrome.storage.sync` | So the extension knows where to send. Syncs across your browser profiles if you have sync on. |
| Button and style preferences | `chrome.storage.sync` | Remembers how you like the popup. |
| Conversion history | `chrome.storage.local` | The last 50 conversions — title, source URL, status, timestamp — so a result isn't lost when the popup closes. Clear it any time with **Clear history** in settings. |
| The article being previewed | `chrome.storage.session` | Held only to hand the article to the preview tab. Cleared when the browser closes. |
| Last seen version | `chrome.storage.local` | So the update notice doesn't nag about a version you dismissed. |

Signing out (settings → **Sign out**) removes the API key. Removing the
extension removes all of it.

## What leaves your browser, and when

Only when **you click a button**. Tome has no background scraping, no
content scripts running on page load, and no automatic reading of tabs. It
holds the `activeTab` permission, which grants access to a page only after you
click the toolbar button, and only for that page.

When you click **Send to Kindle** or **Open preview**, the extension reads the
article from the page you have open and sends it to your configured server:
the title, byline, publication date, source URL, article text and images, and
the reader stylesheet. That is the feature — it cannot render a document
without the document.

The only other requests are to your own server: a status check (which also
asks what extension version it has), and reading or updating your profile.

## What the server receives

Your server stores your **email address**, your **Kindle email address**, and a
**SHA-256 hash of your API key** (never the key itself). It keeps invite codes
until they are used or expire.

Articles are **not stored**. They are rendered to PDF or EPUB in a temporary
directory, sent to your Kindle, and the temporary directory is deleted. What
persists is the conversion history in your own browser, not on the server.

If email delivery is configured, the operator supplies a
[Resend](https://resend.com) API key and the rendered document is emailed to
your Kindle address through Resend. Amazon then receives it, as it must for
Send-to-Kindle to work at all.

## Third parties

**There are none in the extension.** No analytics, no telemetry, no crash
reporting, no advertising or tracking SDK, no CDN. Fonts are bundled in the
package rather than fetched from Google Fonts, specifically so that opening a
preview doesn't disclose to a third party that you're reading something.

Downstream of your server, exactly two services are involved, and only if the
operator set up delivery: **Resend**, which transmits the email, and
**Amazon**, which receives the document on your Kindle. Both are inherent to
emailing a file to a Kindle.

## What is never done

- Your data is **never sold**, and never shared with data brokers.
- It is **never used for advertising**, personalisation, or profiling.
- It is **never used to assess creditworthiness** or for lending.
- Your browsing is **not** collected. Tome sees the one page you explicitly
  asked it to convert, at the moment you asked.

## Your control

- **See what's held:** settings → **Recent conversions**.
- **Erase local data:** **Clear history**, **Sign out**, or remove the extension.
- **Erase server data:** ask the operator to delete your account; if you are
  the operator, delete the user with `tome admin users` or delete the data
  volume.
- **Change your Kindle address:** settings → **Account**, any time.

## Permissions, and why each is needed

| Permission | Why |
|---|---|
| `activeTab` | Read the article — only on the tab whose button you clicked, only when you click it. |
| `scripting` | Run the extractor in that page to pull out the article. |
| `storage` | Keep the settings and history described above. |
| `alarms` | Check every six hours whether your server has a newer extension version. |
| `host_permissions` (localhost) | Reach a Tome server on your own machine. |
| Optional host permissions | Reach a Tome server elsewhere. Requested **at the moment you enter its address**, and only for that host. |

## Contact

Tome is open source — read the code, or raise an issue, at
<https://github.com/patali/tome>. For data held by a server you were invited
to, contact whoever invited you; they administer it, not the Tome authors.
