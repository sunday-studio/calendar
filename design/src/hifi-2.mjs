import { page, section, phoneCard, boardCard, ico, af, upNext, daylight, dayHead, allDay, ev, nowLine, topbar, seg, dock, navbar, sheet, toast, banner, fields, timeline, dial, hm } from './lib.mjs';

const DL = [
  { s: '08:30', e: '09:15', c: 'cobalt' }, { s: '10:00', e: '10:30', c: 'clay' }, { s: '11:00', e: '12:00', c: 'clay' }, { s: '11:30', e: '12:30', c: 'cobalt' },
  { s: '13:00', e: '14:00', c: 'cobalt' }, { s: '14:30', e: '15:30', c: 'mauve' }, { s: '16:00', e: '17:00', c: 'verdigris' }, { s: '19:30', e: '21:00', c: 'rose' },
];
const VIEWS = ['Agenda', 'Day', '3 days', 'Week'];
const standup = (extra = {}) => upNext({ title: 'Standup', rel: 'in 8 min', meta: ['10:00–10:30', 'Work', 'Microsoft Teams'], join: 'Join Teams', min: 8, ...extra });

// ---------- B: agenda ----------
const agendaRows = `
${dayHead('Today', 'later', '5 more', true)}
${allDay([['Ama’s birthday', 'rose'], ['Bin collection', 'verdigris']])}
${ev({ s: '11:00', e: '12:00', t: 'Roadmap review', c: 'clay', cal: 'Work', link: 'Teams', rsvp: 'respond' })}
${ev({ s: '11:30', e: '12:30', t: 'Dentist, Dr Owusu', c: 'cobalt', cal: 'Personal', loc: '14 Harley St' })}
${ev({ s: '13:00', e: '14:00', t: 'Lunch with Sam', c: 'cobalt', cal: 'Personal', loc: 'Kōya, Frith St' })}
${ev({ s: '14:30', e: '15:30', t: 'Design crit', c: 'mauve', cal: 'Side project', link: 'Meet' })}`;

const B1 = `${topbar('Today', 'Tuesday 13 October')}${seg(VIEWS, 0)}${daylight('09:52', DL)}${standup()}
<div class="nowline" style="margin-top:14px;color:var(--ink2)"><span>1 earlier today</span>${ico('chevD', 14)}</div>${agendaRows}${dock()}`;

const B3 = `${topbar('Today', 'Tuesday 13 October')}${seg(VIEWS, 0)}${daylight('10:12', DL)}
${upNext({ title: 'Standup', rel: 'started 12 min ago', kicker: 'In progress', meta: ['10:00–10:30', 'Work', 'Microsoft Teams'], join: 'Join Teams', min: 18, state: 'live', dialLabel: 'left' })}
${dayHead('Today', 'later', '5 more', true)}
${ev({ s: '11:00', e: '12:00', t: 'Roadmap review', c: 'clay', cal: 'Work', link: 'Teams', rsvp: 'respond' })}
${ev({ s: '11:30', e: '12:30', t: 'Dentist, Dr Owusu', c: 'cobalt', cal: 'Personal', loc: '14 Harley St' })}
${ev({ s: '13:00', e: '14:00', t: 'Lunch with Sam', c: 'cobalt', cal: 'Personal', loc: 'Kōya, Frith St' })}${dock()}`;

const sk = (w, h = 14) => `<div class="sk" style="width:${w};height:${h}px"></div>`;
const B5 = `${topbar('Today', 'Tuesday 13 October', { sync: '<i class="sdot stale"></i>' })}${seg(VIEWS, 0)}
<div class="dl"><div class="sk" style="height:10px"></div></div>
<div class="up" style="background:var(--sunk);color:var(--ink)"><div style="display:grid;gap:12px">${sk('90px', 12)}${sk('200px', 28)}${sk('160px', 13)}${sk('100%', 50)}</div></div>
<div style="padding:26px 20px 0;display:grid;gap:22px">${[0, 1, 2, 3].map(() => `<div style="display:grid;grid-template-columns:50px 1fr;gap:12px">${sk('40px', 13)}<div style="display:grid;gap:7px">${sk('70%', 16)}${sk('45%', 12)}</div></div>`).join('')}</div>${dock()}`;

