import { page, section, phoneCard, boardCard, ico, af, topbar, dock, navbar, sheet, toast, banner, fields, glabel, sw, ev, daylight, dayHead, nowLine } from './lib.mjs';

const nav2 = (right = '') => navbar('back', '', right || `<span class="row" style="gap:6px"><span class="ib">${ico('edit')}</span><span class="ib">${ico('more')}</span></span>`);
const calLine = (c, t) => `<div class="row cap" style="margin:18px 20px 0;gap:8px"><i class="sq" style="--c:var(--${c})"></i>${t}</div>`;
const title = (t, whenA, whenB, rel) => `<div style="padding:8px 20px 0"><div class="hero" style="font-size:34px">${t}</div><div style="margin-top:14px;font-family:var(--serif);font-size:20px">${whenA}</div><div class="mono mut" style="font-size:14px;margin-top:2px">${whenB}${rel ? ` · <i class="serif" style="color:var(--brassInk);font-size:16px">${rel}</i>` : ''}</div></div>`;
const joinRow = (l, sub = '') => `<div style="padding:16px 16px 0;display:flex;gap:10px"><div class="btn brass" style="flex:1">${ico('video', 20)}${l}</div><span class="ib" style="width:50px;height:50px;border-radius:14px">${ico('copy')}</span></div>${sub ? `<div class="mut" style="padding:6px 20px 0;font-size:12.5px">${sub}</div>` : ''}`;
const att = (rows) => rows.map(([n, s, o]) => `<div class="row" style="padding:5px 0"><span class="av" style="width:30px;height:30px;font-size:11px;background:var(--sunk);color:var(--ink)">${n.slice(0, 1)}</span><span class="sp">${n}${o ? ` <span class="mut sm13">${o}</span>` : ''}</span><span class="chip ${s === 'Going' ? 'ok' : s === 'No' ? 'er' : ''}" style="height:24px;font-size:12px">${s}</span></div>`).join('');
const rsvpBar = (sel = -1) => `<div style="position:absolute;left:0;right:0;bottom:0;background:var(--surface);padding:14px 16px 30px;border-radius:24px 24px 0 0;box-shadow:0 -8px 30px rgba(22,34,29,.1);z-index:5"><div class="row" style="justify-content:space-between;margin-bottom:10px"><span style="font-weight:500">Are you going?</span><span class="lnk sm13">Add a note</span></div><div style="display:grid;grid-template-columns:repeat(3,1fr);gap:8px">${['Yes', 'Maybe', 'No'].map((x, i) => `<div class="btn ${i === sel ? 'pri' : ''}" style="height:46px">${x}</div>`).join('')}</div></div>`;

const D1 = `${nav2()}${calLine('clay', 'Work · casper@northwind.com')}${title('Roadmap review', 'Tuesday 13 October', '11:00–12:00 · 1 h · BST', 'in 1 h 8 min')}${joinRow('Join Teams', 'Opens in the Teams app')}
<div style="height:14px"></div>${fields([['pin', 'Room 4.12', null, { sub: 'Northwind HQ · 6 min walk' }], ['users', '6 people', null, { sub: 'Organiser Yaw Boateng' }], ['bell', '10 minutes before', null], ['note', 'Agenda for Q4: scope, staffing, dates.', null]])}${rsvpBar()}`;

const D2 = `${nav2()}${calLine('clay', 'Work · you organise this')}${title('Standup', 'Tuesday 13 October', '10:00–10:30 · 30 min · BST', 'started 12 min ago')}${joinRow('Join Teams')}
<div style="height:14px"></div>${fields([['repeat', 'Every weekday', null, { sub: 'Ends never' }], ['bell', '5 minutes before', null]])}
<div class="fl" style="margin-top:12px;padding:12px 16px"><div class="row" style="justify-content:space-between;margin-bottom:4px"><span style="font-weight:500">5 people</span><span class="mono mut sm13">4 going · 1 waiting</span></div>${att([['Ife Adeyemi', 'Going'], ['Kofi Mensah', 'Going'], ['Yaw Boateng', 'Going'], ['Ama Owusu', 'Going'], ['Sam Rivers', 'Waiting']])}</div>`;

