import { page, section, phoneCard, boardCard, ico, af, topbar, navbar, sheet, toast, banner, fields, glabel, sw, daylight, seg } from './lib.mjs';

export const mark = (s = 56) => `<svg width="${s}" height="${s}" viewBox="0 0 56 56" aria-hidden="true"><circle cx="28" cy="28" r="25" fill="none" stroke="currentColor" stroke-width="2"/><path d="M28 28 L52 36 A25 25 0 0 1 44 46 Z" fill="currentColor" opacity=".16"/><path d="M28 9 L36 38 H20 Z" fill="var(--brass)"/><circle cx="28" cy="28" r="2.5" fill="currentColor"/></svg>`;
const mono = (t, c) => `<span style="width:40px;height:40px;border-radius:12px;background:var(--sunk);display:inline-flex;align-items:center;justify-content:center;font-family:var(--serif);font-size:22px;flex:none">${t}</span>`;
const btn = (t, k = 'pri', ic = '') => `<div class="btn ${k}">${ic}${t}</div>`;
const pad = (inner, p = '0 20px') => `<div style="padding:${p}">${inner}</div>`;
const lab = (t) => `<div style="font-size:13px;color:var(--ink2);margin:16px 0 6px">${t}</div>`;
const inp = (v, cls = '', ph = false) => `<div class="in ${cls}"><span class="${ph ? 'ph2' : ''}">${v}</span></div>`;
const spin = (c = 'var(--brass)') => `<svg width="22" height="22" viewBox="0 0 24 24" aria-hidden="true" style="animation:spn 1s linear infinite"><circle cx="12" cy="12" r="9" fill="none" stroke="${c}" stroke-opacity=".25" stroke-width="2.5"/><path d="M12 3a9 9 0 0 1 9 9" fill="none" stroke="${c}" stroke-width="2.5" stroke-linecap="round"/></svg>`;

const A1 = `<div style="padding:150px 28px 0">${mark(64)}<div class="hero" style="margin-top:26px;font-size:46px">Your calendars,<br>in light and <i>shade.</i></div><p class="mut" style="margin-top:18px;font-size:16.5px;max-width:30ch">Gnomon reads Google, Outlook and iCloud through your own home server. Nothing leaves your tailnet.</p></div>
<div style="position:absolute;left:20px;right:20px;bottom:44px" class="stack">${btn('Connect to your server', 'pri')}<div class="mut sm13" style="text-align:center">Needs Tailscale on this phone</div></div>`;

const A2 = `${navbar('back', 'Your server')}${pad(`<div class="sub" style="font-size:15px;margin-top:10px;color:var(--ink2)">Enter the address of the hub and an access token from its settings.</div>${lab('Server address')}${inp('https://hub.tail3a9f.ts.net', 'focus')}${lab('Access token')}${inp('••••••••••••••••••••', '')}<div class="mut sm13" style="margin-top:8px">Tokens are made on the server under API tokens. This phone gets its own.</div>`)}
${banner('nf', '', 'Tailscale is connected. The hub is reachable.', '', 'check').replace('bn nf', 'bn ok')}
<div style="position:absolute;left:20px;right:20px;bottom:44px">${btn('Pair this phone', 'brass')}</div>`;

const A2b = `${navbar('back', 'Your server')}${pad(`<div style="margin-top:10px;color:var(--ink2)">Enter the address of the hub and an access token from its settings.</div>${lab('Server address')}${inp('https://hub.tail3a9f.ts.net', 'err')}${lab('Access token')}${inp('••••••••••••••••••••')}`)}
${banner('er', 'Can’t reach hub.tail3a9f.ts.net', 'The request timed out after 10 seconds.')}
<div class="fl" style="margin-top:14px">${[['Is Tailscale on for this phone?', 'Open Tailscale and check it says Connected'], ['Is the server running?', 'Check the hub container on your home server']].map(([a, b]) => `<div class="fr"><span class="ic">${ico('info')}</span><span class="lb"><b>${a}</b><small>${b}</small></span></div>`).join('')}</div>
<div style="position:absolute;left:20px;right:20px;bottom:44px" class="stack">${btn('Try again', 'pri', ico('refresh'))}</div>`;

