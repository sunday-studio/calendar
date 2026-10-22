import fs from 'fs';
import { page, ico, ICONS, LIGHT, DARK, dial, daylight, upNext, ev, allDay, timeline, boardCard } from './lib.mjs';
import { mark } from './hifi-1.mjs';
import { WCSS, SCREENS, wfCard, flow, w } from './wire.mjs';

const URLS = JSON.parse(fs.readFileSync(new URL('./urls.json', import.meta.url), 'utf8'));
const H = { 1: URLS.hifi1, 2: URLS.hifi2, 3: URLS.hifi3, 4: URLS.hifi4 };
const hf = (id) => ({ A: 1, H: 1, B: 2, C: 2, D: 3, E: 3, F: 3, G: 4, I: 4, J: 4, K: 4 })[id[0]];
const link = (id) => `<a href="${H[hf(id)]}#${id}" target="_blank" rel="noopener" class="mono">${id}</a>`;
const sec = (id, eyebrow, h2, p, body) => `<section class="fl2" id="${id}"><header><span class="eyebrow">${eyebrow}</span><h2>${h2}</h2>${p ? `<p>${p}</p>` : ''}</header>${body}</section>`;
const tbl = (head, rows, cls = '') => `<div class="tw"><table class="t ${cls}"><thead><tr>${head.map((h) => `<th>${h}</th>`).join('')}</tr></thead><tbody>${rows.map((r) => `<tr>${r.map((c) => `<td>${c}</td>`).join('')}</tr>`).join('')}</tbody></table></div>`;
const themed = (inner, wd = 560) => `<div class="pair bpair" style="margin-top:20px">${['light', 'dark'].map((t) => `<div class="bd ${t}" data-t="${t}" style="max-width:${wd}px">${inner}</div>`).join('')}</div>`;
const us = (a) => a.map((x) => `<span class="usc">${x}</span>`).join(' ');