const B6 = `${topbar('Today', 'Tuesday 13 October')}${seg(VIEWS, 0)}${daylight('09:52', [])}
<div style="padding:70px 28px 0"><div class="hero">Nothing planned for the next <i>14 days.</i></div><p class="mut" style="margin-top:14px;font-size:15.5px;max-width:30ch">Your calendars are connected and in sync. Add something, or enjoy the quiet.</p>
<div class="stack" style="margin-top:28px;max-width:260px"><div class="btn pri">${ico('plus', 20)}New event</div><div class="btn ghost" style="justify-content:flex-start">${af('Try “dinner with Ife thursday 8pm”')}</div></div></div>${dock()}`;

const B4 = `${topbar('Thursday', '15 October · 7 events', { sync: '<i class="sdot"></i>' })}${seg(VIEWS, 0)}
${allDay([['Offsite: Lisbon', 'clay'], ['Ama’s birthday', 'rose']])}
${ev({ s: '07:15', e: '10:40', t: 'Flight to Lisbon', c: 'clay', cal: 'Work', loc: 'LHR T5', state: 'past' })}
${nowLine('09:52')}
${ev({ s: '10:00', e: '10:30', t: 'Standup', c: 'clay', cal: 'Work', link: 'Teams', rec: 1 })}
${ev({ s: '11:00', e: '12:00', t: 'Design crit', c: 'mauve', cal: 'Side project', link: 'Meet', rsvp: 'maybe', state: 'tent' })}
${ev({ s: '11:00', e: '11:45', t: 'Pairing with Ife', c: 'cobalt', cal: 'Personal', link: 'Meet' })}
${`<div class="nowline" style="color:var(--madder);padding-left:82px;font-family:var(--sans);font-size:12px;font-weight:500;margin-top:-4px">${ico('alert', 13)}&nbsp;Overlaps Design crit</div>`}
${ev({ s: '13:00', e: '14:00', t: 'Lunch with Sam', c: 'cobalt', cal: 'Personal', loc: 'Kōya' })}
${ev({ s: '15:00', e: '15:30', t: 'Vendor call', c: 'clay', cal: 'Work', rsvp: 'declined', state: 'dec' })}
${ev({ s: '16:00', e: '17:00', t: 'Piano lesson', c: 'verdigris', cal: 'Home' })}${dock()}`;

function month() {
  const days = ['M', 'T', 'W', 'T', 'F', 'S', 'S'];
  let cells = '';
  const dots = { 1: 1, 2: 2, 5: 1, 6: 1, 7: 3, 8: 1, 9: 2, 12: 2, 13: 5, 14: 3, 15: 7, 16: 2, 19: 1, 20: 4, 21: 2, 22: 1, 23: 2, 26: 1, 27: 3, 28: 1 };
  for (let i = 0; i < 4; i++) cells += `<span></span>`;
  for (let d = 1; d <= 31; d++) {
    const on = d === 13, n = dots[d] || 0;
    cells += `<span style="display:flex;flex-direction:column;align-items:center;gap:3px;height:44px;justify-content:center;border-radius:12px;${on ? 'background:var(--ink);color:var(--bg)' : ''}"><span style="font-family:var(--serif);font-size:18px">${d}</span><i style="display:flex;gap:2px;height:4px">${Array.from({ length: Math.min(n, 3) }, () => `<b style="width:4px;height:4px;border-radius:2px;background:${on ? 'var(--brass)' : 'var(--ink3)'}"></b>`).join('')}</i></span>`;
  }
  return `<div style="display:grid;grid-template-columns:repeat(7,1fr);margin-top:14px;text-align:center"><div style="grid-column:1/-1;display:grid;grid-template-columns:repeat(7,1fr);font-size:11px;color:var(--ink2);padding-bottom:6px">${days.map((d) => `<span>${d}</span>`).join('')}</div>${cells}</div>`;
}
const B7 = `${topbar('Today', 'Tuesday 13 October')}${seg(VIEWS, 0)}${daylight('09:52', DL)}${standup()}${agendaRows}${dock()}
${sheet(`<div class="row" style="justify-content:space-between"><span class="sh1">October <i style="color:var(--ink2)">2026</i></span><span class="row" style="gap:6px"><span class="ib flat">${ico('chevL')}</span><span class="ib flat">${ico('chevR')}</span></span></div>${month()}<div class="row" style="margin-top:16px;gap:10px"><div class="btn sm">Today</div><div class="sp"></div><span class="mut sm13">Dots show how busy a day is</span></div>`, 470)}`;