const prov = (m, n, d) => `<div class="fl" style="margin-top:10px"><div class="fr" style="min-height:76px">${mono(m)}<span class="lb"><b style="font-size:17px">${n}</b><small>${d}</small></span>${ico('chevR', 18)}</div></div>`;
const A3 = `${navbar('x', 'Add an account')}<div style="padding:8px 0 0">${prov('G', 'Google', 'Sign in with your Google account')}${prov('M', 'Microsoft', 'Outlook.com, Hotmail or Microsoft 365')}${prov('☁', 'iCloud', 'Needs an app-specific password')}</div>
${pad(`<p class="mut sm13" style="margin-top:20px">You can connect more than one of each. Calendars appear together in your agenda, and you choose which to show.</p>`)}`;

const A4 = `<div style="position:absolute;inset:0;background:var(--scrim)"></div><div style="position:absolute;left:0;right:0;top:48px;bottom:0;background:var(--surface);border-radius:26px 26px 0 0;overflow:hidden">
<div class="row" style="padding:12px 16px;border-bottom:1px solid var(--line)"><span class="lnk" style="text-decoration:none">Done</span><span class="sp" style="text-align:center;font-size:13px" ><span class="row" style="justify-content:center;gap:6px">${ico('lock', 13)}hub.tail3a9f.ts.net</span></span><span style="width:40px"></span></div>
<div style="padding:34px 28px;display:grid;gap:16px;justify-items:start"><div style="width:48px;height:48px;border-radius:50%;background:var(--sunk)"></div><div class="sk" style="width:220px;height:26px"></div><div class="sk" style="width:260px;height:14px"></div><div class="bn nf" style="margin:10px 0 0;width:100%">${ico('info')}<div class="sp"><b>Provider sign in page</b>This is the system browser sheet. It shows Google, Microsoft or iCloud, not this app. The first time, Google warns the app is unverified. That is expected for your own app.</div></div><div class="sk" style="width:100%;height:50px;border-radius:14px"></div><div class="sk" style="width:100%;height:50px;border-radius:14px"></div></div></div>`;

const A4b = `<div style="padding:260px 28px 0;text-align:center;display:grid;justify-items:center;gap:14px">${spin()}<div class="sh1" style="font-family:var(--serif);font-size:26px">Finishing sign in</div><div class="mut" style="max-width:26ch">Your server is exchanging the code and storing the token, encrypted.</div></div>`;

const calL = (c, n, on, def) => `<div class="fr"><i class="sq lg" style="--c:var(--${c})"></i><span class="lb"><b>${n}</b>${def ? `<small>Default for new events</small>` : ''}</span>${sw(on)}</div>`;
const A5 = `${navbar('x', 'Connected')}<div style="padding:8px 20px 0"><div class="row" style="gap:12px"><span class="ib" style="background:var(--verdT);color:var(--verd)">${ico('check')}</span><div><div class="serif" style="font-size:24px">Google</div><div class="mut sm13">casprine.001@gmail.com</div></div></div></div>
<div class="fl" style="margin-top:18px;padding:14px 16px"><div class="row" style="justify-content:space-between;margin-bottom:10px"><span style="font-weight:500">Loading your events</span><span class="mono mut sm13">62%</span></div><div style="height:8px;border-radius:4px;background:var(--sunk);overflow:hidden"><div style="width:62%;height:100%;background:var(--brass)"></div></div><div class="mut sm13" style="margin-top:8px">30 days back to 1 year ahead. You can start using the agenda now.</div></div>
${glabel('3 calendars found')}<div class="fl">${calL('cobalt', 'Personal', 1, 1)}${calL('mauve', 'Side project', 1)}${calL('slate', 'Birthdays', 0)}</div>
<div style="position:absolute;left:20px;right:20px;bottom:44px" class="stack">${btn('Open my agenda', 'brass')}${btn('Add another account', 'pri')}</div>`;