const COUNTS = (() => { const frames = ['hifi-1', 'hifi-2', 'hifi-3', 'hifi-4'].map((f) => fs.readFileSync(new globalThis.URL(`./${f}.mjs`, import.meta.url), 'utf8')).join('').match(/(phoneCard|boardCard)\(\{ id:/g).length; return { frames, screens: 0, stories: '' }; })();
// ------------ 1. summary ------------
const summary = sec('sum', 'Recommendation', 'One surface, many sheets', 'The calendar is a single surface that changes zoom, not a set of tabs. Everything else arrives as a sheet or a push and leaves again.', `
<div class="two"><div>
<ul class="pts">
<li><b>Navigation.</b> No tab bar. The Calendar surface has a view switcher (Agenda, Day, 3 days, Week), an avatar that opens Settings, and one dock at the bottom: a line for <span class="af">${ico('spark', 13)}alfather</span> and a plus. Detail, edit, RSVP, calendars and accounts are pushed or presented over it.</li>
<li><b>Why.</b> Agenda, Day, 3 days and Week are one dataset at four zooms, not four places. The other destinations are rare. A tab bar would spend permanent chrome on screens opened once a month.</li>
<li><b>Hero.</b> Up next, drawn as a dial. It is the answer to the question the app is opened for, and it also drives the widget, the lock screen and the reminder.</li>
<li><b>Identity.</b> Gnomon: a calendar that treats the day as light and shade. Past time is shaded, the present is a brass line, what is next is lit.</li>
</ul></div>
<div class="kv">
${[['@@SCREENS@@', 'screens in the inventory'], [String(COUNTS.frames), 'hi-fi frames, each in light and dark'], ['11', 'key flows drawn as wireframes'], ['@@STORIES@@', 'user stories with a mobile screen. The rest are server, setup or process work']].map(([a, b]) => `<div><span class="serif">${a}</span><span>${b}</span></div>`).join('')}
</div></div>
<p class="mut" style="margin-top:18px;max-width:70ch">Hi-fi mockups: <a href="${URLS.hifi1}" target="_blank" rel="noopener">1 Connect and accounts</a> · <a href="${URLS.hifi2}" target="_blank" rel="noopener">2 Agenda, day and week</a> · <a href="${URLS.hifi3}" target="_blank" rel="noopener">3 Events</a> · <a href="${URLS.hifi4}" target="_blank" rel="noopener">4 Settings, alfather, widgets, offline</a>. All people, places and times are sample content. Sample today is Tuesday 13 October 2026, 09:52.</p>`);

// ------------ 2. goals ------------
const goals = sec('goals', 'User goals', 'What the owner opens the app for', 'One person, several accounts, a home server and a taste for owning the tooling. The list is ordered by how often each job happens.', tbl(['Goal', 'How often', 'Where it lives', 'Stories'], [
  ['Know what is next and join it', 'Many times a day', 'Up next card, widget, lock screen, reminder with Join', us(['US-3.1', 'US-5.6', 'US-6.1', 'US-6.2', 'US-6.4'])],
  ['See my day, week and gaps', 'Daily', 'Agenda, Day, 3 days, Week; second time zone', us(['US-3.2', 'US-3.3', 'US-6.5'])],
  ['Capture an event fast', 'Daily', 'Dock: quick add by sentence, or the form', us(['US-5.1', 'US-7.3'])],
  ['Answer an invitation', 'Daily', 'Respond tag in the agenda, reply sheet', us(['US-5.4'])],
  ['Move or change something', 'Weekly', 'Drag and resize, edit form, move to calendar', us(['US-5.2', 'US-5.7', 'US-6.3', 'US-5.3'])],
  ['Find time for something', 'Weekly', 'Free time through alfather', us(['US-7.4'])],
  ['Trust that it is current', 'Always', 'Sync dot, offline banner, server status', us(['US-2.3', 'US-2.4', 'US-3.5', 'US-7.2'])],
  ['Choose what I see and how it looks', 'Rarely', 'Calendars, colours, default calendar', us(['US-4.1', 'US-4.2', 'US-4.3'])],
  ['Connect and maintain accounts', 'Rarely, but urgent when broken', 'Onboarding, Accounts, banners', us(['US-1.1', 'US-1.2', 'US-1.3', 'US-1.4', 'US-1.5'])],
  ['Let scripts in', 'Rarely', 'API tokens', us(['US-7.1'])],
]));

// ------------ 3. navigation ------------
const optA = `<div class="wf">${w.bar('', 'Agenda')}${w.row()}${w.row()}${w.row()}${w.row()}${w.row()}<div class="wdock" style="justify-content:space-around;height:36px"><span>Agenda</span><span>Day</span><span>Week</span><span>Cals</span><span>⚙</span></div></div>`;
const optB = `<div class="wf">${SCREENS.find((s) => s.id === 'B1').wire}</div>`;
const optC = `<div class="wf">${w.bar('', 'Gnomon')}${w.box(60, '“Standup in 8 min”', 'f')}${w.box(40, 'reply card')}${w.box(60, 'agenda card')}<div class="sp1"></div>${w.dock().replace('✦ add with alfather', 'Ask or add…')}</div>`;
const navopt = (cls, t, d, wf) => `<div class="wfc ${cls}">${wf}<h3>${t}</h3><p style="font-size:13.5px;color:var(--ink2)">${d}</p></div>`;
const dots = (n) => '●'.repeat(n) + '○'.repeat(3 - n);
const nav = sec('nav', 'Navigation model', 'Three options, one recommendation', 'Scored against how the app is used: fast glances, quick capture, and rare visits to settings.', `
<div class="wgal">${navopt('', 'A · Tab bar', 'Agenda, Day, Week, Calendars, Settings as tabs.', optA)}${navopt('rec', 'B · One surface, sheets (recommended)', 'Four zooms of one dataset, a dock, everything else as sheets and pushes.', optB)}${navopt('', 'C · Composer first', 'A single stream you talk to. Calendar views are results.', optC)}</div>
${tbl(['Criterion', 'A · Tabs', 'B · Surface + sheets', 'C · Composer first'], [
  ['Glance at what is next', dots(2), dots(3), dots(1)],
  ['Capture in two taps', dots(2), dots(3), dots(3)],
  ['Fits “four zooms of one thing”', dots(1), dots(3), dots(1)],
  ['Permanent chrome', 'Heavy: 5 tab slots', 'Light: one dock', 'Light'],
  ['Works when alfather is down', dots(3), dots(3), dots(1)],
  ['Reaches rare screens', 'One tap', 'Two taps (avatar)', 'Via the composer, which is ambiguous'],
  ['Risk', 'Reads as a generic app', 'Settings feel buried. Mitigated by the sync dot and banners that link straight to the right screen', 'Depends on AI for core use, which the plan rules out (a deterministic fallback is required)'],
])}
<p class="lede" style="margin-top:22px;font-size:16px">B wins because the main job is looking at time at different zooms, and because the AI has to be an extra, not the way in. The dock keeps quick add a thumb away. Sheets give every secondary task a clear way back, so there is no stack of screens to remember.</p>
<h3 class="h3">Navigation map</h3>
<div class="nmap">
<div><span class="cap">Surface</span><div class="nn big">Calendar<br><small>Agenda · Day · 3 days · Week</small><br><small>Up next · daylight strip · dock</small></div></div>
<div><span class="cap">Opens from the surface</span><div class="nn">Event detail <small>push, tap a row</small></div><div class="nn">New event <small>modal, dock +</small></div><div class="nn">Quick add <small>sheet, dock line</small></div><div class="nn">Jump to date <small>sheet, tap title</small></div><div class="nn">Settings <small>push, avatar</small></div><div class="nn">Server and sync <small>push, sync dot</small></div></div>
<div><span class="cap">Opens from detail or the form</span><div class="nn">Actions <small>sheet</small></div><div class="nn">Reply <small>sheet</small></div><div class="nn">Delete scope <small>sheet</small></div><div class="nn">Edit form <small>modal</small></div><div class="nn">Repeat scope · calendar · video call <small>sheets</small></div></div>
<div><span class="cap">Opens from Settings</span><div class="nn">Calendars → colour</div><div class="nn">Accounts → detail → reconnect, remove</div><div class="nn">Add account → provider sheet</div><div class="nn">Time zones · Working hours · Notifications</div><div class="nn">API tokens → create → reveal</div></div>
</div>
<p class="mut" style="margin-top:10px;font-size:13.5px">Outside the app: widget and lock screen tap into Event detail. A reminder’s Join action leaves for Teams, Meet or the browser.</p>`);

// ------------ 4. design direction ------------
const swatch = (n, t, d, map) => { const m = map.match(new RegExp(`--${t}:(#[0-9A-Fa-f]{6})`)); return `<div class="sw2"><i style="background:var(--${t})"></i><span><b>${n}</b><small class="mono">${m ? m[1] : ''}</small><small>${d}</small></span></div>`; };
const PAL = [['Chalk', 'bg', 'Ground'], ['Paper', 'surface', 'Cards and fields'], ['Sunk', 'sunk', 'Segments, quiet fills'], ['Line', 'line', 'Hairlines'], ['Ink', 'ink', 'Text, primary button'], ['Ink 2', 'ink2', 'Secondary text'], ['Brass', 'brass', 'Now, Join, the dial'], ['Brass ink', 'brassInk', 'Brass used as text'], ['Madder', 'madder', 'Errors, delete'], ['Verdigris', 'verd', 'In sync, success'], ['Inverse', 'inv', 'The Up next card']];
const PIG = [['Cobalt', 'cobalt', 'Personal'], ['Mauve', 'mauve', 'Side project'], ['Clay', 'clay', 'Work'], ['Ochre', 'ochre', 'Team'], ['Verdigris', 'verdigris', 'Home'], ['Rose', 'rose', 'Family'], ['Moss', 'moss', 'Spare'], ['Slate', 'slate', 'Holidays']];
const palBoard = (map) => `<div class="cap" style="margin-bottom:10px">Core</div><div class="swg">${PAL.map(([n, t, d]) => swatch(n, t, d, map)).join('')}</div><div class="cap" style="margin:18px 0 10px">Pigments for calendars</div><div class="swg">${PIG.map(([n, t, d]) => swatch(n, t, d, map)).join('')}</div>`;
const dialSvg = (m, l, b, s = 84) => `<div style="width:${s}px;height:${s}px;color:var(--ink)">${dial(m, l, b).replace('class="dial"', `class="dial" width="${s}" height="${s}"`)}</div>`;
const DL = [{ s: '08:30', e: '09:15', c: 'cobalt' }, { s: '10:00', e: '10:30', c: 'clay' }, { s: '11:00', e: '12:00', c: 'clay' }, { s: '13:00', e: '14:00', c: 'cobalt' }, { s: '14:30', e: '15:30', c: 'mauve' }, { s: '16:00', e: '17:00', c: 'verdigris' }, { s: '19:30', e: '21:00', c: 'rose' }];
const sig = `<div class="cap" style="margin-bottom:12px">The dial: minutes until the next event, over a 60 minute window</div><div class="row" style="gap:16px;flex-wrap:wrap">${[[60, 'min', '1h'], [30, 'min'], [8, 'min'], [0, 'now', '●']].map(([m, l, b]) => `<div style="text-align:center">${dialSvg(m, l, b)}<div class="mono mut" style="font-size:11px;margin-top:4px">${m === 60 ? '≥ 1 h' : m === 0 ? 'in progress, then rest' : m + ' min'}</div></div>`).join('')}</div>
<div class="cap" style="margin:22px 0 4px">Light and shade: the daylight strip at three moments</div>${['07:30', '13:10', '20:40'].map((n) => `<div style="margin:0 -20px 0"><div class="mono mut" style="font-size:11px;padding:6px 20px 0">${n}</div>${daylight(n, DL)}</div>`).join('')}`;
const shadeDemo = `<div class="cap" style="margin-bottom:6px">Elapsed time is hatched, the line is now</div><div style="position:relative;height:210px;overflow:hidden;border-radius:14px">${timeline({ top: 4, h0: 9, h1: 13, hh: 52, cols: [[{ s: '09:00', e: '09:45', t: 'Run with Kofi', c: 'cobalt', state: 'past' }, { s: '10:00', e: '10:30', t: 'Standup', c: 'clay' }, { s: '11:00', e: '12:00', t: 'Roadmap review', c: 'clay' }]], now: '09:52' })}</div>`;

const icoGrid = `<div class="icg">${ICONS.map((n) => `<span title="${n}">${ico(n, 22)}<small>${n}</small></span>`).join('')}</div>`;
const shapeBoard = `<div style="display:grid;gap:14px">
<div class="row" style="gap:10px;flex-wrap:wrap"><span class="btn pri sm" style="width:auto">Primary</span><span class="btn brass sm" style="width:auto">${ico('video', 16)}Join</span><span class="btn sm" style="width:auto">Secondary</span><span class="btn dng sm" style="width:auto">Delete</span></div>
<div class="row" style="gap:10px;flex-wrap:wrap"><span class="chip">Chip, radius 10</span><span class="chip br">Respond</span><span class="sw on"></span><span class="sw"></span><span class="in" style="height:44px;width:160px"><span class="ph2">Field, 14</span></span></div>
<div style="position:relative;height:150px;border-radius:14px;background:var(--bg);overflow:hidden"><div class="sheet" style="position:absolute;padding:20px;min-height:110px;bottom:-30px"><div class="sh1" style="font-size:20px">Sheet, radius 30, with a notch</div></div></div>
<div class="row mut sm13" style="gap:14px;flex-wrap:wrap"><span>Radii: 10 chips, 14 controls, 18 groups, 24 cards, 30 sheets</span></div></div>`;

const motionRows = [
  ['Settle ease', 'cubic-bezier(.2,.8,.2,1). Starts quickly, lands softly, never overshoots.', 'Sheets 360 ms · pushes 280 ms · fades 160 ms'],
  ['One shared element', 'The row’s title and time travel into Event detail. Nothing already on screen is duplicated during a transition.', 'Follows the “fluidity” principle'],
  ['The shade advances', 'The daylight strip and the grid shade move once a minute, with a slow 600 ms ease.', 'Time passing is quiet'],
  ['The dial sweeps', 'It redraws once on pull to refresh and once when the card changes. It never loops.', 'Reduced motion: no sweep'],
  ['Snaps tick', 'Dragging in the grid ticks a light haptic at each 15 minute snap. Dropping gives one soft tap; a failed write gives a double tap.', 'Haptics only on touch'],
  ['Arrival', 'Rows rise 8 px and fade in, 24 ms apart, at most 5 rows.', 'Reduced motion: fade only'],
];
const voiceRows = [
  ['Plain, dry, a little warm', 'Standup is in 8 minutes.', 'Heads up! Your meeting is about to start!'],
  ['Time words over timestamps', 'Started 12 min ago · ends 10:30', '2026-10-13T10:00:00+01:00'],
  ['Errors say what, what is safe, what next', 'Google didn’t save this event. Nothing was saved. Your edits are still here.', 'Oops, something went wrong.'],
  ['Controls name the effect', 'Delete and notify 5 people', 'OK'],
  ['alfather is a labelled helper', 'alfather is reading it', 'AI magic'],
  ['No exclamation marks, no emoji', 'Replied Maybe to Yaw', 'Reply sent! 🎉'],
];
const REFROWS = [
  ['Alfather flow canvases', 'Few type sizes, one hero per screen, tints and shadows in place of borders, quiet chrome around rich content. Every screen is scored against written rules.', 'We keep the discipline, not the look. Our rules: one hero per screen, a serif for time, hairlines allowed for groups, texture (hatch) for state. Palette, faces and shapes are our own.'],
  ['Shop and reader design canvases', 'One artboard per state: empty, loading, offline, failed. Rows of fixed height, consistent gutters, sheets that rise, honest error screens.', 'We adopt the state-per-frame method and the 16 to 20 px gutters. Our sheets settle rather than spring, and carry a notch instead of a grab bar.'],
  ['benji.org · Family Values', 'Simplicity (basics first, the rest when relevant), fluidity (elements travel between screens, no static transitions, nothing duplicated), selective delight (more on frequent actions).', 'We use the continuity rule and the delight curve. Our delight is the dial and the shade, not bounce. Motion has no overshoot.'],
  ['benji.org · Honkish', 'Presence made visible, countdown borders for urgency, colour that signals the meaning of an action, deliberate friction where it protects the user.', 'The dial is our countdown. Madder tints mark destructive sheets. Friction is spent on organiser deletes and cross account moves.'],
  ['distill.id', 'Calm, sparse, confident copy. Agents as helpers that tidy things in the background. Works offline and says it is private.', 'Our voice is dry and short. alfather is a labelled helper. Offline states say how old the data is.'],
];
const direction = sec('direction', 'Design direction', 'Gnomon: your day, in light and shade', 'The identity is built for a person who looks at time all day: warm, analogue, exact. The references taught craft. The look is ours.', `
<div class="two" style="margin-top:22px"><div>
<div class="bd" style="max-width:none" data-t="light"><div class="row" style="gap:18px;align-items:center">${mark(76)}<div><div class="serif" style="font-size:44px;line-height:1">Gnomon</div><div class="mut" style="margin-top:6px">The part of a sundial that casts the shadow.</div></div></div></div>
<h3 class="h3">Name and mood</h3>
<p class="mut" style="max-width:62ch">Gnomon is the working name. Mood: a well made instrument that sits on a desk. Matte paper, deep green ink, one touch of brass. Unhurried, exact, private. The wordmark is a serif; the mark is a dial with a brass gnomon throwing a pale shadow.</p>
<h3 class="h3">Type pairing</h3>
<div class="specs"><div><span class="serif" style="font-size:54px;line-height:1">10:00</span><small class="mono">Newsreader 400. Display, times, dates, titles. Lining tabular numerals, italic for relative time.</small></div>
<div><span style="font-size:22px;font-weight:500">Roadmap review</span><small class="mono">Schibsted Grotesk 400, 500, 600. All interface text. Warm, newspaper bred, very legible small.</small></div>
<div><span class="mono" style="font-size:15px">11:00–12:00 · 1 h · BST</span><small class="mono">DM Mono 400, 500. Durations, counts, tokens, ids.</small></div></div>
${tbl(['Role', 'Face', 'Size / leading'], [['Hero', 'Newsreader 400', '34 to 46 / 1.05'], ['Screen title', 'Newsreader 400', '32 / 1.05'], ['Section, day header', 'Newsreader 400', '20 / 1.2'], ['Row title', 'Schibsted 500', '16 / 1.25'], ['Body', 'Schibsted 400', '15 / 1.35'], ['Caption', 'Schibsted 500, caps, +6%', '12 / 1.3'], ['Time, data', 'DM Mono 400', '11 to 13']])}
</div><div>
<h3 class="h3" style="margin-top:0">Colour system, light and dark</h3>
<p class="mut" style="max-width:60ch">Two grounds with a green cast, so neutrals are never grey. Brass is the only accent. Dark mode is “Night”: the same hues, lifted for contrast, with brass kept as the one warm thing.</p>
<div class="pair bpair" style="margin-top:14px"><div class="bd light" data-t="light" style="max-width:380px">${palBoard(LIGHT)}</div><div class="bd dark" data-t="dark" style="max-width:380px">${palBoard(DARK)}</div></div>
<p class="mut sm" style="margin-top:12px;font-size:13px;max-width:60ch">Measured contrast: ink on chalk 14.4 : 1 (dark 15.8). Ink 2 on chalk 5.5 : 1 (dark 7.5). Brass ink on chalk 5.7 : 1. Madder on its tint 5.2 : 1. Text on the brass button 7.3 : 1 (dark 8.7). Calendar colours are never the only signal: RSVP and state also use form (hatch, strike, outline).</p>
</div></div>
<div class="two" style="margin-top:34px"><div>
<h3 class="h3" style="margin-top:0">Signature 1 · The dial</h3>
<p class="mut" style="max-width:60ch">Up next is an inverse card with a dial. The brass arc is the part of the next hour still to come. At 10 minutes the Join button turns brass. During the event the dial counts minutes left. At the end of the day it rests. The same dial appears in the widget, the lock screen Live Activity and the notification.</p>
${themed(sig, 420)}</div><div>
<h3 class="h3" style="margin-top:0">Signature 2 · Light and shade</h3>
<p class="mut" style="max-width:60ch">The day has a lit part and a shaded part. In every view, time already gone is hatched and slightly dimmed, the brass line is now, and what is ahead is open paper. Events that are over fade and go hollow. You read the state of the day before reading a word.</p>
${themed(shadeDemo, 420)}</div></div>
<div class="two" style="margin-top:34px"><div>
<h3 class="h3" style="margin-top:0">Shape language</h3><p class="mut" style="max-width:60ch">Soft rectangles, not pills: the app has no capsule buttons. Switches are rounded squares. Sheets have a small triangular notch where other apps put a grab bar, like the point of a gnomon. States use texture: hatch for maybe and for free time, strike for declined, hollow for past.</p>
${themed(shapeBoard, 420)}</div><div>
<h3 class="h3" style="margin-top:0">Iconography</h3><p class="mut" style="max-width:60ch">Line icons on a 24 grid, 1.75 stroke, round caps and joins, flat terminals and generous counters. One icon per meaning. The only filled mark is the brass dot for live and now. alfather always carries the four point spark.</p>
<div class="bd" data-t="light" style="max-width:none;margin-top:20px">${icoGrid}</div></div></div>
<div class="two" style="margin-top:34px"><div>
<h3 class="h3" style="margin-top:0">Motion principles</h3>${tbl(['Principle', 'Spec', 'Note'], motionRows)}</div><div>
<h3 class="h3" style="margin-top:0">Voice and microcopy</h3>${tbl(['Rule', 'We write', 'We avoid'], voiceRows)}</div></div>
<h3 class="h3" style="margin-top:34px">What we learned from the references, and how we differ</h3>${tbl(['Reference', 'What we learned', 'How we differ'], REFROWS)}`);

// ------------ 5. inventory ------------
const INV = [
  ['A1', 'Welcome', 'Full screen', 'First launch, no accounts', 'A1'], ['A2', 'Pair with server', 'Push', 'A1, Settings', 'A2, A2b'], ['A3', 'Add an account', 'Sheet', 'A1, Accounts, empty agenda', 'A3'], ['A4', 'Provider sign in', 'System sheet', 'A3', 'A4, A4b'], ['A5', 'Connected', 'Push', 'A4', 'A5'], ['A6', 'iCloud app password', 'Push', 'A3, H3', 'A6, A6c, A6b, H6'], ['A7', 'Admin approval', 'Push', 'A4', 'A7'],
  ['B1', 'Agenda and Up next', 'Surface', 'App start, view switcher', 'B1, B2, B3, B4, B5, B6, H2, K1, K5, K6'], ['B7', 'Jump to date', 'Sheet', 'Title tap', 'B7'], ['C1', 'Day', 'Surface', 'View switcher', 'C1, C2'], ['C3', '3 days', 'Surface', 'View switcher', 'C3, C5, C6, C7'], ['C4', 'Week', 'Surface', 'View switcher', 'C4'],
  ['D1', 'Event detail', 'Push', 'Row, widget, notification', 'D1, D2, D3, D4'], ['D5', 'Actions', 'Sheet', 'Detail ⋯', 'D5'], ['D6', 'Move to calendar', 'Sheet', 'Actions', 'D6'], ['D7', 'Delete', 'Sheet', 'Actions', 'D7, D8, D9'],
  ['E1', 'Create and edit', 'Modal', 'Dock +, detail ✎', 'E1, E2, E7, E8, E9'], ['E3', 'Date and time picker', 'Inline', 'E1', 'E3'], ['E4', 'Choose calendar', 'Sheet', 'E1', 'E4'], ['E5', 'Repeat scope', 'Sheet', 'E1 Save', 'E5'], ['E6', 'Add video call', 'Sheet', 'E1', 'E6'], ['F1', 'Reply to invitation', 'Sheet', 'Respond tag, detail', 'F1, F2, F3, F4'],
  ['G1', 'Settings', 'Push', 'Avatar', 'G1'], ['G2', 'Calendars', 'Push', 'G1', 'G2'], ['G3', 'Calendar colour', 'Push', 'G2', 'G3'], ['G4', 'Time zones', 'Push', 'G1', 'G4'], ['G5', 'Notifications', 'Push', 'G1', 'G5'], ['G5b', 'Working hours', 'Push', 'G1', 'G5b'], ['G6', 'API tokens', 'Push', 'G1', 'G6, G7, G8'],
  ['H1', 'Accounts', 'Push', 'G1, banner', 'H1, H3, H3b, H4, H5, H7'], ['I1', 'Quick add', 'Sheet', 'Dock', 'I1, I2, I3, I4, I5, I6'], ['I7', 'Free time', 'Sheet + overlay', 'I1', 'I7, I8, I9'],
  ['J1', 'Widgets', 'System', 'Home screen', 'J1, J2'], ['J3', 'Lock screen', 'System', '30 min before', 'J3'], ['J4', 'Notifications', 'System', 'Before events', 'J4, J5'], ['K4', 'Server and sync', 'Push', 'Sync dot, Settings', 'K3, K4'], ['K2', 'Offline write', 'Sheet', 'Any write while offline', 'K2'],
];
const inventory = sec('inv', 'Screen inventory', `${INV.length} screens, grouped by flow`, 'Kind says how it arrives. The last column lists the hi-fi frames, including every state.', tbl(['ID', 'Screen', 'Kind', 'Reached from', 'Hi-fi frames'], INV.map(([id, n, k, f, fr]) => [`${link(id.replace('G5b', 'G5b'))}`, n, k, f, fr.split(', ').map((x) => link(x)).join(' ')])));

// ------------ 6. story map ------------
const ST = [
  ['US-0.1', 'Reach my server securely', 'A2, A2b, K5'], ['US-0.2', 'Register my own Google app', '—'], ['US-0.3', 'Register my own Microsoft app', '—'], ['US-0.4', 'Skeleton projects', '—'],
  ['US-1.1', 'Connect a Google account', 'A3, A4, A4b, A5'], ['US-1.2', 'Connect multiple Google accounts', 'A3, A5, H1'], ['US-1.3', 'Connect a Microsoft account', 'A3, A4, A7'], ['US-1.4', 'Connect iCloud', 'A3, A6, A6b, A6c'], ['US-1.5', 'Manage connected accounts', 'H1 to H6, G1'],
  ['US-2.1', 'Mirror my calendar list', 'A5, G2, E4'], ['US-2.2', 'Initial full sync', 'A5'], ['US-2.3', 'Stay fresh', 'K3, K6'], ['US-2.4', 'Resilient sync', 'K3, K4, H7'],
  ['US-3.1', 'Up next', 'B1, B2, B3'], ['US-3.2', 'Merged agenda', 'B1, B4, B5, B6'], ['US-3.3', 'Day view', 'C1, C2, B7'], ['US-3.4', 'Event details', 'D1 to D4'], ['US-3.5', 'Offline glance', 'K1, K2, K5, B5'],
  ['US-4.1', 'Show or hide calendars', 'G2, A5'], ['US-4.2', 'Custom colours', 'G3'], ['US-4.3', 'Default calendar', 'G2, G3, E1'],
  ['US-5.1', 'Create an event', 'E1 to E4, E7, E8'], ['US-5.2', 'Edit an event', 'E5, E9, D2'], ['US-5.3', 'Delete an event', 'D7, D8, D9'], ['US-5.4', 'RSVP', 'F1 to F4, D3'], ['US-5.5', 'Add a video link', 'E2, E6'], ['US-5.6', 'Join a meeting', 'B1, D1, D4, J3, J4'], ['US-5.7', 'Duplicate or move', 'D5, D6'],
  ['US-6.1', 'Home-screen widget', 'J1, J2'], ['US-6.2', 'Reminders', 'G5, J4, J5'], ['US-6.3', 'Week and 3-day, drag and drop', 'C3 to C7'], ['US-6.4', 'Next meeting on lock screen', 'J3, G5'], ['US-6.5', 'Multiple time zones', 'C2, G4, D4'],
  ['US-7.1', 'API access for my scripts', 'G6, G7, G8, A2'], ['US-7.2', 'Live updates', 'K6, K3'], ['US-7.3', 'Quick add (alfather)', 'I1 to I6'], ['US-7.4', 'Find a free slot (alfather)', 'I7, I8, I9, G5b'], ['US-7.5', 'Automations', '—'], ['US-7.6', 'Agent access via alfather', '— (labels only)'],
  ['US-8.1', 'Parity checklist', '—'], ['US-8.2', 'Remove Notion’s access', '—'],
];
const noui = { 'US-0.2': 'Provider setup, no app UI', 'US-0.3': 'Provider setup, no app UI', 'US-0.4': 'Engineering', 'US-7.5': 'Server rules, no app UI in v1', 'US-7.6': 'Plumbing is out of scope. UI only labels alfather', 'US-8.1': 'Process', 'US-8.2': 'Manual, in provider security pages' };
const map = sec('map', 'Stories to screens', 'Every story, and where it is designed', '', tbl(['Story', 'Title', 'Screens', 'Note'], ST.map(([id, t, s]) => [`<span class="usc">${id}</span>`, t, s === '—' ? '<span class="mut">None</span>' : s.split(', ').map((x) => (/^[A-K]\d/.test(x) ? link(x) : x)).join(' '), `<span class="mut">${noui[id] || ''}</span>`])));

// ------------ 7. hierarchy ------------
const hier = sec('hier', 'Content hierarchy', 'What reads first, second and last', 'Each key screen has one hero. Anything not needed to decide the next action waits behind a tap.', tbl(['Screen', 'First (hero)', 'Second', 'Third', 'On demand'], [
  ['Agenda', 'Up next: title, time, countdown, Join', 'Today’s remaining events with colour and time', 'Later days; all-day chips', 'Earlier today, day jump, view switch, alfather'],
  ['Day, 3 days, week', 'The grid with the now line and shade', 'Event blocks, overlaps side by side', 'All-day chips, second time zone', 'Drag, resize, jump to date'],
  ['Event detail', 'Title', 'Time with zone and countdown; Join', 'Place, people, reminder, notes', 'Actions, edit, reply, delete'],
  ['Create and edit', 'Title', 'Calendar and time', 'Place, video call, reminder', 'Repeat, notes, time zone'],
  ['Quick add', 'The sentence and what was understood', 'Fields as editable lines', 'Conflict check', 'Edit details, other calendar'],
  ['Accounts', 'Which account needs attention', 'Status and last sync for each', 'Calendar counts', 'Reconnect, remove'],
  ['Settings', 'Identity and hub health', 'Calendars and accounts', 'Alerts and tokens', 'Server and sync'],
  ['Widget, lock screen', 'Next event with the dial', 'Join', 'Place', 'Open the app'],
]));

// ------------ 8. flows ------------
const ext = (t) => ({ ext: t });
const flows = sec('flows', 'User flows', 'Eleven flows, drawn in wireframe', 'Each step is a screen from the wireframe set. Solid arrows are the main path; red arrows are failure branches.', [
  flow('1 · First run', 'Pair the phone with the hub, connect the first account, land on a live agenda.', [{ id: 'A1', t: 'Welcome', c: 'Connect to your server' }, { id: 'A2', t: 'Pair', c: 'Address and token, reachability check' }, { id: 'A3', t: 'Choose provider' }, { id: 'A4', t: 'Provider sheet', c: 'System browser' }, { id: 'A5', t: 'Connected', c: 'Calendars appear, events load' }, { id: 'B1', t: 'Agenda' }], 'Hub unreachable: checklist and retry. Work tenant blocks consent: admin approval screen (A7). iCloud wrong password: inline error, nothing saved (A6).'),
  flow('2 · Join the next meeting', 'Three doors, one result: the meeting opens in its own app.', [{ id: 'J4', t: 'Reminder', c: '10 minutes before, Join' }, { ...ext('Teams, Meet or browser opens'), t: 'Meeting app' }, { id: 'B1', t: 'Or Up next', c: 'Join in the last 10 minutes or while live' }], 'Also from the widget, the lock screen card, and Event detail. No link: the card shows place and walking time instead.'),
  flow('3 · Create an event', 'The form is the full route. Quick add is the short one (flow 7).', [{ id: 'B1', t: 'Dock plus' }, { id: 'E1', t: 'Form', c: 'Title first, default calendar' }, { id: 'E3', t: 'Date and time' }, { id: 'E6', t: 'Add Meet', c: 'Optional' }, { id: 'B1', t: 'Appears in agenda', c: 'After the provider confirms' }], 'Provider write fails: stays on the form with the edits and a retry (E8). Conflicting change from elsewhere: shows both versions (E9).'),
  flow('4 · Edit a repeating event', 'The scope question appears only when it matters.', [{ id: 'B1', t: 'Tap the event' }, { id: 'D1', t: 'Detail', c: 'Edit' }, { id: 'E1', t: 'Change the time' }, { id: 'E5', t: 'This, following or all', c: 'On Save' }, { id: 'B1', t: 'Updated' }], 'Read-only calendar: Edit is shown disabled with the reason.'),
  flow('5 · Reply to an invitation', 'Unanswered invitations are the only rows with a tag.', [{ id: 'B1', t: 'Respond tag' }, { id: 'D1', t: 'Detail', c: 'Reply bar pinned' }, { id: 'F1', t: 'Yes, Maybe, No', c: 'Optional note' }, { id: 'B1', t: 'Row updates', c: 'Toast with Undo' }], 'iCloud invitations: status is read only (D3). Provider error: the note is kept, retry.'),
  flow('6 · Reschedule by dragging', 'The 3 day view is the drag surface.', [{ id: 'C3', t: '3 days', c: 'Long press an event' }, { id: 'C5', t: 'Drag', c: 'Ghost stays, chip shows the new time' }, { ...ext('Drop: server writes, block stays'), t: 'Saved' }, { ...ext('Toast: back at 14:30. Google rejected the change. Retry'), t: 'Rolled back', alt: true }], 'Resize uses the bottom handle and shows a duration chip. Read-only calendars never lift.'),
  flow('7 · Quick add with alfather', 'Type a sentence, check it, confirm. Nothing is created without a tap.', [{ id: 'B1', t: 'Tap the dock line' }, { id: 'I1', t: 'Type', c: 'Words are marked as understood' }, { id: 'I3', t: 'Preview', c: 'Fields editable, conflict check' }, { id: 'B1', t: 'Created', c: 'Toast with Undo' }], 'Ambiguous time or person: one question with tappable answers (I4). alfather down: basic parsing handles simple phrases (I5).'),
  flow('8 · Find free time', 'Ask in words, see slots, book one.', [{ id: 'I1', t: 'Ask', c: '“Free for 2 hours this week?”' }, { id: 'I7', t: 'Slots as cards' }, { id: 'C1', t: 'Overlay on Day', c: 'Swipe between slots' }, { id: 'B1', t: 'Booked' }], 'alfather down: the screen says so and offers the week view (I9).'),
  flow('9 · Reconnect an account', 'A banner takes you straight to the fix.', [{ id: 'B1', t: 'Banner', c: '“Work needs you to sign in again”' }, { id: 'H1', t: 'Accounts' }, { id: 'H3', t: 'Detail', c: 'Reconnect' }, { id: 'A4', t: 'Provider sheet' }, { id: 'B1', t: 'Caught up' }], 'Admin blocks the app now: A7. iCloud: new app password form (H6).'),
  flow('10 · Choose what shows', 'Show, hide and recolour calendars.', [{ id: 'G1', t: 'Settings' }, { id: 'G2', t: 'Calendars', c: 'Switches, default noted' }, { id: 'G3', t: 'Colour', c: 'Eight pigments' }], 'Choices are stored on the server, so every device agrees.'),
  flow('11 · Check sync health', 'The dot beside the avatar is the way in.', [{ id: 'B1', t: 'Dot turns brass or madder' }, { id: 'K4', t: 'Server and sync', c: 'Per account, backup, secret expiry' }, { id: 'H3', t: 'Account detail', c: 'Reconnect if needed' }], 'Offline: the banner states the age of the data (K1). Hub down: K5.'),
].join(''));

// ------------ 9. wireframes ------------
const wires = sec('wires', 'Wireframes', `Low fidelity, ${SCREENS.length} screens`, 'Grey boxes, one brass number per content block. Numbers match the legend under each frame and the hierarchy table above. States are listed under each title and drawn in hi-fi.', `<div class="wgal">${SCREENS.map(wfCard).join('')}</div>`);

// ------------ 10. states ------------
const STATES = [
  ['Empty: no accounts', 'A1'], ['Empty: no events', 'B6'], ['Loading', 'B5, A4b, A6c, E7, A5 (progress)'], ['Populated', 'B1, C1, C3, C4'], ['Dense and overlapping', 'B4, C2'], ['All-day events', 'B1, B4 (chips)'], ['In-progress meeting', 'B3, J2'], ['Recurring edit and delete choices', 'E5, D8'],
  ['Error: provider write failed', 'E8, C7, F4'], ['Error: needs reauth', 'H2, H3, H4, J5'], ['Error: admin approval needed', 'A7'], ['Error: wrong iCloud password', 'A6b, H6'], ['Offline and stale', 'K1, K2, K5, J2'], ['Conflict', 'E9'], ['Read only', 'D3'], ['alfather unavailable', 'I5, I9'], ['Light and dark', 'Every frame'],
];
const states = sec('states', 'State coverage', 'Every state the brief asked for', '', tbl(['State', 'Hi-fi frames'], STATES.map(([s, f]) => [s, f.split(', ').map((x) => (/^[A-K]\d/.test(x) ? link(x.split(' ')[0]) + x.slice(x.split(' ')[0].length) : x)).join(', ')])));

// ------------ 11. refs ------------
const refs = sec('refs', 'References', 'What was read, and how', '', tbl(['Reference', 'How it was read', 'Status'], [
  ['claude.ai artifact · A3 flows', 'Artifact tool, saved HTML. Rules and tokens read from its page', 'Read'],
  ['claude.ai artifact · A4 flows', 'Artifact tool, saved HTML', 'Read'],
  ['claude.ai artifact · shop design canvas', 'Artifact tool: file list, and one screen’s source (settings, trays, motion)', 'Read, partly: one screen of 145 in full'],
  ['claude.ai artifact · reader design canvas', 'Artifact tool: file list only (30 files: setup, queue, reader, review, settings states)', 'Skimmed'],
  ['benji.org, Honkish, Family Values', 'Fetched and searched', 'Read'],
  ['distill.id', 'Fetched and searched', 'Read. Only the home page text came through; videos did not'],
]) + '<p class="mut" style="margin-top:14px;max-width:70ch">Everything read was treated as reference data. Nothing was reproduced: no palette, typeface, layout or motif from these sources appears in Gnomon.</p>');

// ------------ 12. open ------------
const OPEN = [
  ['Conflict', 'No story covers pairing the app itself with the hub. US-7.1 describes tokens for scripts. The app needs a device token and a pairing screen (A2).', 'Add a story to E0 or E1.'],
  ['Conflict', 'The plan’s risks table names a read-only ICS fallback for blocked work accounts. No story or UI exists. A7 offers it.', 'Add a story, or drop the fallback.'],
  ['Conflict', 'US-7.4 says free time respects working hours. There is no story to set them. G5b adds the screen.', 'Add to E4 or E7.'],
  ['Conflict', 'US-2.4 puts server status on a server page (/status). The app shows it too (K4), as the observability NFR asks.', 'Confirm.'],
  ['Conflict', 'US-3.2 loads 14 days. The agenda needs more when scrolled. B1 assumes paging in 14 day chunks.', 'Confirm paging.'],
  ['Conflict', 'US-6.3 asks for drag and drop in a week view. Seven columns in portrait is cramped. The 3 day view is the primary drag surface; week drags need a long press.', 'Accept, or make week landscape only for drag.'],
  ['Conflict', 'US-6.4 and widgets need a native target. On Expo this is extra work. The designs assume it exists.', 'Decide on Expo or SwiftUI.'],
  ['Question', 'Should writes made offline queue and sync later? The design disables them (K2) and keeps the draft.', 'Product decision.'],
  ['Question', 'Is the Up next 10 minute Join window (US-3.1) fixed or a setting?', 'Fixed in the designs.'],
  ['Question', 'Search is not in the plan. Notion Calendar has it. No search screen is designed.', 'Add or leave out.'],
  ['Question', 'Event colours: provider colour per event is ignored. Only per calendar colours are used (US-4.2).', 'Confirm.'],
  ['Question', 'Working name Gnomon, and a wordmark in a serif, need a check against the owner’s taste. Fonts are Google Fonts (Newsreader, Schibsted Grotesk, DM Mono), free to ship.', 'Approve or rename.'],
];
const open = sec('open', 'Open questions and conflicts with the plan', 'Things to settle before build', '', tbl(['Type', 'Item', 'Ask'], OPEN.map(([a, b, c]) => [`<span class="chip ${a === 'Conflict' ? 'er' : 'br'}" style="height:24px;font-size:12px">${a}</span>`, b, `<span class="mut">${c}</span>`])));

const doc = page({
  title: 'Gnomon IA and Wireframes',
  eyebrow: 'Gnomon · Information architecture · Design direction · Wireframes',
  h1: 'A calendar that treats the day as <i>light and shade.</i>',
  lede: 'Research and structure for a personal calendar app on your own home server: goals, navigation, a distinct identity, screens, flows and wireframes, each tied to a user story in the plan.',
  nav: [['sum', 'Recommendation'], ['goals', 'Goals'], ['nav', 'Navigation'], ['direction', 'Design direction'], ['inv', 'Screens'], ['map', 'Stories'], ['hier', 'Hierarchy'], ['flows', 'Flows'], ['wires', 'Wireframes'], ['states', 'States'], ['refs', 'References'], ['open', 'Open questions']],
  sections: [summary, goals, nav, direction, inventory, map, hier, flows, wires, states, refs, open].join(''),
  extraCss: `${WCSS}
.two{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,420px),1fr));gap:34px;align-items:start}
.pts{display:grid;gap:12px;color:var(--ink2);max-width:62ch}.pts b{color:var(--ink);font-weight:500}
.kv{display:grid;grid-template-columns:repeat(2,1fr);gap:12px}.kv>div{background:var(--surface);border-radius:18px;padding:16px;display:grid;gap:4px;align-content:start;min-width:0}.kv .serif{font-size:34px;line-height:1}.kv span:last-child{color:var(--ink2);font-size:13.5px}
.h3{font-family:var(--serif);font-weight:500;font-size:22px;margin-top:28px}
.swg{display:grid;grid-template-columns:repeat(auto-fill,minmax(150px,1fr));gap:10px}
.sw2{display:flex;gap:10px;align-items:center;min-width:0}.sw2 i{width:38px;height:38px;border-radius:11px;flex:none;box-shadow:inset 0 0 0 1px var(--line)}.sw2 span{display:grid;line-height:1.2;min-width:0}.sw2 b{font-weight:500;font-size:13.5px}.sw2 small{font-size:11px;color:var(--ink2)}
.specs{display:grid;gap:18px;margin:18px 0}.specs>div{display:grid;gap:4px}.specs small{color:var(--ink2)}
.icg{display:grid;grid-template-columns:repeat(auto-fill,minmax(74px,1fr));gap:12px}.icg span{display:grid;justify-items:center;gap:4px}.icg small{font-family:var(--mono);font-size:9.5px;color:var(--ink2)}
.nmap{display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:14px;margin-top:14px}.nmap>div{display:grid;gap:8px;align-content:start}.nn{background:var(--surface);border-radius:14px;padding:10px 12px;font-size:14px}.nn small{display:block;color:var(--ink2);font-size:12px}.nn.big{background:var(--inv);color:var(--invInk);padding:16px;font-family:var(--serif);font-size:22px}.nn.big small{color:var(--invDim);font-family:var(--sans)}
.wfc.rec .wf{border-color:var(--brass);box-shadow:0 0 0 3px var(--brassT)}
.pg a{color:var(--brassInk)}
.t td a{text-decoration:none;font-size:12.5px}
.t td{font-size:14px}
`,
});

export default doc.replace('@@SCREENS@@', String(INV.length)).replace('@@STORIES@@', `${ST.filter((s) => s[2] !== '—' && !(s[0] in noui)).length} of ${ST.length}`);