const D3 = `${nav2(`<span class="ib">${ico('more')}</span>`)}${calLine('verdigris', 'Family · iCloud · read only')}${title('Parents’ evening', 'Thursday 15 October', '18:30–19:30 · 1 h · BST', '')}
${banner('nf', '', 'iCloud does not let other apps reply to invitations. Your status is <b style="display:inline">Accepted</b>. Change it in Calendar on a Mac or iPhone.')}
<div style="height:14px"></div>${fields([['pin', 'Oakfield Primary', null, { sub: '12 Mill Lane · Open in Maps', cls: '' }], ['users', '2 people', null, { sub: 'Organiser Ms Hart' }]])}
<div style="padding:16px;display:grid"><div class="btn off">${ico('edit')}Edit</div><p class="mut sm13" style="margin-top:8px">This event is in a read-only calendar, so editing and deleting are off.</p></div>`;

const D4 = `${nav2()}${calLine('cobalt', 'Personal · casprine.001@gmail.com')}${title('Call with Tunde', 'Friday 16 October', '15:00–15:30 · BST', '')}
<div style="padding:12px 16px 0"><div class="chip br" style="height:36px;width:100%;justify-content:flex-start;padding:0 12px;border-radius:12px">${ico('globe', 16)}&nbsp;10:00 for Tunde in New York</div></div>
${joinRow('Join Zoom', 'Link found in the notes')}
<div style="height:14px"></div>${fields([['pin', 'Open in Maps', null, { sub: 'No place set. Tap to add one.' }], ['bell', '10 minutes before', null], ['note', 'zoom.us/j/884 1156 029 · passcode in invite', null]])}`;

const actions = `<div class="sh1">Roadmap review</div><div class="stack" style="margin-top:16px;gap:2px">${[['edit', 'Edit'], ['copy', 'Duplicate'], ['moveto', 'Move to another calendar'], ['link', 'Copy join link']].map(([i, t]) => `<div class="row" style="height:52px;gap:14px">${ico(i, 22)}<span style="font-size:16px">${t}</span></div>`).join('')}<div class="row" style="height:52px;gap:14px;color:var(--madder);border-top:1px solid var(--line)">${ico('trash', 22)}<span style="font-size:16px">Delete…</span></div></div>`;
const D5 = D1 + sheet(actions, 380);

const calRow = (c, n, sub, o = '') => `<div class="row" style="height:56px;gap:14px;${o}"><i class="sq lg" style="--c:var(--${c})"></i><span class="sp"><b style="font-weight:500">${n}</b><small class="mut" style="display:block;font-size:12.5px">${sub}</small></span></div>`;
const D6 = D1 + sheet(`<div class="sh1">Move to</div>${glabel('Same account · Work')}${calRow('clay', 'Work', 'Moves, attendees are not notified again')}${glabel('Other accounts')}${calRow('cobalt', 'Personal', 'Google · copy, then delete the original')}${calRow('verdigris', 'Home', 'iCloud · copy, then delete the original')}${calRow('slate', 'UK holidays', 'Read only', 'opacity:.4')}${banner('', 'Moving across accounts', 'creates a new event and deletes this one. Attendees get a new invitation.')}`, 560);

const D7 = D4 + sheet(`<div class="sh1">Delete “Call with Tunde”?</div><div class="sh2">It is removed from Personal on Google and from your other devices.</div><div class="stack" style="margin-top:20px"><div class="btn dng">Delete event</div><div class="btn">Cancel</div></div>`, 270);
const D8 = D2 + sheet(`<div class="sh1">Delete “Standup”</div><div class="sh2">This is a repeating event.</div><div class="stack" style="margin-top:18px"><div class="btn dng" style="justify-content:flex-start;padding:0 16px">This event, Tue 13 Oct</div><div class="btn dng" style="justify-content:flex-start;padding:0 16px">This and following events</div><div class="btn dng" style="justify-content:flex-start;padding:0 16px">All events in the series</div><div class="btn">Cancel</div></div>`, 440);
const D9 = D2 + sheet(`<div class="sh1">Cancel “Standup” for everyone?</div><div class="sh2">You organise this meeting. Deleting it sends a cancellation to 5 people.</div>${banner('', '', 'This is the provider’s default behaviour. You can’t turn it off here.')}<div class="stack" style="margin-top:16px"><div class="btn dng">Delete and notify 5 people</div><div class="btn">Keep event</div></div>`, 400);