const A6 = `${navbar('back', 'iCloud')}${pad(`<div style="margin-top:10px;color:var(--ink2)">Apple only allows other apps in with an app-specific password. Your Apple ID password won’t work.</div>${lab('Apple ID')}${inp('caspar@icloud.com', 'focus')}${lab('App-specific password')}${inp('<span class="ph2">xxxx-xxxx-xxxx-xxxx</span>')}<div class="lnk sm13" style="margin-top:10px;display:inline-block">How to create one at account.apple.com</div>`)}
<div class="fl" style="margin-top:18px">${[['Sign in at account.apple.com'], ['Open Sign-In and Security'], ['Choose App-Specific Passwords and add one named Gnomon']].map(([t], i) => `<div class="fr"><span class="mono mut" style="width:18px">${i + 1}</span><span class="lb" style="font-size:14px">${t}</span></div>`).join('')}</div>
<div style="position:absolute;left:20px;right:20px;bottom:44px">${btn('Connect iCloud', 'brass')}</div>`;

const A6b = `${navbar('back', 'iCloud')}${pad(`<div style="margin-top:10px;color:var(--ink2)">Apple only allows other apps in with an app-specific password.</div>${lab('Apple ID')}${inp('caspar@icloud.com')}${lab('App-specific password')}${inp('abcd-efgh-ijkl-mnop', 'err')}<div style="color:var(--madder);font-size:13.5px;margin-top:8px;display:flex;gap:6px">${ico('alert', 16)}Apple didn’t accept this password. Make a new app-specific password and paste it here.</div>`)}
${banner('nf', '', 'Nothing was saved. Your Apple ID details are only stored once Apple accepts them.')}
<div style="position:absolute;left:20px;right:20px;bottom:44px">${btn('Try again', 'brass')}</div>`;

const A6c = `${navbar('back', 'iCloud')}${pad(`${lab('Apple ID')}${inp('caspar@icloud.com')}${lab('App-specific password')}${inp('••••-••••-••••-••••')}`)}
<div class="fl" style="margin-top:20px;padding:16px;display:flex;gap:14px;align-items:center">${spin()}<div><b style="font-weight:500">Looking for calendars</b><div class="mut sm13">Checking with iCloud over CalDAV</div></div></div>
<div style="position:absolute;left:20px;right:20px;bottom:44px">${btn('Connecting…', 'brass').replace('btn brass', 'btn brass off')}</div>`;

const A7 = `${navbar('back', 'Microsoft')}<div style="padding:30px 24px 0"><div class="row cap" style="gap:8px;color:var(--madder)">${ico('lock', 15)}Needs admin approval</div><div class="hero" style="margin-top:14px;font-size:34px">Northwind needs to allow Gnomon first.</div><p class="mut" style="margin-top:14px;max-width:34ch">Your organisation blocks apps it hasn’t approved, so signing in as casper@northwind.com stopped at Microsoft’s consent step.</p></div>
<div class="fl" style="margin-top:22px">${[['copy', 'Copy a request for your admin', 'App name, ID and the permissions it asks for'], ['link', 'Add as a read-only link instead', 'Paste the calendar’s ICS link. Updates, no editing'], ['refresh', 'Try again', 'After your admin has approved']].map(([i, a, b]) => `<div class="fr" style="min-height:64px"><span class="ic">${ico(i)}</span><span class="lb"><b>${a}</b><small>${b}</small></span>${ico('chevR', 16)}</div>`).join('')}</div>`;

// ---- H accounts and errors ----
const acct = (m, n, e, st, kind, meta) => `<div class="fr" style="min-height:76px">${mono(m)}<span class="lb"><b>${n}</b><small>${e}</small><small style="margin-top:2px;${kind === 'er' ? 'color:var(--madder)' : kind === 'br' ? 'color:var(--brassInk)' : ''}">${meta}</small></span><span class="chip ${kind}" style="height:26px;font-size:12px">${st}</span></div>`;
const H1 = `${navbar('back', 'Accounts')}<div class="fl" style="margin-top:10px">${acct('G', 'Google', 'casprine.001@gmail.com', 'OK', 'ok', 'Synced 1 min ago · 3 calendars')}${acct('G', 'Google', 'caspar@loomwork.dev', 'OK', 'ok', 'Synced 1 min ago · 2 calendars')}${acct('M', 'Microsoft', 'casper@northwind.com', 'Sign in', 'er', 'Needs you to sign in again · last sync 3 days ago')}${acct('☁', 'iCloud', 'caspar@icloud.com', 'Syncing', 'br', 'Syncing now · 4 calendars')}</div>
<div style="padding:16px" class="stack">${btn('Add an account', 'pri', ico('plus'))}</div>${pad(`<p class="mut sm13">Removing an account deletes its sign-in and its copy of your events from the server. Nothing is deleted at the provider.</p>`)}`;

