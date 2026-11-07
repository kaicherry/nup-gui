# nup-gui

nup-gui is a desktop workbench for creating and working with NUP packages. It uses a Go backend and a TypeScript/Vite frontend, packaged with Wails v2.16.0.

## Pack and Archive workflows

**Pack** creates a package from a source directory and an output file. Enter the title ID, display version, publisher, and name; you can also provide an optional PNG or JPEG package icon. Packing shows actual progress and can be cancelled.

**Archive** opens one NUP package and keeps it selected while you use its actions:

- **Info** shows a package summary.
- **List** shows entries and sizes, with raw, zstd, or empty tags.
- **Extract** extracts selected package contents, optionally limited to a subtree.
- **Edit** updates the name, publisher, display version, or icon; fields with clear options can also be cleared. You can preview or replace the embedded package icon.
- **Verify** reports the integrity result.

## Prerequisites and development/build

Install Go 1.25 or later, Node.js/npm, and the Wails CLI v2. Follow the [Wails installation guide](https://wails.io/docs/gettingstarted/installation/) for platform prerequisites.

From the repository root:

```sh
wails dev
wails build -platform darwin/arm64
wails build -platform windows/amd64
(cd gonup && go test ./...)
```

Wails runs the frontend build as part of `wails build`. The single-target outputs are `build/bin/nup-gui.app` and `build/bin/nup-gui.exe`.

The Go dependency is included in this repository as the `gonup` nested module, and the root `go.mod` replaces `github.com/kaicherry/nup` with `./gonup`. A fresh clone does not need a sibling checkout.

For Wails macOS packaging, `build/appicon.png` must contain actual PNG bytes. Windows uses `build/windows/icon.ico`.

## macOS signing, notarization, and DMG distribution

Wails' macOS build is ad-hoc signed. Distribution outside the App Store requires a **Developer ID Application** identity; Apple Development is not a distribution certificate. In your local shell, set `IDENTITY` to your Developer ID Application identity and `PROFILE` to the name of a locally stored `notarytool` Keychain profile. Create the profile locally with `xcrun notarytool store-credentials "$PROFILE"`. Apple ID app-specific password and Team ID, or API key authentication, must never be placed in the repository.

Sign and notarize the app, then staple and validate its ticket:

```sh
APP=build/bin/nup-gui.app
codesign --force --options runtime --timestamp --sign "$IDENTITY" "$APP"
ditto -c -k --sequesterRsrc --keepParent "$APP" build/bin/nup-gui-app.zip
xcrun notarytool submit build/bin/nup-gui-app.zip --keychain-profile "$PROFILE" --wait
xcrun stapler staple "$APP"
xcrun stapler validate "$APP"
```

Stage the signed app with an `/Applications` link, create and sign the DMG, then notarize and staple it:

```sh
STAGE="$(mktemp -d)"
ditto "$APP" "$STAGE/nup-gui.app"
ln -s /Applications "$STAGE/Applications"
DMG=build/bin/nup-gui.dmg
hdiutil create -volname "nup-gui" -srcfolder "$STAGE" -ov -format UDZO "$DMG"
codesign --force --timestamp --sign "$IDENTITY" "$DMG"
xcrun notarytool submit "$DMG" --keychain-profile "$PROFILE" --wait
xcrun stapler staple "$DMG"
xcrun stapler validate "$DMG"
```

Verify the app and disk image:

```sh
codesign --verify --deep --strict "$APP"
spctl --assess --type execute "$APP"
xcrun stapler validate "$APP"
spctl --assess --type open --context context:primary-signature "$DMG"
hdiutil verify "$DMG"
xcrun stapler validate "$DMG"
```

## Windows signing

The Windows `.exe` is not Authenticode-signed by the Apple certificate. Trusted Windows code signing requires a separate Windows Authenticode certificate.