// ----- E create / edit -----
const ctitle = (t, caret = true) => `<div style="padding:16px 20px 0"><div style="font-family:var(--serif);font-size:32px;line-height:1.1;min-height:44px">${t}${caret ? '<i class="caret"></i>' : ''}</div></div>`;
const calChip = (c, n, s = '') => `<div style="padding:12px 20px 0"><span class="chip" style="height:34px"><i class="sq" style="--c:var(--${c})"></i>${n}${s ? `<span class="mut">· ${s}</span>` : ''}${ico('chevD', 15)}</span></div>`;
const saveBtn = (l = 'Save', st = '') => `<span class="btn brass sm ${st}">${l}</span>`;
const formRows = (o = {}) => fields([['clock', 'All day', sw(false)], ['', 'Starts', o.s || 'Wed 14 Oct · 13:00'], ['', 'Ends', o.e || '14:00 · 1 h'], ['globe', 'Time zone', 'London'], ['repeat', 'Repeat', o.rep || 'Never']], 'margin-top:14px')
  + fields([['pin', 'Location', o.loc || '<span style="color:var(--ink3)">Add a place</span>'], ['video', o.vid || 'Add video call', o.vidv ?? ico('chevR', 16)], ['bell', 'Reminder', '10 min before'], ['note', 'Notes', '<span style="color:var(--ink3)">Add notes</span>']], 'margin-top:12px');

const E1 = `${navbar('x', 'New event', saveBtn('Save', 'off'))}${ctitle('<span style="color:var(--ink3)">Title</span>')}${calChip('cobalt', 'Personal', 'default')}${formRows()}
<div style="padding:14px 20px"><span class="af">${ico('spark', 13)}Or type it in one line with alfather</span></div>`;
const E2 = `${navbar('x', 'New event', saveBtn())}${ctitle('Lunch with Sam', false)}${calChip('cobalt', 'Personal', 'casprine.001@gmail.com')}${formRows({ loc: 'Kōya, 49 Frith St', vid: 'Google Meet', vidv: '<span class="lnk">Remove</span>' })}
<div class="fl" style="margin-top:12px;padding:12px 16px"><div class="row">${ico('video', 20)}<span class="sp"><b style="font-weight:500">meet.google.com/kqr-vbxn-tui</b><small class="mut" style="display:block;font-size:12.5px">Created when you save</small></span></div></div>`;

const wheel = (items, on, w) => `<div style="flex:${w};text-align:center;display:grid;gap:0">${items.map((x, i) => `<div style="height:38px;line-height:38px;font-family:var(--serif);font-size:${i === on ? 26 : 20}px;color:${Math.abs(i - on) === 0 ? 'var(--ink)' : Math.abs(i - on) === 1 ? 'var(--ink2)' : 'var(--ink3)'}">${x}</div>`).join('')}</div>`;
const E3 = `${navbar('x', 'New event', saveBtn())}${ctitle('Lunch with Sam', false)}${calChip('cobalt', 'Personal')}
<div class="fl" style="margin-top:14px"><div class="fr"><span class="lb"><b>All day</b></span>${sw(false)}</div><div class="fr"><span class="lb"><b>Starts</b></span><span class="vl lk">Wed 14 Oct · 13:00</span></div>
<div style="position:relative;padding:8px 16px 16px;display:flex;gap:6px"><div style="position:absolute;left:12px;right:12px;top:84px;height:38px;border-radius:10px;background:var(--sunk);z-index:0"></div><div style="position:relative;display:flex;gap:6px;width:100%;z-index:1">${wheel(['Mon 12', 'Tue 13', 'Wed 14', 'Thu 15', 'Fri 16'], 2, 2.2)}${wheel(['11', '12', '13', '14', '15'], 2, 1)}${wheel(['30', '45', '00', '15', '30'], 2, 1)}</div></div>
<div class="fr"><span class="lb"><b>Ends</b></span><span class="vl">14:00 · 1 h</span></div></div>
<div class="row" style="padding:12px 20px;gap:8px"><span class="chip">+30 min</span><span class="chip">+1 h</span><span class="chip">+2 h</span><span class="sp"></span><span class="mut sm13">Snaps to 5 min</span></div>`;