const H2 = `${topbar('Today', 'Tuesday 13 October', { sync: '<i class="sdot err"></i>' })}${seg(['Agenda', 'Day', '3 days', 'Week'], 0)}
${banner('er', 'Work needs you to sign in again', 'Its events may be out of date since Saturday.', 'Reconnect')}
${daylight('09:52', [{ s: '10:00', e: '10:30', c: 'clay' }, { s: '13:00', e: '14:00', c: 'cobalt' }])}
<div class="up" style="margin-top:14px"><div class="k">Up next · in 1 h 8 min</div><div class="t" style="font-size:26px">Lunch with Sam</div><div class="m"><span>13:00–14:00</span><span>Personal</span><span>Kōya, Frith St</span></div></div>
<div class="dh"><span class="d">Later<i>today</i></span><span class="n">3 more</span></div><div style="padding:8px 20px;color:var(--ink2);font-size:13.5px">Work events are hidden until you sign in again.</div>
<div class="fade"></div><div class="dock"><div class="dk">${ico('spark', 20)}<span>Add with alfather</span></div><span class="fab">${ico('plus', 24)}</span></div>`;

const H3 = `${navbar('back', 'Work')}<div style="padding:8px 20px 0"><div class="row" style="gap:12px">${mono('M')}<div><div class="serif" style="font-size:24px">Microsoft</div><div class="mut sm13">casper@northwind.com</div></div></div></div>
${banner('er', 'Signed out', 'Microsoft revoked this sign in on Saturday 10 October. Your calendars are paused.')}
<div class="fl" style="margin-top:14px">${[['', 'Status', '<span class="chip er" style="height:24px;font-size:12px">needs_reauth</span>'], ['', 'Last successful sync', '3 days ago'], ['', 'Calendars', '3 · Work, Team, Flights'], ['', 'Events kept on the server', '214']].map(([, a, b]) => `<div class="fr"><span class="lb"><b style="font-weight:400">${a}</b></span><span class="vl">${b}</span></div>`).join('')}</div>
<div style="position:absolute;left:20px;right:20px;bottom:44px" class="stack">${btn('Reconnect', 'brass', ico('refresh'))}${btn('Remove account', 'dng', ico('trash'))}</div>`;

const H4 = H3 + sheet(`<div class="sh1">Sign in as casper@northwind.com</div><div class="sh2">Use the same account. A different one would be added as a new account.</div><div class="stack" style="margin-top:20px">${btn('Continue to Microsoft', 'brass')}${btn('Cancel')}</div>`, 300);
const H5 = H3 + sheet(`<div class="sh1">Remove “Work”?</div><div class="sh2">The sign in and 214 events are deleted from your server. Nothing changes in Outlook. You can connect it again later.</div><div class="stack" style="margin-top:20px">${btn('Remove account', 'dng')}${btn('Keep it')}</div>`, 320);
const H6 = `${navbar('back', 'iCloud')}${banner('er', 'iCloud signed this app out', 'The app-specific password was revoked or changed. Make a new one to keep syncing.')}${pad(`${lab('Apple ID')}${inp('caspar@icloud.com')}${lab('New app-specific password')}${inp('<span class="ph2">xxxx-xxxx-xxxx-xxxx</span>', 'focus')}`)}
<div style="position:absolute;left:20px;right:20px;bottom:44px">${btn('Reconnect iCloud', 'brass')}</div>`;
const H3b = `${navbar('back', 'Work')}<div style="padding:8px 20px 0"><div class="row" style="gap:12px">${mono('M')}<div><div class="serif" style="font-size:24px">Microsoft</div><div class="mut sm13">casper@northwind.com</div></div></div></div>${banner('ok', 'Signed in again', 'Work is back. Events from the last 3 days are catching up.')}`;

