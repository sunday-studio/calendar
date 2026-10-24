# Assets

Images used by the project README. Nothing here is hand-edited.

| Path | What | How it is made |
|---|---|---|
| `icon.png` | App icon, 512 x 512 | `scripts/screenshots.sh` exports it from `ios/Calendar/Resources/Assets.xcassets` (the icon itself comes from `scripts/make-icons.swift`) |
| `hero.png`, `hero-light.png`, `hero-dark.png` | Three phones side by side | composed by `scripts/screenshots.sh` from the screenshots |
| `screenshots/<screen>-<light or dark>.png` | The real app on an iPhone 17 Pro class simulator, about 540 px wide | `scripts/screenshots.sh` (an XCUITest tour, `ios/CalendarUITests/ScreenshotTourUITests.swift`, against a throwaway hub with the rich demo data) |
| `design/*.png` | Widget, Live Activity and notification mocks from the design canvas boards (`w2-*`) | rendered once from the canvas HTML; mocks, not device captures. Widgets and notifications cannot be captured reliably in the simulator |

The screenshots show the throwaway hub on a loopback address and demo accounts only (`*@example.com` data);
no real address, token or account appears. To regenerate: `scripts/screenshots.sh` (needs Xcode, XcodeGen and a few
minutes; it creates its own simulator "Calendar Shots" and stops everything it started).