const E4 = E2 + sheet(`<div class="sh1">Add to calendar</div>${glabel('Google · casprine.001@gmail.com')}${calRow('cobalt', 'Personal', 'Default')}${calRow('mauve', 'Side project', 'Writable')}${glabel('Microsoft · casper@northwind.com')}${calRow('clay', 'Work', 'Writable')}${glabel('iCloud')}${calRow('verdigris', 'Home', 'Writable')}${calRow('slate', 'UK holidays', 'Read only, can’t add events', 'opacity:.4')}`, 560);

const scope = `<div class="sh1">Edit repeating event</div><div class="sh2">“Standup” repeats every weekday. You moved it to 10:15.</div><div class="stack" style="margin-top:18px">${[['This event', 'Only Tue 13 Oct changes'], ['This and following', 'From Tue 13 Oct on. Earlier ones stay'], ['All events', 'Every event in the series']].map(([a, b]) => `<div class="btn" style="height:62px;flex-direction:column;align-items:flex-start;justify-content:center;gap:1px;padding:0 16px"><span>${a}</span><span class="mut" style="font-size:12.5px;font-weight:400">${b}</span></div>`).join('')}<div class="btn ghost">Cancel</div></div>`;
const E5 = `${navbar('x', 'Edit event', saveBtn())}${ctitle('Standup', false)}${calChip('clay', 'Work')}${formRows({ s: 'Tue 13 Oct · 10:15', e: '10:45 · 30 min', rep: 'Every weekday', vid: 'Microsoft Teams', vidv: '<span class="lnk">Remove</span>' })}${sheet(scope, 460)}`;

const E6 = E1 + sheet(`<div class="sh1">Add video call</div><div class="sh2">One tap, created when you save.</div><div class="stack" style="margin-top:16px"><div class="btn" style="justify-content:flex-start;padding:0 16px;gap:14px">${ico('video', 22)}Google Meet</div><div class="btn" style="justify-content:flex-start;padding:0 16px;gap:14px">${ico('link', 22)}Paste a link<span class="mut sm13" style="font-weight:400">Zoom, Webex, Teams</span></div></div><p class="mut sm13" style="margin-top:14px">Microsoft Teams is only offered for Work calendars. Outlook.com calendars can’t create Teams meetings, so it stays hidden.</p>`, 380);

const E7 = `${navbar('x', 'New event', `<span class="btn brass sm" style="gap:8px"><i class="live" style="background:#16221D;animation:none"></i>Saving</span>`)}<div style="opacity:.55">${ctitle('Lunch with Sam', false)}${calChip('cobalt', 'Personal')}${formRows({ loc: 'Kōya, 49 Frith St', vid: 'Google Meet', vidv: '' })}</div>${banner('nf', '', 'Saving to Google Calendar first, then your agenda updates.')}`;
const E8 = `${navbar('x', 'New event', saveBtn('Try again'))}${banner('er', 'Google didn’t save this event', 'The request timed out. Nothing was saved. Your edits are still here.', '')}${ctitle('Lunch with Sam', false)}${calChip('cobalt', 'Personal')}${formRows({ loc: 'Kōya, 49 Frith St', vid: 'Google Meet', vidv: '' })}<div style="padding:14px 16px"><div class="btn ghost">Choose another calendar</div></div>`;
const E9 = D2 + sheet(`<div class="row cap" style="gap:8px">${ico('refresh', 16)}Updated elsewhere</div><div class="sh1" style="margin-top:8px">“Roadmap review” changed while you were editing</div><div class="sh2">Yaw moved it on another device. Showing the latest version.</div><div class="fl sunk" style="margin:16px 0 0"><div class="fr"><span class="lb"><small>You</small><b>11:00–12:00</b></span><span class="lb"><small>Now on Google</small><b style="color:var(--brassInk)">14:00–15:00</b></span></div></div><div class="stack" style="margin-top:18px"><div class="btn pri">Use the latest</div><div class="btn">Apply my changes again</div></div>`, 420);