// up next states board
const miniUp = (o) => `<div style="margin-bottom:12px">${upNext({ compact: true, ...o })}</div>`;
const UPB = `<div style="display:grid;gap:4px"><div class="cap" style="margin-bottom:8px">Five states of the same card</div>
${miniUp({ title: 'Roadmap review', rel: 'in 2 h 8 min', meta: ['12:00', 'Work', 'Microsoft Teams'], join: 'Join Teams', state: 'later', min: 60, dialBig: '2h', dialLabel: 'away' })}
${miniUp({ title: 'Standup', rel: 'in 8 min', meta: ['10:00', 'Work'], join: 'Join Teams', min: 8 })}
${miniUp({ title: 'Standup', rel: 'started 12 min ago', kicker: 'In progress', meta: ['ends 10:30', 'Work'], join: 'Join Teams', min: 18, state: 'live', dialLabel: 'left' })}
${miniUp({ title: 'Dentist, Dr Owusu', rel: 'in 8 min', meta: ['11:30', '14 Harley St, 12 min walk'], min: 8 })}
${miniUp({ title: 'Nothing else today', rel: 'next is tomorrow', kicker: 'Up next', meta: ['Wed 14 · Standup, 10:00'], min: 0, dialBig: '☾', dialLabel: 'rest' })}
</div>`;

// ---------- C: day / week ----------
const strip = (on) => `<div class="dayhd" style="position:relative;padding:0 12px;margin-top:12px">${[['M', 12], ['T', 13], ['W', 14], ['T', 15], ['F', 16], ['S', 17], ['S', 18]].map(([n, d]) => `<div class="${d === on ? 'on' : ''}"><div class="dn">${n}</div><div class="dd">${d}</div></div>`).join('')}</div>`;
const dayEvs = [
  { s: '08:30', e: '09:15', t: 'Run with Kofi', c: 'cobalt', state: 'past' },
  { s: '10:00', e: '10:30', t: 'Standup', c: 'clay' },
  { s: '11:00', e: '12:00', t: 'Roadmap review', c: 'clay', lanes: 2, lane: 0, state: 'tent' },
  { s: '11:30', e: '12:30', t: 'Dentist', c: 'cobalt', lanes: 2, lane: 1 },
  { s: '13:00', e: '14:00', t: 'Lunch with Sam', c: 'cobalt' },
  { s: '14:30', e: '15:30', t: 'Design crit', c: 'mauve' },
];
const C1 = `${topbar('Tuesday', '13 October · week 42')}${seg(VIEWS, 1)}${strip(13)}${allDay([['Ama’s birthday', 'rose'], ['Bin collection', 'verdigris']])}
${timeline({ top: 262, h0: 8, h1: 17, hh: 52, cols: [dayEvs], now: '09:52' })}${dock()}`;

