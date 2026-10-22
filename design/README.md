# Gnomon: design set

Working name **Gnomon** ("your day, in light and shade"). Sample "today" in every mock is Tuesday 13 October 2026, 09:52. All people, hosts, places and counts are sample content.

## Artifacts

| # | Artifact | URL | Contains |
|---|---|---|---|
| 1 | IA and Wireframes | https://claude.ai/artifact/WwK2PNeqLUhCtTwaGMLh2V | User goals, navigation options and recommendation, **Design direction** (name, type, colour light and dark, shape, icons, motion, voice, two signature elements, "learned vs how we differ" table), screen inventory, story-to-screen map, content hierarchy, 11 flows, 40 wireframes, state coverage, references, open questions |
| 2 | Hi-fi 1: Connect and accounts | https://claude.ai/artifact/23ph9zKEzRKoDUXQ1yViJy | Onboarding and connect accounts (A1 to A7), accounts and errors (H1 to H7) |
| 3 | Hi-fi 2: Agenda and day | https://claude.ai/artifact/AeVf9bLpy7ZZgA89LA21Ma | Agenda and Up next (B1 to B7), day, 3 day, week, drag, resize, rollback (C1 to C7) |
| 4 | Hi-fi 3: Events | https://claude.ai/artifact/XzRwrGxv5F3EmGVjHRsV18 | Event detail and actions (D1 to D9), create and edit (E1 to E9), RSVP (F1 to F4) |
| 5 | Hi-fi 4: Settings, alfather, widgets | https://claude.ai/artifact/5mheqLUtZRgA6Qb9WLKNPB | Calendars and settings (G1 to G8), quick add and free time with alfather (I1 to I9), widgets, lock screen, notifications (J1 to J5), offline and sync (K1 to K6) |

Every hi-fi frame is shown in light and dark with a toggle. Each carries notes and the user stories it covers. The artifacts are private to the owner until shared.

Sources are in `design/src` (`lib.mjs` design system, `hifi-*.mjs`, `wire.mjs`, `ia.mjs`). Rebuild with `node src/build.mjs hifi-1 hifi-2 hifi-3 hifi-4 ia`. Built HTML is in `design/*.html`.

## Recommendation in one paragraph

No tab bar. One Calendar surface with a view switcher (Agenda, Day, 3 days, Week), an avatar for Settings and a dock with an alfather line and a plus. Event detail, forms, RSVP, calendars and accounts arrive as pushes and sheets. The hero is Up next, drawn as a dial that counts down to the next event and drives the widget, lock screen and reminder. Past time is hatched shade; now is a brass line.

## Open design questions

1. Name: is "Gnomon" and a serif wordmark right?
2. Offline writes: queue and sync later, or disable (as designed in K2)?
3. Search is not in the plan and not designed.
4. Is the 10 minute Join window fixed (as designed) or a setting?
5. Week view in portrait is read first; drag needs a long press. Accept, or landscape only for drag?
6. Only per calendar colours are used; provider per event colours are ignored.
7. Agenda paging beyond the 14 days in US-3.2 (assumed 14 day chunks).

## Conflicts with, and gaps in, PLAN.md

- **No story for pairing the app with the hub.** US-7.1 covers tokens for scripts only. The app needs a device token and a pairing screen (A2).
- **Read-only ICS fallback** appears in the risks table but has no story. A7 offers it.
- **Working hours** are needed by US-7.4 but nothing lets the user set them. Added G5b.
- **US-2.4** places server status on the server. The app also shows it (K4), as the observability NFR implies.
- **US-3.2** loads 14 days; infinite scroll needs paging.
- **US-6.3** drag and drop in a week view is cramped in portrait. The 3 day view is the main drag surface.
- **US-6.1 and US-6.4** (widgets, Live Activity) need a native target, which is extra work on Expo.
- **No mobile UI** for US-0.2, 0.3, 0.4, 7.5, 8.1, 8.2. US-7.6 is only represented by "alfather" labels, as briefed.

## References

Read: both Alfather flow canvases (A3, A4), benji.org, Honkish, Family Values, distill.id. The shop design canvas was read as a file list plus one screen's source. The reader design canvas was only skimmed (file list and index). Nothing from these was reproduced: no palette, typeface, layout or motif.