// ----- F RSVP -----
const F1 = D1.replace(rsvpBar(), '') + sheet(`<div class="sh1">Roadmap review</div><div class="sh2">Tue 13 Oct, 11:00. Reply goes to Yaw Boateng.</div><div style="display:grid;grid-template-columns:repeat(3,1fr);gap:8px;margin-top:18px">${['Yes', 'Maybe', 'No'].map((x, i) => `<div class="btn ${i === 0 ? 'pri' : ''}" style="height:56px">${x}</div>`).join('')}</div><div class="in" style="margin-top:14px;height:50px"><span class="ph2">Add a note for Yaw (optional)</span></div><div class="btn brass" style="margin-top:14px">Send reply</div><p class="mut sm13" style="margin-top:10px;text-align:center">Sent through Microsoft. Others see your answer.</p>`, 440);
const F2 = `${topbar('Today', 'Tuesday 13 October')}<div style="height:12px"></div>${daylight('09:52', [{ s: '10:00', e: '10:30', c: 'clay' }, { s: '11:00', e: '12:00', c: 'clay' }])}
${dayHead('Today', 'later', '5 more', true)}${ev({ s: '11:00', e: '12:00', t: 'Roadmap review', c: 'clay', cal: 'Work', link: 'Teams', rsvp: 'maybe', state: 'tent' })}${ev({ s: '11:30', e: '12:30', t: 'Dentist, Dr Owusu', c: 'cobalt', cal: 'Personal' })}${ev({ s: '13:00', e: '14:00', t: 'Lunch with Sam', c: 'cobalt', cal: 'Personal' })}${toast('Replied Maybe to Yaw', 'Undo', 'check', 100)}${dock()}`;
const F4 = `${navbar('x', 'Reply')}<div style="height:12px"></div>${banner('er', 'Reply not sent', 'Microsoft returned an error. You are still shown as “Needs reply”.', 'Try again')}${fields([['users', 'Roadmap review', null, { sub: 'Tue 13 Oct · 11:00 · Yaw Boateng' }], ['', 'Your answer', 'Maybe'], ['note', 'Note', 'Can I join after 11:15?']], 'margin-top:14px')}<div style="padding:16px" class="stack"><div class="btn brass">Send again</div><div class="btn ghost">Discard reply</div></div>`;

const miniEv = (o) => ev(o).replace('padding:11px 20px', '');
const RS = `<div class="cap" style="margin-bottom:6px">Agenda rows</div><div style="margin:0 -20px">
${ev({ s: '11:00', e: '12:00', t: 'Roadmap review', c: 'clay', cal: 'Work', rsvp: 'respond' })}
${ev({ s: '11:00', e: '12:00', t: 'Design crit', c: 'mauve', cal: 'Side project', rsvp: 'maybe', state: 'tent' })}
${ev({ s: '15:00', e: '15:30', t: 'Vendor call', c: 'clay', cal: 'Work', rsvp: 'declined', state: 'dec' })}
${ev({ s: '13:00', e: '14:00', t: 'Lunch with Sam', c: 'cobalt', cal: 'Personal' })}
${ev({ s: '18:30', e: '19:30', t: 'Parents’ evening', c: 'verdigris', cal: 'Family · iCloud' })}</div>
<div class="row" style="gap:18px;flex-wrap:wrap;margin-top:6px" ><span class="mut sm13"><b style="color:var(--ink);font-weight:500">Needs reply</b> brass tag</span><span class="mut sm13"><b style="color:var(--ink);font-weight:500">Maybe</b> hatched square</span><span class="mut sm13"><b style="color:var(--ink);font-weight:500">Declined</b> struck through</span><span class="mut sm13"><b style="color:var(--ink);font-weight:500">Accepted</b> says nothing</span></div>`;

