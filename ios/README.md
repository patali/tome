# Tome for iOS

Share an article from Safari and it lands on your Kindle, typeset by the same
server the browser extension uses.

**The server needs no changes.** The app POSTs the same JSON the extension does
(see `deliver()` in [`extension/background.js`](../extension/background.js)) to
the same `/send-to-kindle` endpoint.

## Two ways in

| | Session it uses | Reaches logged-in sites? |
|---|---|---|
| **Share sheet**, from Safari | Your real Safari session | Yes — nothing to sign in to |
| **In-app browser** | The app's own cookie jar | Yes, after signing in once here |

The share sheet is the main path. iOS runs Tome's extractors *inside the live
Safari page* (`NSExtensionJavaScriptPreprocessingFile`), so a page you're logged
into extracts exactly as it does on the desktop.

**Sharing from anywhere else** — the X app, Reddit, Messages — hands over a bare
URL with no DOM. The extension can't read those itself: it has its own cookie
jar, so fetching the link would arrive logged out, which for X means no article
content at all. So it posts a **notification**; tapping it opens Tome with the
link loading in the in-app browser, where the site sessions actually live. Then
**Send to Kindle**.

The notification detour is not a workaround for something simpler: a share
extension **cannot launch its containing app**. Apple permits that only for
Today extensions and widgets, and the responder-chain `openURL:` trick that used
to work is force-failed from iOS 18 on (*"BUG IN CLIENT OF UIKIT: The caller of
UIApplication.openURL(:) needs to migrate…"*). A local notification is the
documented route across. If notification permission is denied, the sheet falls
back to offering the link with a **Copy link** button.

That's also why the activation rule declares *both*
`NSExtensionActivationSupportsWebPageWithMaxCount` and
`…SupportsWebURLWithMaxCount`. With only the first, Tome never appears in any
share sheet but Safari's.

## "Sign in with Google" doesn't work in the in-app browser

It can't, and this isn't fixable here. Google deliberately blocks OAuth in
embedded webviews (`disallowed_useragent`) to stop apps harvesting credentials,
and detects far more than the user-agent string.

Use the **Safari share sheet** for those sites — it runs against your real
Safari session, so anything you're signed into there works. The in-app browser
is for sites you can sign into directly, like X. Between the two, nothing is
actually locked out.

## Preview

The **Preview** button in the browser renders the article through the server's
`/convert` endpoint and shows the resulting PDF in PDFKit — the real document at
the real page geometry, so where pages break on screen is where they break on
the device. The extension's reader previews styled HTML, which is close but
can't show pagination.

Device, image mode, and body face can be changed from the preview and re-render
in place. Those changes are local to the preview, so you can check a page as
Paperwhite and still send it as Scribe; the defaults live in Settings.
**Save PDF** exports the file (the counterpart of the reader's "Save as PDF"),
and **Send to Kindle** delivers exactly what you're looking at.

Each change is a server round trip — headless Chrome on the far end — so a
render takes a moment rather than updating live. Re-renders replace any
in-flight one instead of queueing.

## One extraction implementation

`Scripts/build-extraction-js.sh` concatenates the extension's own sources into a
single bundle:

```
extension/lib/Readability.js
extension/extractors/x.js
extension/extractors/generic.js
runExtractors()            ← lifted from extension/background.js by marker comment
Scripts/tome-ios-glue.js   ← the only iOS-specific code
```

iOS allows exactly one preprocessing file, hence the concatenation. The
dispatcher is *extracted* rather than copied, between the
`/* #tome:dispatch-start */` markers in `background.js`, so the app and the
extension can never disagree about which extractor claims a page. Adding
`extractors/medium.js` later benefits both with no iOS code change.

The script also copies `extension/reader.css`, which the app ships in the
payload so the PDF matches the desktop output — without it the server falls back
to its own deliberately-compact approximation.

It runs automatically as an Xcode build phase, and validates its output with
`node --check` plus a check that every extractor survived.

## Setup

Requires Xcode (the iOS SDK — Command Line Tools alone is not enough).

```bash
brew install xcodegen

cd ios
./Scripts/setup.sh          # builds the bundle, then generates the project
# add your API key to the Config.xcconfig it just created
open Tome.xcodeproj
```

`setup.sh` exists because the order is load-bearing and easy to get wrong:
`Resources/Generated` is derived and gitignored, so on a fresh clone XcodeGen
has nothing to reference and refuses to generate until the bundle is built.
Re-run it any time `project.yml` changes.

In Xcode, set the signing team on **both** targets (`Tome` and `TomeShare`) to
your personal Apple ID, then run on a real device — the Share Extension needs
real Safari, so the simulator won't exercise the main path.

### Building from the command line

Useful for checking a change compiles without launching Xcode. If
`xcode-select -p` still points at CommandLineTools, override it per-command
rather than switching it globally (no `sudo` needed):

```bash
export DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer
xcodebuild -project Tome.xcodeproj -scheme Tome \
  -sdk iphonesimulator -configuration Debug \
  CODE_SIGNING_ALLOWED=NO CODE_SIGNING_REQUIRED=NO build
```

Both `Info.plist` files are hand-written with `GENERATE_INFOPLIST_FILE = NO`, so
every `CFBundle*` key has to be present explicitly. A missing
`CFBundleIdentifier` in the extension surfaces as *"Embedded binary's bundle
identifier is not prefixed with the parent app's"*, which reads like a naming
problem but isn't.

### Turning on the share sheet entry

Tome is a **Share extension**, not a Safari *web* extension — there is nothing
to enable under Settings → Safari → Extensions. It appears in the share sheet:

1. Open Safari on any article.
2. Tap **Share** (the square with an up arrow).
3. Scroll the top row of app icons right to the end → **More**.
4. Toggle **Tome** on → **Done**. (**Edit** in that list lets you drag it
   nearer the front.)

If Tome isn't listed:

- Launch the app once first. iOS doesn't register an extension until its
  containing app has run.
- Otherwise delete the app and reinstall — iOS caches extension registrations,
  and changes to `Info.plist` activation rules are exactly what goes stale.
- If it worked and then disappeared, that's the 7-day free-provisioning expiry.
  Rebuild from Xcode.

## Free-account limitations

App Groups and Keychain Sharing require a paid membership, which has three
visible consequences:

- **Settings don't reach the share sheet.** The extension uses the server and
  key compiled in from `Config.xcconfig`; changing them in Settings affects only
  the app. Rebuild to change what the share sheet uses.
- **Your API key is in the binary.** Fine for a build that never leaves your
  device — and the reason this build must not be shared.
- **Builds expire after 7 days.** The app stops launching until you rebuild.

All of it is confined to
[`Shared/TomeConfig.swift`](Shared/TomeConfig.swift); moving to a paid account
means changing that one file.

## App Store readiness

This build is for personal sideloading. If it's ever submitted, here's the
honest state. Not legal advice.

**Handled in the project:**

- `PrivacyInfo.xcprivacy` — mandatory since May 2024. Declares no tracking and
  no collected data, with required-reason codes for `UserDefaults` (CA92.1) and
  file timestamps (C617.1).
- `ITSAppUsesNonExemptEncryption = false`, so App Store Connect stops asking.
- App icon asset catalogue. **Currently upscaled from the repo's 512 px icon —
  replace with a true 1024 px render before submitting**, or it will look soft.
- `NSLocalNetworkUsageDescription`, for servers on a LAN.
- No use of private API. The responder-chain `openURL:` trick that a share
  extension would normally reach for is gone — it doesn't work from iOS 18 and
  App Review objects to it.

**Still blocking, and not fixable in code:**

1. **The API key is compiled into the binary.** `Config.xcconfig` bakes it into
   both targets, so anyone with the `.ipa` can read it. The app itself no longer
   needs this — onboarding can obtain a key at runtime — but the share extension
   does, because without App Groups it can't read what the app stored. **A paid
   membership removes this entirely**: App Group + Keychain, and `TOME_API_KEY`
   can be deleted.
2. **Guideline 2.1 — a reviewer can't exercise the app.** Tome is self-hosted
   and invite-only, so a reviewer sees a setup screen and nothing else. Needs a
   demo server plus credentials in App Review notes.
3. **The in-app browser is a general-purpose web browser.** Apps with
   unrestricted web access generally need a 17+ age rating or content
   filtering (1.1.6, 2.3.7).
4. **Privacy policy URL** is required in App Store Connect. The server already
   serves one at `/privacy`.
5. **X's Terms of Service.** `extractors/x.js` reads X's DOM and reformats X
   Articles. X prohibits scraping and enforces it; stores tend to pull apps on a
   credible complaint rather than adjudicate. The mitigation is already latent
   in the design — the extractor registry is pluggable, so per-site extractors
   could be **fetched from the user's own server at runtime** instead of
   shipped, leaving only the generic Readability path in the binary.

## Layout

| Path | What it is |
|---|---|
| `Shared/` | Config, payload types, and the server client. Compiled into both targets. |
| `Tome/` | Container app: in-app browser and settings. |
| `TomeShare/` | Share Extension. Self-contained — it uploads directly. |
| `Scripts/` | Bundle builder and the iOS-only JS glue. |
| `Resources/Generated/` | Build output. Gitignored. |