const C2 = `${topbar('Tuesday', '13 October · London and New York')}${seg(VIEWS, 1)}${strip(13)}
${timeline({
  top: 218, h0: 9, h1: 18, hh: 52, g: 88, cols: [[
    { s: '10:00', e: '10:30', t: 'Standup', c: 'clay' }, { s: '11:00', e: '12:00', t: 'Roadmap review', c: 'clay', lanes: 3, lane: 0, state: 'tent' }, { s: '11:30', e: '12:30', t: 'Dentist', c: 'cobalt', lanes: 3, lane: 1 }, { s: '11:45', e: '12:15', t: 'Call Ife', c: 'mauve', lanes: 3, lane: 2 },
    { s: '13:00', e: '14:00', t: 'Lunch with Sam', c: 'cobalt' }, { s: '14:30', e: '15:30', t: 'Design crit', c: 'mauve' }, { s: '16:00', e: '17:00', t: 'Piano lesson', c: 'verdigris' }].map((e) => ({ ...e, title: e.t }))], now: '09:52',
  extra: Array.from({ length: 10 }, (_, i) => `<span style="position:absolute;left:52px;top:${i * 52 - 8}px;font-family:var(--mono);font-size:10.5px;color:var(--ink3);background:var(--bg);padding:0 3px">${String((9 + i + 19) % 24).padStart(2, '0')}</span>`).join('') + `<span class="cap" style="position:absolute;left:12px;top:-26px;font-size:10px">LON</span><span class="cap" style="position:absolute;left:52px;top:-26px;font-size:10px;color:var(--brassInk)">NYC</span>`,
})}${dock()}`;

const w3 = (cols) => `<div class="dayhd" style="position:absolute;top:196px;padding-left:44px;padding-right:8px">${cols.map(([n, d, on]) => `<div class="${on ? 'on' : ''}"><div class="dn">${n}</div><div class="dd">${d}</div></div>`).join('')}</div>`;
const T3 = [
  [{ s: '10:00', e: '10:30', t: 'Standup', c: 'clay' }, { s: '11:00', e: '12:00', t: 'Roadmap review', c: 'clay', lanes: 2, lane: 0, state: 'tent' }, { s: '11:30', e: '12:30', t: 'Dentist', c: 'cobalt', lanes: 2, lane: 1 }, { s: '13:00', e: '14:00', t: 'Lunch with Sam', c: 'cobalt' }, { s: '14:30', e: '15:30', t: 'Design crit', c: 'mauve' }],
  [{ s: '10:00', e: '10:30', t: 'Standup', c: 'clay' }, { s: '13:00', e: '14:30', t: 'Planning', c: 'clay' }, { s: '15:00', e: '15:45', t: '1:1 with Ife', c: 'mauve' }],
  [{ s: '10:00', e: '10:30', t: 'Standup', c: 'clay' }, { s: '11:00', e: '12:00', t: 'Design crit', c: 'mauve', state: 'tent' }, { s: '13:00', e: '14:00', t: 'Lunch with Sam', c: 'cobalt' }, { s: '15:00', e: '15:30', t: 'Vendor call', c: 'clay', state: 'dec' }],
].map((c) => c.map((e) => ({ ...e, title: e.t })));
const C3 = `${topbar('3 days', '13 to 15 October')}${seg(VIEWS, 2)}${w3([['Tue', 13, 1], ['Wed', 14], ['Thu', 15]])}
${timeline({ top: 252, h0: 9, h1: 18, hh: 52, g: 44, cols: T3, now: '09:52', nowCol: 0, compact: false })}${dock()}`;