const cardsD = [
  phoneCard({ id: 'D1', title: 'Event detail, invitation', state: 'Needs reply', us: ['US-3.4', 'US-5.4', 'US-5.6'], screen: D1, notes: ['Title is the hero; time follows in serif with a mono duration and an italic “in 1 h 8 min”.', 'Join sits right under the time with a copy-link button beside it (US-5.6).', 'Place taps through to Maps. People are summarised; tap for statuses.', 'The reply bar is pinned only while a reply is pending, then disappears.', '<b>Motion:</b> the row’s title and time travel to this screen; the sheet notch leads the eye down.']}),
  phoneCard({ id: 'D2', title: 'Event detail, organiser', state: 'Live', us: ['US-3.4', 'US-5.2', 'US-5.6'], screen: D2, notes: ['Recurrence is stated in words (“Every weekday”).', 'Attendees with each status; the chip colour is a tint, never the only signal.', 'Edit and More sit in the nav. “You organise this” is stated in the calendar line.']}),
  phoneCard({ id: 'D3', title: 'Read-only iCloud invitation', state: 'Read only', us: ['US-5.4', 'US-3.4'], screen: D3, notes: ['No reply buttons and no edit. The banner says why and where to change it.', 'Edit is shown disabled rather than hidden so the layout is stable.']}),
  phoneCard({ id: 'D4', title: 'Other time zone, link in notes', state: 'Populated', us: ['US-3.4', 'US-5.6', 'US-6.5'], screen: D4, notes: ['A brass strip shows the other zone only when it differs from yours.', 'No structured link, so Join comes from the notes (“Link found in the notes”) (US-5.6).']}),
  phoneCard({ id: 'D5', title: 'Actions', state: 'Sheet', us: ['US-5.7', 'US-5.3'], screen: D5, notes: ['Reached from More. Edit, Duplicate, Move, Copy link, Delete. Delete is last and set apart in madder.']}),
  phoneCard({ id: 'D6', title: 'Move to another calendar', state: 'Sheet', us: ['US-5.7'], screen: D6, notes: ['Same account moves in place. Other accounts copy then delete, with a plain warning (US-5.7).', 'Read-only calendars are listed but dimmed.']}),
  phoneCard({ id: 'D7', title: 'Delete event', state: 'Confirm', us: ['US-5.3'], screen: D7, notes: ['One sentence on what is removed and where. Primary action is the destructive one in madder tint; Cancel is neutral.']}),
  phoneCard({ id: 'D8', title: 'Delete repeating event', state: 'Choice', us: ['US-5.3', 'US-5.2'], screen: D8, notes: ['Three scopes, same wording as edit: this event, this and following, all events.']}),
  phoneCard({ id: 'D9', title: 'Delete as organiser', state: 'Confirm', us: ['US-5.3'], screen: D9, notes: ['The button states the consequence: “Delete and notify 5 people”.']}),
].join('');