const ERR = [
  ['Needs sign in', 'er', 'Work needs you to sign in again.', 'Banner on agenda. Reconnect.', 'US-1.5'],
  ['Admin approval', 'er', 'Northwind needs to allow Gnomon first.', 'Copy request, ICS link, retry.', 'US-1.3'],
  ['Wrong iCloud password', 'er', 'Apple didn’t accept this password.', 'Inline. Nothing saved.', 'US-1.4'],
  ['Save failed', 'er', 'Google didn’t save this event. Nothing was saved.', 'Inline banner. Edits kept.', 'US-5.1'],
  ['Move failed', 'er', 'Design crit is back at 14:30. Google rejected the change.', 'Toast with Retry. Rolls back.', 'US-6.3'],
  ['Reply failed', 'er', 'Reply not sent. You are still shown as Needs reply.', 'Banner. Note kept.', 'US-5.4'],
  ['Changed elsewhere', 'br', 'Yaw moved it on another device. Showing the latest.', 'Sheet. Diff.', 'US-5.2'],
  ['Read only', 'nf', 'This calendar is read only.', 'Toast, short haptic.', 'US-2.1'],
  ['Server unreachable', 'er', 'Can’t reach the hub. Is Tailscale on?', 'Banner in agenda. Cached data shown.', 'US-0.1'],
  ['Sync backing off', 'br', 'iCloud is slow to answer. Trying again in 4 min.', 'Account row and status.', 'US-2.4'],
];
const ERRB = `<table class="t"><thead><tr><th>Case</th><th>What the user reads</th><th>Where, what next</th><th>Story</th></tr></thead><tbody>${ERR.map((r) => `<tr><td><span class="chip ${r[1]}" style="height:24px;font-size:12px;white-space:nowrap">${r[0]}</span></td><td>${r[2]}</td><td class="mut">${r[3]}</td><td class="mono mut">${r[4]}</td></tr>`).join('')}</tbody></table>`;

const cardsA = [
  phoneCard({ id: 'A1', title: 'Welcome, no accounts', state: 'Empty', us: ['US-0.1', 'US-1.1'], screen: A1, notes: ['The first and the emptiest screen. The mark is the sundial: a brass gnomon casting a shadow.', 'One action. The Tailscale requirement is stated before it can fail.']}),
  phoneCard({ id: 'A2', title: 'Pair with your server', state: 'Default', us: ['US-0.1', 'US-7.1'], screen: A2, notes: ['Address and token. The reachability check runs as the address is typed and turns green.', 'The token is a device token from the server’s API tokens screen. <b>The plan has no story for pairing the app itself</b>; see README.']}),
  phoneCard({ id: 'A2b', title: 'Server unreachable', state: 'Error', us: ['US-0.1'], screen: A2b, notes: ['Says what happened and gives a two item checklist, then a single retry.']}),
  phoneCard({ id: 'A3', title: 'Add an account', state: 'Choice', us: ['US-1.1', 'US-1.2', 'US-1.3', 'US-1.4'], screen: A3, notes: ['Three providers. Monogram tiles instead of provider logos.', 'The same screen is reached from Accounts and from the empty agenda.']}),
  phoneCard({ id: 'A4', title: 'Provider sign in', state: 'System sheet', us: ['US-1.1', 'US-1.3'], screen: A4, notes: ['The system browser sheet (ASWebAuthenticationSession) shows the provider, not the app. This frame is a placeholder for it.', 'Google’s one time “unverified app” warning is expected (US-1.1). The account chooser shows every time (US-1.2).']}),
  phoneCard({ id: 'A4b', title: 'Finishing sign in', state: 'Loading', us: ['US-1.1'], screen: A4b, notes: ['After the redirect to calendarapp://connected while the server stores the token.']}),
  phoneCard({ id: 'A5', title: 'Connected, loading events', state: 'Success', us: ['US-1.1', 'US-2.1', 'US-2.2', 'US-4.1'], screen: A5, notes: ['Shows the account email, a real progress bar, and the calendars found. Toggles decide what shows (US-4.1).', 'The agenda is usable before the progress reaches 100%.']}),
  phoneCard({ id: 'A6', title: 'iCloud app password', state: 'Default', us: ['US-1.4'], screen: A6, notes: ['Says up front why a normal password won’t work, with the three steps in the screen.']}),
  phoneCard({ id: 'A6c', title: 'Checking with iCloud', state: 'Loading', us: ['US-1.4'], screen: A6c, notes: ['The server discovers calendars over CalDAV before saving anything.']}),
  phoneCard({ id: 'A6b', title: 'Wrong password', state: 'Error', us: ['US-1.4'], screen: A6b, notes: ['Inline under the field, plus a note that nothing was saved.']}),
  phoneCard({ id: 'A7', title: 'Needs admin approval', state: 'Error', us: ['US-1.3'], screen: A7, notes: ['Work tenants can block unverified apps. Three ways out: ask the admin with a prepared request, fall back to a read only ICS link, or retry.', '<b>The ICS fallback comes from the plan’s risks table and has no user story.</b> See README.']}),
].join('');