const WK = [
  [{ s: '10:00', e: '10:30', c: 'clay', t: 'Standup' }, { s: '11:00', e: '12:00', c: 'clay', t: 'Roadmap', lanes: 2, lane: 0 }, { s: '11:30', e: '12:30', c: 'cobalt', t: 'Dentist', lanes: 2, lane: 1 }, { s: '13:00', e: '14:00', c: 'cobalt', t: 'Lunch' }, { s: '14:30', e: '15:30', c: 'mauve', t: 'Crit' }],
  [{ s: '10:00', e: '10:30', c: 'clay', t: 'Standup' }, { s: '13:00', e: '14:30', c: 'clay', t: 'Planning' }, { s: '15:00', e: '15:45', c: 'mauve', t: '1:1' }],
  [{ s: '10:00', e: '10:30', c: 'clay', t: 'Standup' }, { s: '11:00', e: '12:00', c: 'mauve', t: 'Crit' }, { s: '13:00', e: '14:00', c: 'cobalt', t: 'Lunch' }],
  [{ s: '10:00', e: '10:30', c: 'clay', t: 'Standup' }, { s: '16:00', e: '17:00', c: 'verdigris', t: 'Piano' }],
  [{ s: '09:00', e: '10:00', c: 'cobalt', t: 'Run' }, { s: '12:00', e: '13:30', c: 'cobalt', t: 'Brunch' }, { s: '19:00', e: '21:00', c: 'rose', t: 'Dinner' }],
  [{ s: '10:00', e: '12:00', c: 'verdigris', t: 'Market' }],
  [{ s: '11:00', e: '13:00', c: 'rose', t: 'Roast at Mum’s' }],
].map((c) => c.map((e) => ({ ...e, title: e.t })));
const C4 = `${topbar('Week 42', '12 to 18 October')}${seg(VIEWS, 3)}
<div class="dayhd" style="position:absolute;top:196px;padding-left:32px;padding-right:6px">${['M 12', 'T 13', 'W 14', 'T 15', 'F 16', 'S 17', 'S 18'].map((x, i) => `<div class="${i === 1 ? 'on' : ''}"><div class="dn">${x.split(' ')[0]}</div><div class="dd" style="font-size:17px;${i === 1 ? 'width:28px' : ''}">${x.split(' ')[1]}</div></div>`).join('')}</div>
<div class="wk">${timeline({ top: 248, h0: 8, h1: 19, hh: 46, g: 32, cols: WK, now: '09:52', nowCol: 1, compact: true })}</div>${dock()}`;

const C5 = `${topbar('Wednesday', '14 October')}${seg(VIEWS, 1)}${strip(14)}
${timeline({
  top: 220, h0: 11, h1: 20, hh: 52, cols: [[{ s: '13:00', e: '14:30', t: 'Planning', c: 'clay' }]],
  extra: `<div class="tghost" style="left:54px;width:326px;top:${(14.5 - 11) * 52}px;height:52px"></div><div class="te drag" style="--c:var(--mauve);left:54px;width:326px;top:${(15.25 - 11) * 52 + 1}px;height:50px"><b>Design crit</b><small>15:15–16:15</small><i class="rz"></i></div><div class="chipt" style="left:130px;top:${(15.25 - 11) * 52 - 36}px">Wed 15:15 · +45 min</div>`,
})}${dock()}`;

const C6 = `${topbar('Tuesday', '13 October')}${seg(VIEWS, 1)}${strip(13)}
${timeline({
  top: 220, h0: 11, h1: 20, hh: 52, cols: [[{ s: '11:00', e: '12:00', t: 'Roadmap review', c: 'clay', title: 'Roadmap review' }, { s: '14:30', e: '15:30', t: 'Design crit', c: 'mauve', title: 'Design crit' }]],
  extra: `<div class="tghost" style="left:54px;width:326px;top:${(13 - 11) * 52}px;height:52px"></div><div class="te drag" style="--c:var(--cobalt);left:54px;width:326px;top:${(13 - 11) * 52 + 1}px;height:${1.25 * 52 - 2}px"><b>Lunch with Sam</b><small>13:00–14:15</small><i class="rz"></i></div><div class="chipt" style="left:250px;top:${(14.25 - 11) * 52 + 8}px">1 h 15 min</div>`,
})}${dock()}`;