const cardsE = [
  phoneCard({ id: 'E1', title: 'New event', state: 'Default', us: ['US-5.1', 'US-4.3'], screen: E1, notes: ['Title first, large. The calendar chip shows the default calendar (US-4.3).', 'Save stays disabled until there is a title. The alfather link offers the one-line route (see quick add).']}),
  phoneCard({ id: 'E2', title: 'New event, filled', state: 'Ready', us: ['US-5.1', 'US-5.5'], screen: E2, notes: ['A Meet link shows as “created when you save” so the user knows it is not live yet (US-5.5).', 'Reminder default 10 minutes; tap to override per event (US-6.2).']}),
  phoneCard({ id: 'E3', title: 'Date and time picker', state: 'Inline', us: ['US-5.1'], screen: E3, notes: ['Opens inline in the form. Day, hour and minute wheels, with quick duration chips under Ends.', 'The selection band tracks the finger with a light tick each step.']}),
  phoneCard({ id: 'E4', title: 'Choose calendar', state: 'Sheet', us: ['US-5.1', 'US-4.3', 'US-2.1'], screen: E4, notes: ['Grouped by account. Read-only calendars are dimmed with the reason.']}),
  phoneCard({ id: 'E5', title: 'Edit repeating event', state: 'Choice', us: ['US-5.2'], screen: E5, notes: ['Appears on Save, after the user changes a repeating event. This event, This and following, All events, each with the effect in a line.']}),
  phoneCard({ id: 'E6', title: 'Add video call', state: 'Sheet', us: ['US-5.5'], screen: E6, notes: ['Google Meet for Google calendars. Teams is hidden unless the calendar supports it (work accounts only).', 'Paste a link covers Zoom and Webex.']}),
  phoneCard({ id: 'E7', title: 'Saving', state: 'Loading', us: ['US-5.1'], screen: E7, notes: ['The server writes to the provider first, then updates the mirror. The form dims and the button shows progress; the event appears in the agenda when it lands.']}),
  phoneCard({ id: 'E8', title: 'Provider write failed', state: 'Error', us: ['US-5.1'], screen: E8, notes: ['Plain statement, what was not saved, what is kept, what to do. Nothing half saved.']}),
  phoneCard({ id: 'E9', title: 'Changed elsewhere', state: 'Conflict', us: ['US-5.2'], screen: E9, notes: ['On an etag mismatch the app refetches and shows both versions side by side. Default is the latest.']}),
].join('');

const cardsF = [
  phoneCard({ id: 'F1', title: 'Reply to an invitation', state: 'Sheet', us: ['US-5.4'], screen: F1, notes: ['Yes, Maybe, No as big equal buttons. Optional note to the organiser. Send is explicit.', 'A one-line footer states which provider carries the reply.']}),
  phoneCard({ id: 'F2', title: 'Replied', state: 'Toast', us: ['US-5.4'], screen: F2, notes: ['The row switches to its Maybe look at once. Toast with Undo for 5 seconds.']}),
  phoneCard({ id: 'F4', title: 'Reply not sent', state: 'Error', us: ['US-5.4'], screen: F4, notes: ['The note is kept. Retry, or discard to leave the status unchanged.']}),
  boardCard({ id: 'F3', title: 'How RSVP shows in the agenda', state: 'Component', us: ['US-5.4', 'US-3.2'], inner: RS, w: 420, notes: ['Only unanswered invitations ask for attention. Accepted events say nothing.', 'iCloud invitations show their status but never a Respond tag.']}),
].join('');

export default page({
  title: 'Gnomon Hi-fi: Events',
  eyebrow: 'Gnomon · Hi-fi 3 of 4 · Event detail · Create and edit · RSVP',
  h1: 'Open it, change it, <i>answer it.</i>',
  lede: 'Everything you can do to a single event, and everything that can go wrong while doing it. All names and places are sample content.',
  nav: [['d', 'Event detail and actions'], ['e', 'Create and edit'], ['f', 'RSVP']],
  sections: section('d', 'Epics E3, E5 · US-3.4, 5.3, 5.6, 5.7', 'Event detail and actions', 'Detail is a place you pass through. Actions come from one sheet.', cardsD) + section('e', 'Epic E5 · US-5.1, 5.2, 5.5', 'Create and edit', 'One form for new and existing events. Recurring edits ask which events they affect.', cardsE) + section('f', 'Epic E5 · US-5.4', 'RSVP', 'Reply from the agenda or the detail. iCloud is read only.', cardsF),
});