const cardsH = [
  phoneCard({ id: 'H1', title: 'Accounts', state: 'Mixed', us: ['US-1.5', 'US-2.4'], screen: H1, notes: ['Provider, email, state and last sync for each. One row is in trouble and says so in words and in madder.', 'The state chip is a tint plus text.']}),
  phoneCard({ id: 'H2', title: 'Sign in needed banner', state: 'Error', us: ['US-1.5'], screen: H2, notes: ['A banner under the view switcher. The sync dot turns madder. Events from that account are hidden until sign in, not shown stale.']}),
  phoneCard({ id: 'H3', title: 'Account detail', state: 'needs_reauth', us: ['US-1.5'], screen: H3, notes: ['Status, last sync, calendars and events kept. Reconnect is the primary action.']}),
  phoneCard({ id: 'H4', title: 'Reconnect', state: 'Sheet', us: ['US-1.5', 'US-1.1'], screen: H4, notes: ['Reruns provider sign in. Signing in with the same account updates it rather than adding another.']}),
  phoneCard({ id: 'H3b', title: 'Reconnected', state: 'Success', us: ['US-1.5'], screen: H3b, notes: ['Banner flips to green. Events catch up.']}),
  phoneCard({ id: 'H5', title: 'Remove account', state: 'Confirm', us: ['US-1.5'], screen: H5, notes: ['States exactly what is deleted and what is not.']}),
  phoneCard({ id: 'H6', title: 'iCloud password revoked', state: 'Error', us: ['US-1.5', 'US-1.4'], screen: H6, notes: ['A 401 from iCloud marks the account needs_reauth. The form is prefilled with the Apple ID.']}),
  boardCard({ id: 'H7', title: 'Every error, in words', state: 'Catalogue', us: ['US-1.3', 'US-1.4', 'US-1.5', 'US-5.1', 'US-2.4'], inner: ERRB, w: 900, notes: ['One voice for all errors: say what happened, what is safe, what to do. No apologies, no codes.']}),
].join('');

export default page({
  title: 'Gnomon Hi-fi: Connect and Accounts',
  eyebrow: 'Gnomon · Hi-fi 1 of 4 · Onboarding and connect accounts · Accounts and errors',
  h1: 'Connect once. <i>Stay in sync.</i>',
  lede: 'Pairing with the home server, connecting Google, Microsoft and iCloud, and the ways each can go wrong. All emails, hosts and counts are sample content.',
  nav: [['a', 'Onboarding and connect'], ['h', 'Accounts and errors']],
  sections: section('a', 'Epics E0, E1 · US-0.1, 1.1 to 1.4', 'Onboarding and connect accounts', 'Pair the phone with the hub, then add accounts. Provider specific problems are handled where they happen.', cardsA) + section('h', 'Epic E1 · US-1.5', 'Accounts and errors', 'Reconnect, remove, and what the app says when something fails.', cardsH),
  extraCss: '@keyframes spn{to{transform:rotate(360deg)}}@media (prefers-reduced-motion:reduce){svg[style*="spn"]{animation:none!important}}',
});