const C7 = `${topbar('Wednesday', '14 October')}${seg(VIEWS, 1)}${strip(14)}
${timeline({ top: 220, h0: 11, h1: 20, hh: 52, cols: [[{ s: '13:00', e: '14:30', t: 'Planning', c: 'clay', title: 'Planning' }, { s: '14:30', e: '15:30', t: 'Design crit', c: 'mauve', title: 'Design crit' }]] })}
${toast('Design crit is back at 14:30. Google rejected the change.', 'Retry', 'alert', 100)}${dock()}`;

// ---------- cards ----------
const nB = [['b-agenda', 'Agenda and Up next'], ['b-day', 'Day, 3 days, week']];

const cardsB = [
  phoneCard({ id: 'B1', title: 'Agenda with Up next', state: 'Populated', us: ['US-3.1', 'US-3.2', 'US-6.5'], screen: B1, notes: ['<b>Hero:</b> the Up next card. Everything else is quiet text on chalk.', 'The <b>dial</b> counts down a 60 minute window. Inside 10 minutes the Join button turns brass (US-3.1).', '<b>Daylight strip</b>: elapsed hours are in shade, the brass tick is now, coloured ticks are events.', 'All-day events pin as chips at the top of each day, not as rows (US-3.2).', 'Rows show calendar colour, time, title, place or call type. RSVP state appears only when it needs you.', '<b>Motion:</b> tapping a row lifts it into Event detail; the title and time travel, nothing else duplicates. Pull to refresh draws the dial once.']}),
  phoneCard({ id: 'B3', title: 'Meeting in progress', state: 'Live', us: ['US-3.1', 'US-5.6'], screen: B3, notes: ['Kicker becomes “In progress” with a pulsing brass dot; the dial shows minutes left, not minutes until.', 'Join stays prominent until the event ends, then the card moves on to the next event.', 'The daylight strip’s shade has advanced to 10:12; it moves once a minute with a slow ease.', 'The ended Standup never leaves a gap: Up next becomes “Roadmap review” at 10:30.']}),
  phoneCard({ id: 'B4', title: 'Dense day', state: 'Populated', us: ['US-3.2', 'US-5.4'], screen: B4, notes: ['Past events are faded and hollow; the now line sits between them and what is next.', 'Two events that overlap get a one line warning in madder, not a modal.', 'Tentative events use a hatched square; declined events are struck through and quiet.', 'Recurring events carry the repeat glyph. An all-day event spanning days stays in chips.']}),
  phoneCard({ id: 'B5', title: 'Loading', state: 'Loading', us: ['US-3.2', 'US-3.5'], screen: B5, notes: ['Shown only on a cold first launch with no cache. If a cached agenda exists, it shows instantly (US-3.5) and the sync dot goes brass.', 'The skeleton matches the final layout so nothing jumps.', 'Shimmer stops under reduced motion.']}),
  phoneCard({ id: 'B6', title: 'Empty: no events', state: 'Empty', us: ['US-3.2', 'US-5.1', 'US-7.3'], screen: B6, notes: ['Accounts connected, nothing in the next 14 days. Warm and short. One primary action, one alfather hint.', 'The no-accounts empty state is in the onboarding group (A1).']}),
  phoneCard({ id: 'B7', title: 'Jump to date', state: 'Sheet', us: ['US-3.3'], screen: B7, notes: ['Tap the page title to open. Dots show load per day; the selected day is the inked square.', 'The sheet rises with a settle ease, 360 ms, no bounce. The notch at its top points at the day you came from.', 'Works the same from Day, 3 days and Week.']}),
  boardCard({ id: 'B2', title: 'Up next, five states', state: 'Component', us: ['US-3.1', 'US-5.6'], inner: UPB, w: 400, notes: ['Join appears only with a join link and only inside 10 minutes of start or while live (US-3.1). Before that it is dimmed so the layout never changes.', 'No link: the card shows the place and walking time instead of Join.', 'End of day: the dial rests and the card points at tomorrow.']}),
].join('');

const cardsC = [
  phoneCard({ id: 'C1', title: 'Day view', state: 'Populated', us: ['US-3.3'], screen: C1, notes: ['Hour grid with a brass now line and a small flag in the gutter. <b>Time above it is in hatched shade.</b>', 'Overlapping events sit side by side (Roadmap review and Dentist).', 'Swipe horizontally between days; the date strip follows. Tap the strip or title to jump.', 'All-day events live in chips above the grid.']}),
  phoneCard({ id: 'C2', title: 'Dense day with second time zone', state: 'Populated', us: ['US-3.3', 'US-6.5'], screen: C2, notes: ['Secondary time zone column (New York) sits beside the hour labels; its offset is shown in the header, not in every row.', 'Three way overlap narrows to thirds; titles truncate, a tap opens the event.', 'Turn the column on in Settings, Time zones (G4).']}),
  phoneCard({ id: 'C3', title: '3 day view', state: 'Populated', us: ['US-3.3', 'US-6.3'], screen: C3, notes: ['The main surface for drag and drop on a phone: wide enough for titles, short enough to scroll.', 'Only today’s column shows the now line and its shade.', 'Declined events keep their slot as an outline so the hour is not mistaken for free.']}),
  phoneCard({ id: 'C4', title: 'Week view', state: 'Populated', us: ['US-3.3', 'US-6.3'], screen: C4, notes: ['Seven columns in portrait are for reading. Titles shorten; tap for the sheet. Rotate for a wider grid.', 'Drag works here too but snaps to 15 minutes only after a long press, to avoid accidental moves.']}),
  phoneCard({ id: 'C5', title: 'Dragging to reschedule', state: 'Interaction', us: ['US-6.3', 'US-5.2'], screen: C5, notes: ['Long press lifts the event (haptic tick); the original slot stays as a dashed ghost.', 'A dark chip follows the finger with the new time and the shift (+45 min). Snaps to 15 minutes, ticking at each snap.', 'Drop writes to the server. The block is optimistic and shows its calendar colour.', 'Dragging near the screen edge scrolls to the next day.']}),
  phoneCard({ id: 'C6', title: 'Resizing', state: 'Interaction', us: ['US-6.3'], screen: C6, notes: ['Grab the handle at the bottom edge. The chip shows the new duration, not a time.', 'Minimum 15 minutes. Start time never moves while resizing.']}),
  phoneCard({ id: 'C7', title: 'Move failed, rolled back', state: 'Error', us: ['US-6.3', 'US-5.1'], screen: C7, notes: ['The block eases back to its slot and the toast names the event, the old time and who refused.', 'Retry repeats the same move. Nothing is half saved (US-5.1).', 'Read-only calendars never lift on long press; they give a short haptic and the toast “This calendar is read only.”']}),
].join('');

const html = page({
  title: 'Gnomon Hi-fi: Agenda and Day',
  eyebrow: 'Gnomon · Hi-fi 2 of 4 · Agenda and Up next · Day, 3 day and week',
  h1: 'The day, <i>in light and shade.</i>',
  lede: 'Where the app earns its place: what is next, what is free, and how to move things. All names, times and places are sample content. Today is Tuesday 13 October 2026, 09:52.',
  nav: nB,
  sections: section('b-agenda', 'Epics E3 · US-3.1 to 3.5', 'Agenda and Up next', 'One merged list from every account. The Up next card is the hero; the daylight strip tells you how much of the day is behind you.', cardsB) + section('b-day', 'Epics E3, E6 · US-3.3, 6.3, 6.5', 'Day, 3 days and week', 'Past time is hatched shade. Drag, resize and a second time zone are all one gesture away.', cardsC),
  extraCss: `.wk .te{font-size:9.5px;padding:3px 3px;border-radius:6px}.wk .te small{display:none}.wk .tl .hr span{left:2px;font-size:9px;padding:0 2px}`,
});
export default html;
