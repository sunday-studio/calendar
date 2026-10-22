// Gnomon design system: shared tokens, components and page shell.
// Identity: "Your day, in light and shade." Chalk/ink-green/brass, Newsreader + Schibsted Grotesk + DM Mono.

export const FONT_LINK = `<link rel="preconnect" href="https://fonts.googleapis.com"><link rel="preconnect" href="https://fonts.gstatic.com" crossorigin><link href="https://fonts.googleapis.com/css2?family=DM+Mono:wght@400;500&family=Newsreader:ital,opsz,wght@0,6..72,400;0,6..72,500;1,6..72,400;1,6..72,500&family=Schibsted+Grotesk:wght@400;500;600&display=swap" rel="stylesheet">`;

export const LIGHT = `--bg:#EFF1EA;--surface:#F8F9F4;--sunk:#E4E8DE;--line:#D3D8CA;--ink:#16221D;--ink2:#56645B;--ink3:#8E9B93;--brass:#D9A441;--brassInk:#80560A;--brassT:#F3E6C4;--inv:#16221D;--invInk:#EFF1EA;--invDim:#9DB0A4;--madder:#A5361F;--madderT:#F6DDD5;--verd:#176B58;--verdT:#D6EADF;--scrim:rgba(22,34,29,.42);--shade:rgba(22,34,29,.07);--shadow:0 1px 2px rgba(22,34,29,.06),0 12px 28px rgba(22,34,29,.09);
--cobalt:#3E63DD;--rose:#C9507F;--ochre:#B9801A;--mauve:#8458B3;--verdigris:#1F8A74;--slate:#5F7284;--moss:#6A8A2E;--clay:#C4553A;--wp1:#7E968A;--wp2:#2F4339;`;
export const DARK = `--bg:#0E1411;--surface:#161E1A;--sunk:#1E2923;--line:#2B3830;--ink:#E9EEE3;--ink2:#9AA89E;--ink3:#5E6D64;--brass:#E6B54E;--brassInk:#E6B54E;--brassT:#3A2F12;--inv:#E9EEE3;--invInk:#16221D;--invDim:#56645B;--madder:#F0866B;--madderT:#3A1B14;--verd:#54C2A6;--verdT:#11302A;--scrim:rgba(0,0,0,.55);--shade:rgba(0,0,0,.28);--shadow:0 1px 2px rgba(0,0,0,.4),0 12px 28px rgba(0,0,0,.45);
--cobalt:#7C98F2;--rose:#EE86AC;--ochre:#E0A94A;--mauve:#B28CE0;--verdigris:#4FC4AB;--slate:#8FA3B5;--moss:#A3C25E;--clay:#F0866B;--wp1:#22332A;--wp2:#080D0A;`;

export const BASE_CSS = `
:root{${LIGHT}
 --serif:'Newsreader',Georgia,'Times New Roman',serif;--sans:'Schibsted Grotesk',system-ui,-apple-system,'Segoe UI',sans-serif;--mono:'DM Mono',ui-monospace,Menlo,monospace;
 --settle:cubic-bezier(.2,.8,.2,1)}
@media (prefers-color-scheme:dark){:root:not([data-theme="light"]){${DARK}color-scheme:dark}}
:root[data-theme="dark"]{${DARK}color-scheme:dark}
.ph,.bd{${LIGHT}}
.ph[data-t="dark"],.bd[data-t="dark"]{${DARK}}
body{background:var(--bg);color:var(--ink);font-family:var(--sans);font-size:15px;line-height:1.5;-webkit-font-smoothing:antialiased}
*{box-sizing:border-box}
h1,h2,h3,h4,p,ul,ol{margin:0}
ul,ol{padding:0;list-style:none}
.mono{font-family:var(--mono);font-variant-numeric:tabular-nums}
.serif{font-family:var(--serif);font-variant-numeric:lining-nums tabular-nums}

/* ============ phone + board frame ============ */
.pw{--s:.7;width:calc(390px*var(--s));height:calc(844px*var(--s));flex:none}
@media (max-width:520px){.pw{--s:.8}}
.ph{width:390px;height:844px;position:relative;overflow:hidden;background:var(--bg);color:var(--ink);font-family:var(--sans);font-size:15px;line-height:1.35;transform:scale(var(--s,.7));transform-origin:0 0;border-radius:44px;box-shadow:0 0 0 1px var(--line);font-variant-numeric:tabular-nums}
.ph *{box-sizing:border-box}
.bd{background:var(--bg);color:var(--ink);font-family:var(--sans);font-size:15px;line-height:1.35;border-radius:24px;box-shadow:0 0 0 1px var(--line);padding:18px;width:100%;overflow:hidden;font-variant-numeric:tabular-nums}
.bd *{box-sizing:border-box}

/* ============ in-app components ============ */
.ph .top{padding:60px 20px 0;display:flex;align-items:flex-start;justify-content:space-between;gap:12px}
.ph .ttl{font-family:var(--serif);font-size:32px;line-height:1.05;font-weight:400;letter-spacing:-.01em}
.ph .sub{font-size:13px;color:var(--ink2);margin-top:3px}
.ph .nav{padding:60px 16px 0;display:flex;align-items:center;justify-content:space-between;gap:8px}
.ph .nav .nvt{font-family:var(--serif);font-size:22px}
.cap{font-size:12px;letter-spacing:.06em;text-transform:uppercase;color:var(--ink2);font-weight:500}
.ib{width:40px;height:40px;border-radius:12px;background:var(--surface);display:inline-flex;align-items:center;justify-content:center;color:var(--ink);flex:none}
.ib.flat{background:transparent}
.av{width:36px;height:36px;border-radius:50%;background:var(--ink);color:var(--bg);display:inline-flex;align-items:center;justify-content:center;font-size:13px;font-weight:500;flex:none}
.sdot{width:8px;height:8px;border-radius:50%;background:var(--verd);display:inline-block;flex:none}
.sdot.stale{background:var(--brass)}.sdot.off{background:var(--ink3)}.sdot.err{background:var(--madder)}
.seg{margin:14px 20px 0;background:var(--sunk);border-radius:12px;padding:3px;display:flex;gap:2px}
.seg span{flex:1;text-align:center;height:32px;line-height:32px;font-size:13px;border-radius:9px;color:var(--ink2)}
.seg span.on{background:var(--surface);color:var(--ink);font-weight:500;box-shadow:0 1px 2px rgba(22,34,29,.12)}
.dock{position:absolute;left:12px;right:12px;bottom:26px;height:62px;border-radius:22px;background:var(--surface);box-shadow:var(--shadow);display:flex;align-items:center;gap:10px;padding:7px 7px 7px 16px;z-index:5}
.dock .dk{flex:1;color:var(--ink2);font-size:15px;display:flex;align-items:center;gap:9px;min-width:0}
.dock .dk svg{color:var(--brassInk);flex:none}
.fab{width:48px;height:48px;border-radius:15px;background:var(--brass);color:#16221D;display:inline-flex;align-items:center;justify-content:center;flex:none}
.fade{position:absolute;left:0;right:0;bottom:0;height:120px;background:linear-gradient(transparent,var(--bg) 70%);pointer-events:none;z-index:4}
.scroll{position:absolute;left:0;right:0;overflow:hidden}

/* daylight strip */
.dl{margin:16px 20px 0}
.dl .bar{position:relative;height:10px;border-radius:5px;background:var(--brassT);overflow:hidden}
.dl .sh{position:absolute;left:0;top:0;bottom:0;background:var(--ink);opacity:.2}
.dl .tk{position:absolute;top:2px;bottom:2px;width:4px;border-radius:2px;background:var(--c)}
.dl .nw{position:absolute;top:-3px;bottom:-3px;width:2px;background:var(--brass);box-shadow:0 0 0 2px var(--bg)}
.dl .lb{display:flex;justify-content:space-between;font-family:var(--mono);font-size:10px;color:var(--ink3);margin-top:5px}

/* Up next: the dial */
.up{margin:16px 16px 0;border-radius:24px;background:var(--inv);color:var(--invInk);padding:18px 18px 16px;position:relative}
.up .k{font-size:12px;letter-spacing:.06em;text-transform:uppercase;color:var(--invDim);font-weight:500;display:flex;align-items:center;gap:7px}
.up .t{font-family:var(--serif);font-size:30px;line-height:1.08;margin-top:8px;letter-spacing:-.01em;max-width:230px}
.up .m{font-size:13.5px;color:var(--invDim);margin-top:8px;display:flex;gap:6px 12px;flex-wrap:wrap}
.up .m span{display:inline-flex;align-items:center;gap:5px}
.up .dial{position:absolute;right:16px;top:16px;width:84px;height:84px}
.up .jn{margin-top:16px;height:50px;border-radius:14px;background:var(--brass);color:#16221D;font-weight:600;display:flex;align-items:center;justify-content:center;gap:9px;font-size:16px}
.up .jn.dim{background:rgba(255,255,255,.08);color:var(--invInk)}
.up.compact .t{font-size:24px}
.live{width:8px;height:8px;border-radius:50%;background:var(--brass);animation:pulse 2.4s var(--settle) infinite}
@keyframes pulse{0%{box-shadow:0 0 0 0 rgba(217,164,65,.6)}70%{box-shadow:0 0 0 8px rgba(217,164,65,0)}100%{box-shadow:0 0 0 0 rgba(217,164,65,0)}}

/* agenda */
.dh{display:flex;align-items:baseline;justify-content:space-between;padding:0 20px;margin-top:22px}
.dh .d{font-family:var(--serif);font-size:20px}
.dh .d i{font-style:italic;color:var(--ink2);font-size:15px;margin-left:6px}
.dh .n{font-size:12px;color:var(--ink2);font-family:var(--mono)}
.dh.today .d::before{content:'';display:inline-block;width:8px;height:8px;border-radius:50%;background:var(--brass);margin-right:9px;vertical-align:2px}
.ad{display:flex;flex-wrap:wrap;gap:6px;padding:10px 20px 0}
.ad span{height:28px;line-height:28px;padding:0 10px;border-radius:9px;font-size:13px;background:color-mix(in srgb,var(--c) 18%,var(--bg));color:color-mix(in srgb,var(--c) 65%,var(--ink));display:inline-flex;align-items:center;gap:6px}
.ev{display:grid;grid-template-columns:50px 1fr auto;gap:0 12px;padding:11px 20px;align-items:start;position:relative}
.ev .tm{font-family:var(--mono);font-size:13px;line-height:1.25}
.ev .tm small{display:block;color:var(--ink2);font-size:11.5px;margin-top:1px}
.ev .bd2{min-width:0}
.ev .nm{font-size:16px;font-weight:500;line-height:1.25;display:flex;align-items:center;gap:8px}
.ev .nm::before{content:'';width:9px;height:9px;border-radius:3px;background:var(--c);flex:none;margin-top:1px}
.ev .nm b{font-weight:500;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.ev .sb{font-size:13px;color:var(--ink2);margin:2px 0 0 17px;display:flex;gap:4px 10px;flex-wrap:wrap}
.ev .sb span{display:inline-flex;align-items:center;gap:4px}
.ev.past{opacity:.5}
.ev.past .nm::before{background:transparent;box-shadow:inset 0 0 0 1.5px var(--c)}
.ev.dec .nm b{text-decoration:line-through;color:var(--ink2)}
.ev.dec .nm::before{background:transparent;box-shadow:inset 0 0 0 1.5px var(--ink3)}
.ev.tent .nm::before{background:repeating-linear-gradient(135deg,var(--c) 0 2px,transparent 2px 4px);box-shadow:inset 0 0 0 1.5px var(--c)}
.ev.live2{background:var(--brassT);border-radius:16px;margin:0 8px;padding-left:12px;padding-right:12px}
.rs{font-size:12px;font-weight:500;height:24px;line-height:24px;padding:0 9px;border-radius:8px;background:var(--brassT);color:var(--brassInk);white-space:nowrap}
.rs.q{background:var(--sunk);color:var(--ink2)}
.nowline{display:flex;align-items:center;gap:6px;padding:2px 20px;color:var(--brassInk);font-family:var(--mono);font-size:11px}
.nowline::after{content:'';flex:1;height:2px;background:var(--brass);border-radius:1px}
.sk{background:linear-gradient(90deg,var(--sunk),var(--surface),var(--sunk));background-size:200% 100%;animation:sh 1.8s linear infinite;border-radius:8px}
@keyframes sh{to{background-position:-200% 0}}
@media (prefers-reduced-motion:reduce){.live,.sk{animation:none}}

/* timeline */
.tl{position:absolute;left:0;right:0}
.tl .hr{position:absolute;left:0;right:0;border-top:1px solid var(--line);height:0}
.tl .hr span{position:absolute;left:12px;top:-8px;font-family:var(--mono);font-size:10.5px;color:var(--ink2);background:var(--bg);padding:0 4px}
.tl .shade{position:absolute;left:0;right:0;top:0;background:repeating-linear-gradient(135deg,var(--shade) 0 1.5px,transparent 1.5px 7px)}
.tl .now{position:absolute;left:0;right:0;height:2px;background:var(--brass);z-index:3}
.tl .now::before{content:'';position:absolute;left:46px;top:-5px;border:6px solid transparent;border-left:9px solid var(--brass);border-right:0}
.tl .now em{position:absolute;left:4px;top:-9px;font-style:normal;font-family:var(--mono);font-size:10.5px;background:var(--brass);color:#16221D;padding:1px 4px;border-radius:5px;z-index:4}
.te{position:absolute;border-radius:10px;padding:5px 7px;font-size:12.5px;line-height:1.2;overflow:hidden;background:color-mix(in srgb,var(--c) 20%,var(--bg));color:color-mix(in srgb,var(--c) 55%,var(--ink));box-shadow:inset 0 0 0 1px color-mix(in srgb,var(--c) 30%,transparent)}
.te b{font-weight:600;display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.te small{font-family:var(--mono);font-size:10.5px;opacity:.85}
.te.past{opacity:.55}
.te.dec{background:transparent;box-shadow:inset 0 0 0 1px var(--ink3);color:var(--ink2)}
.te.dec b{text-decoration:line-through}
.te.tent{background:repeating-linear-gradient(135deg,color-mix(in srgb,var(--c) 22%,var(--bg)) 0 4px,var(--bg) 4px 8px)}
.te.drag{box-shadow:var(--shadow),inset 0 0 0 2px var(--c);background:color-mix(in srgb,var(--c) 32%,var(--surface));z-index:6}
.te .rz{position:absolute;left:50%;bottom:2px;width:22px;height:4px;margin-left:-11px;border-radius:2px;background:currentColor;opacity:.7}
.tghost{position:absolute;border-radius:10px;border:1.5px dashed var(--ink3);z-index:2}
.ghostfree{position:absolute;border-radius:10px;background:repeating-linear-gradient(135deg,var(--brassT) 0 5px,transparent 5px 10px);box-shadow:inset 0 0 0 1.5px var(--brass);color:var(--brassInk);font-size:12px;padding:5px 8px;z-index:2;font-weight:500}
.dayhd{position:absolute;left:0;right:0;display:flex;padding-left:52px;gap:0}
.dayhd div{flex:1;text-align:center}
.dayhd .dn{font-size:11px;color:var(--ink2);text-transform:uppercase;letter-spacing:.06em}
.dayhd .dd{font-family:var(--serif);font-size:22px;line-height:1.1}
.dayhd .on .dd{background:var(--ink);color:var(--bg);border-radius:10px;width:40px;margin:0 auto}
.chipt{position:absolute;z-index:8;background:var(--ink);color:var(--bg);font-family:var(--mono);font-size:12px;padding:5px 9px;border-radius:9px;box-shadow:var(--shadow)}

/* sheets, buttons, fields */
.scrim{position:absolute;inset:0;background:var(--scrim);z-index:20}
.sheet{position:absolute;left:0;right:0;bottom:0;background:var(--surface);border-radius:30px 30px 0 0;padding:22px 20px 30px;z-index:21;box-shadow:0 -10px 40px rgba(0,0,0,.18)}
.sheet::before{content:'';position:absolute;left:50%;top:-9px;margin-left:-11px;border:11px solid transparent;border-bottom:10px solid var(--surface);border-top:0}
.sheet .sh1{font-family:var(--serif);font-size:24px;line-height:1.15}
.sheet .sh2{font-size:14px;color:var(--ink2);margin-top:6px;line-height:1.4}
.btn{height:50px;border-radius:14px;display:flex;align-items:center;justify-content:center;gap:8px;font-weight:500;font-size:16px;background:var(--sunk);color:var(--ink);width:100%}
.btn.pri{background:var(--ink);color:var(--bg)}
.btn.brass{background:var(--brass);color:#16221D;font-weight:600}
.btn.dng{background:var(--madderT);color:var(--madder)}
.btn.ghost{background:transparent;color:var(--ink2)}
.btn.sm{height:36px;font-size:14px;border-radius:11px;width:auto;padding:0 14px}
.btn.off{opacity:.4}
.stack{display:flex;flex-direction:column;gap:10px}
.fl{background:var(--surface);border-radius:18px;margin:0 16px;overflow:hidden}
.fl.sunk{background:var(--sunk)}
.fr{display:flex;align-items:center;gap:13px;min-height:54px;padding:8px 16px;border-top:1px solid var(--line)}
.fr:first-child{border-top:0}
.fr .ic{color:var(--ink2);flex:none;display:inline-flex}
.fr .lb{flex:1;min-width:0}
.fr .lb b{display:block;font-weight:500;font-size:15.5px}
.fr .lb small{display:block;font-size:12.5px;color:var(--ink2)}
.fr .vl{font-size:14px;color:var(--ink2);display:inline-flex;align-items:center;gap:6px;white-space:nowrap}
.fr .vl.lk{color:var(--brassInk);font-weight:500}
.fr.err .lb small{color:var(--madder)}
.gl{font-size:12px;letter-spacing:.06em;text-transform:uppercase;color:var(--ink2);font-weight:500;padding:20px 20px 8px}
.sw{width:46px;height:28px;border-radius:9px;background:var(--sunk);position:relative;flex:none;box-shadow:inset 0 0 0 1px var(--line)}
.sw::after{content:'';position:absolute;left:3px;top:3px;width:22px;height:22px;border-radius:7px;background:var(--surface);box-shadow:0 1px 2px rgba(0,0,0,.25)}
.sw.on{background:var(--ink);box-shadow:none}
.sw.on::after{left:21px;background:var(--bg)}
.chip{display:inline-flex;align-items:center;gap:6px;height:30px;padding:0 10px;border-radius:10px;background:var(--sunk);font-size:13.5px;color:var(--ink)}
.chip.br{background:var(--brassT);color:var(--brassInk)}
.chip.er{background:var(--madderT);color:var(--madder)}
.chip.ok{background:var(--verdT);color:var(--verd)}
.sq{width:12px;height:12px;border-radius:4px;background:var(--c);flex:none;display:inline-block}
.sq.lg{width:22px;height:22px;border-radius:7px}
.bn{margin:14px 16px 0;border-radius:16px;padding:12px 14px;display:flex;gap:11px;align-items:flex-start;font-size:13.5px;line-height:1.4;background:var(--brassT);color:var(--ink)}
.bn svg{flex:none;margin-top:1px;color:var(--brassInk)}
.bn.er{background:var(--madderT)}.bn.er svg{color:var(--madder)}
.bn.ok{background:var(--verdT)}.bn.ok svg{color:var(--verd)}
.bn.nf{background:var(--sunk)}.bn.nf svg{color:var(--ink2)}
.bn b{font-weight:600;display:block}
.bn a,.lnk{color:var(--brassInk);font-weight:500;text-decoration:underline;text-underline-offset:3px;text-decoration-thickness:1px}
.toast{position:absolute;left:16px;right:16px;bottom:100px;background:var(--inv);color:var(--invInk);border-radius:16px;padding:13px 16px;font-size:14px;display:flex;align-items:center;gap:11px;z-index:30;box-shadow:var(--shadow)}
.toast b{font-weight:500;flex:1}
.toast u{text-decoration:none;color:var(--brass);font-weight:600}
.af{display:inline-flex;align-items:center;gap:5px;font-family:var(--mono);font-size:11px;color:var(--brassInk);letter-spacing:.02em}
.in{height:54px;border-radius:14px;background:var(--surface);box-shadow:inset 0 0 0 1px var(--line);display:flex;align-items:center;padding:0 14px;gap:10px;font-size:16px}
.in.focus{box-shadow:inset 0 0 0 2px var(--ink)}
.in.err{box-shadow:inset 0 0 0 2px var(--madder)}
.in .ph2{color:var(--ink3)}
.caret{display:inline-block;width:2px;height:20px;background:var(--brass);vertical-align:-4px;margin-left:1px;animation:blink 1.1s steps(1) infinite}
@keyframes blink{50%{opacity:0}}
.kb{position:absolute;left:0;right:0;bottom:0;height:300px;background:var(--sunk);z-index:6;border-radius:20px 20px 0 0;padding:12px 6px;display:grid;grid-template-rows:repeat(4,1fr);gap:8px;opacity:.95}
.kb i{display:flex;gap:5px;justify-content:center}
.kb i s{width:30px;border-radius:6px;background:var(--surface);display:block;box-shadow:0 1px 0 var(--line)}
.sp{flex:1}
.row{display:flex;align-items:center;gap:10px}
.mut{color:var(--ink2)}
.sm13{font-size:13px}
.pad{padding:0 20px}
.hero{font-family:var(--serif);font-size:38px;line-height:1.05;letter-spacing:-.015em}
.hero i{color:var(--brassInk)}
.ring{display:inline-block}
`;

// ---------- icons ----------
const P = {
  chevL: 'm15 6-6 6 6 6', chevR: 'm9 6 6 6-6 6', chevD: 'm6 9 6 6 6-6', plus: 'M12 5v14M5 12h14', x: 'M6 6l12 12M18 6 6 18', check: 'm5 12.5 4.5 4.5L19 7.5',
  video: 'M3 8a3 3 0 0 1 3-3h6a3 3 0 0 1 3 3v8a3 3 0 0 1-3 3H6a3 3 0 0 1-3-3zM15 10.5l6-3.5v10l-6-3.5',
  pin: 'M12 21s-6.5-5.2-6.5-10.5a6.5 6.5 0 0 1 13 0C18.5 15.8 12 21 12 21zM12 12.5a2 2 0 1 0 0-4 2 2 0 0 0 0 4',
  clock: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM12 7v5l3.2 2',
  repeat: 'M17 3l3 3-3 3M4 11V9.5A3.5 3.5 0 0 1 7.5 6H20M7 21l-3-3 3-3M20 13v1.5a3.5 3.5 0 0 1-3.5 3.5H4',
  users: 'M15.5 20v-1a4 4 0 0 0-4-4h-4a4 4 0 0 0-4 4v1M9.5 11a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7M21 20v-1a4 4 0 0 0-3-3.9M15.5 4.2a3.5 3.5 0 0 1 0 6.6',
  bell: 'M6 9a6 6 0 0 1 12 0c0 6.5 2.5 8 2.5 8h-17S6 15.500 6 9M10.200 20.500a2 2 0 0 0 3.600 0',
  spark: 'M12 3c.7 4.700 2.800 6.800 7.500 7.500-4.700.700-6.800 2.800-7.500 7.500-.7-4.700-2.800-6.800-7.500-7.500C9.200 9.800 11.300 7.700 12 3z',
  refresh: 'M20 11a8 8 0 0 0-14.200-4M4 4.500V9h4.500M4 13a8 8 0 0 0 14.200 4M20 19.500V15h-4.500',
  alert: 'M12 9v4.500M12 17h.01M10.300 4 2.600 17.500A2 2 0 0 0 4.300 20.500h15.400a2 2 0 0 0 1.700-3L13.700 4a2 2 0 0 0-3.400 0z',
  wifioff: 'M2 8.800A15 15 0 0 1 6 6.300M22 8.800a15 15 0 0 0-8.500-3.700M5 12.900a10 10 0 0 1 5-2.700M19 12.900a10 10 0 0 0-2.500-1.900M8.500 16.400a5 5 0 0 1 7 0M12 20h.01M3 3l18 18',
  server: 'M4 4h16a1 1 0 0 1 1 1v5a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1zM4 13h16a1 1 0 0 1 1 1v5a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1v-5a1 1 0 0 1 1-1zM7 7.500h.01M7 16.500h.01',
  key: 'M8 15.500a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM11 12.500 21 3M17 7l3 3M14.500 9.500l2 2',
  trash: 'M4 7h16M10 11v6M14 11v6M6 7l1 12.500A1.500 1.500 0 0 0 8.500 21h7a1.500 1.500 0 0 0 1.500-1.500L18 7M9 7V4h6v3',
  copy: 'M9 9h10a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1H9a1 1 0 0 1-1-1V10a1 1 0 0 1 1-1zM5 15V5a1 1 0 0 1 1-1h10',
  arrowR: 'M4 12h16m-6-6 6 6-6 6',
  cal: 'M5 5h14a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2zM3 10h18M8 3v4M16 3v4',
  globe: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM3 12h18M12 3c3 3 3 15 0 18M12 3c-3 3-3 15 0 18',
  more: 'M5 12h.01M12 12h.01M19 12h.01',
  lock: 'M6 11h12a1 1 0 0 1 1 1v7a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1v-7a1 1 0 0 1 1-1zM8 11V8a4 4 0 0 1 8 0v3',
  link: 'M10 14a4 4 0 0 0 5.700 0l3-3a4 4 0 0 0-5.700-5.700l-1 1M14 10a4 4 0 0 0-5.700 0l-3 3a4 4 0 0 0 5.700 5.700l1-1',
  eye: 'M2 12s3.500-6.500 10-6.500S22 12 22 12s-3.500 6.500-10 6.500S2 12 2 12zM12 14.500a2.500 2.500 0 1 0 0-5 2.500 2.500 0 0 0 0 5',
  eyeoff: 'M3 3l18 18M10.500 6a9 9 0 0 1 1.500-.1c6.500 0 10 6.100 10 6.100a17 17 0 0 1-3 3.700M6.600 7.600A16 16 0 0 0 2 12s3.500 6.500 10 6.500a9 9 0 0 0 4-1',
  edit: 'M4 20h4L19.500 8.500a1.800 1.800 0 0 0 0-2.500L18 4.500a1.800 1.800 0 0 0-2.500 0L4 16z',
  mail: 'M4 5h16a1 1 0 0 1 1 1v12a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1zM3 7l9 6.500L21 7',
  sun: 'M12 16.500a4.500 4.500 0 1 0 0-9 4.500 4.500 0 0 0 0 9zM12 2v2M12 20v2M4.200 4.200l1.400 1.400M18.400 18.400l1.400 1.400M2 12h2M20 12h2M4.200 19.800l1.400-1.400M18.400 5.600l1.400-1.400',
  hand: 'M9 11V5.500a1.500 1.500 0 0 1 3 0V10M12 10V4.500a1.500 1.500 0 0 1 3 0V10M15 10V6.500a1.500 1.500 0 0 1 3 0V14a7 7 0 0 1-7 7h-1a6 6 0 0 1-4.500-2L3 14.500a1.500 1.500 0 0 1 2.300-1.900L9 16',
  moveto: 'M4 7a2 2 0 0 1 2-2h4l2 2h6a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2zM11 13h5m-2-2.500 2.500 2.500L14 15.500',
  shield: 'M12 3 4.500 6v5.500c0 4.500 3 8 7.500 9.500 4.500-1.500 7.500-5 7.500-9.500V6z',
  list: 'M8 6h13M8 12h13M8 18h13M3.500 6h.01M3.500 12h.01M3.500 18h.01',
  swap: 'M7 4 3 8l4 4M3 8h14M17 12l4 4-4 4M21 16H7',
  undo: 'M9 14 4 9l5-5M4 9h10a6 6 0 0 1 0 12h-3',
  mapPin: 'M12 21s-6.500-5.200-6.500-10.500a6.500 6.500 0 0 1 13 0C18.500 15.800 12 21 12 21z',
  info: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM12 11v5M12 8h.01',
  grip: 'M9 6h.01M15 6h.01M9 12h.01M15 12h.01M9 18h.01M15 18h.01',
  note: 'M5 4h14a1 1 0 0 1 1 1v14a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1zM8 9h8M8 13h8M8 17h4',
};
export const ico = (n, s = 20, extra = '') => `<svg width="${s}" height="${s}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" ${extra}><path d="${P[n]}"/></svg>`;
export const ICONS = Object.keys(P);

// ---------- helpers ----------
const COL = (c) => `style="--c:var(--${c})"`;
export const af = (t = 'alfather') => `<span class="af">${ico('spark', 13)}${t}</span>`;
export const hm = (s) => { const [h, m] = s.split(':').map(Number); return h + m / 60; };

// dial (Up next): minutes remaining out of 60
export function dial(min, label = 'min', big = null) {
  const r = 36, C = 2 * Math.PI * r, f = Math.max(0, Math.min(1, min / 60));
  return `<svg class="dial" viewBox="0 0 84 84" aria-hidden="true"><circle cx="42" cy="42" r="${r}" fill="none" stroke="currentColor" stroke-opacity=".18" stroke-width="5"/><circle cx="42" cy="42" r="${r}" fill="none" stroke="var(--brass)" stroke-width="5" stroke-linecap="round" stroke-dasharray="${(C * f).toFixed(1)} ${C.toFixed(1)}" transform="rotate(-90 42 42)"/>${Array.from({ length: 12 }, (_, i) => { const a = i * 30 * Math.PI / 180; return `<line x1="${42 + 28 * Math.sin(a)}" y1="${42 - 28 * Math.cos(a)}" x2="${42 + 31 * Math.sin(a)}" y2="${42 - 31 * Math.cos(a)}" stroke="currentColor" stroke-opacity=".3"/>`; }).join('')}<text x="42" y="46" text-anchor="middle" font-family="DM Mono, monospace" font-size="${big ? 18 : 22}" fill="currentColor" font-weight="500">${big ?? Math.round(min)}</text><text x="42" y="60" text-anchor="middle" font-family="DM Mono, monospace" font-size="9" fill="currentColor" fill-opacity=".6">${label}</text></svg>`;
}

// daylight strip: now "HH:MM", events [{s,e,c}]
export function daylight(now, evs = [], opts = {}) {
  const A = opts.from ?? 6, B = opts.to ?? 24, pos = (x) => ((x - A) / (B - A)) * 100;
  return `<div class="dl" ${opts.style ? `style="${opts.style}"` : ''}><div class="bar"><div class="sh" style="width:${pos(hm(now))}%"></div>${evs.map((e) => `<i class="tk" style="--c:var(--${e.c});left:${pos(hm(e.s))}%;width:max(4px,${pos(hm(e.e)) - pos(hm(e.s))}%)"></i>`).join('')}<i class="nw" style="left:${pos(hm(now))}%"></i></div><div class="lb"><span>06</span><span>12</span><span>18</span><span>24</span></div></div>`;
}

// upnext card
export function upNext(o) {
  const { title, rel, meta = [], join = null, min = 8, state = 'soon', kicker = 'Up next', dialLabel = 'min', dialBig = null } = o;
  const joinEl = join ? `<div class="jn ${state === 'later' ? 'dim' : ''}">${ico('video', 20)}${join}</div>` : '';
  return `<div class="up ${o.compact ? 'compact' : ''}">${dial(min, dialLabel, dialBig)}<div class="k">${state === 'live' ? '<i class="live"></i>' : ''}${kicker} · ${rel}</div><div class="t">${title}</div><div class="m">${meta.map((m) => `<span>${m}</span>`).join('')}</div>${joinEl}</div>`;
}

export function dayHead(label, italic, n, today = false) {
  return `<div class="dh ${today ? 'today' : ''}"><span class="d">${label}<i>${italic}</i></span><span class="n">${n}</span></div>`;
}
export function allDay(items) {
  return `<div class="ad">${items.map(([t, c]) => `<span ${COL(c)}>${t}</span>`).join('')}</div>`;
}
// agenda row. o: {s,e,t,c,cal,loc,link,state: past|dec|tent|live2, rsvp: 'respond'|'maybe'|'declined'|null, rec, dur}
export function ev(o) {
  const cls = [o.state || ''].join(' ');
  const sub = [o.cal ? `<span>${o.cal}</span>` : '', o.loc ? `<span>${ico('pin', 13)}${o.loc}</span>` : '', o.link ? `<span>${ico('video', 13)}${o.link}</span>` : '', o.rec ? `<span>${ico('repeat', 13)}</span>` : ''].join('');
  const rs = o.rsvp === 'respond' ? '<span class="rs">Respond</span>' : o.rsvp === 'maybe' ? '<span class="rs q">Maybe</span>' : o.rsvp === 'declined' ? '<span class="rs q">Declined</span>' : '';
  return `<div class="ev ${cls}" ${COL(o.c)}><div class="tm">${o.s}<small>${o.e || o.dur || ''}</small></div><div class="bd2"><div class="nm"><b>${o.t}</b></div><div class="sb">${sub}</div></div>${rs}</div>`;
}
export const nowLine = (t) => `<div class="nowline">${t}</div>`;

export function topbar(title, sub, opts = {}) {
  return `<div class="top"><div><div class="ttl">${title}</div><div class="sub">${sub}</div></div><div class="row">${opts.sync || '<i class="sdot"></i>'}<span class="av">${opts.av || 'C'}</span></div></div>`;
}
export const seg = (items, on) => `<div class="seg">${items.map((x, i) => `<span class="${i === on ? 'on' : ''}">${x}</span>`).join('')}</div>`;
export const dock = (txt = 'Add with alfather') => `<div class="fade"></div><div class="dock"><div class="dk">${ico('spark', 20)}<span>${txt}</span></div><span class="fab">${ico('plus', 24)}</span></div>`;
export const navbar = (left, title, right = '') => `<div class="nav"><span class="ib">${ico(left === 'x' ? 'x' : 'chevL', 20)}</span><span class="nvt">${title}</span>${right || '<span style="width:40px"></span>'}</div>`;
export const sheet = (inner, h) => `<div class="scrim"></div><div class="sheet" ${h ? `style="min-height:${h}px"` : ''}>${inner}</div>`;
export const toast = (txt, act = '', icon = 'check', bottom = 100) => `<div class="toast" style="bottom:${bottom}px">${ico(icon, 20)}<b>${txt}</b>${act ? `<u>${act}</u>` : ''}</div>`;
export const banner = (kind, title, body, act = '', icon) => `<div class="bn ${kind}">${ico(icon || (kind === 'er' ? 'alert' : kind === 'ok' ? 'check' : kind === 'nf' ? 'info' : 'alert'), 20)}<div class="sp">${title ? `<b>${title}</b>` : ''}${body}${act ? ` <a>${act}</a>` : ''}</div></div>`;
export const sw = (on) => `<span class="sw ${on ? 'on' : ''}"></span>`;
// field rows: [icon,label,value,{sub,cls}]
export function fields(rows, style = '') {
  return `<div class="fl" ${style ? `style="${style}"` : ''}>${rows.map(([i, l, v, o = {}]) => `<div class="fr ${o.cls || ''}"><span class="ic">${i ? ico(i, 20) : ''}</span><span class="lb"><b>${l}</b>${o.sub ? `<small>${o.sub}</small>` : ''}</span>${v !== undefined && v !== null ? `<span class="vl ${o.lk ? 'lk' : ''}">${v}</span>` : ''}</div>`).join('')}</div>`;
}
export const glabel = (t) => `<div class="gl">${t}</div>`;

// timeline. o: {top,hour0,hour1,hh(px per hour),gutter,cols:[{ev:[...] }],now,tz}
export function timeline(o) {
  const { top, h0 = 8, h1 = 19, hh = 58, g = 52, cols = [[]], now, nowCol = null, extra = '', tz2 = null } = o;
  const H = (h1 - h0) * hh;
  const hours = Array.from({ length: h1 - h0 + 1 }, (_, i) => `<div class="hr" style="top:${i * hh}px"><span>${String(h0 + i).padStart(2, '0')}</span></div>`).join('');
  const tzs = tz2 ? Array.from({ length: h1 - h0 + 1 }, (_, i) => `<span style="position:absolute;right:${390 - 44 - 10 + 0}px;top:${i * hh - 8}px"></span>`).join('') : '';
  const gw = 390 - g - 8, cw = gw / cols.length;
  const evs = cols.map((c, ci) => c.map((e) => {
    const lanes = e.lanes || 1, lane = e.lane || 0;
    const l = g + ci * cw + (lane / lanes) * cw + 2, w = cw / lanes - 4;
    const t = (hm(e.s) - h0) * hh + 1, hgt = (hm(e.e) - hm(e.s)) * hh - 2;
    return `<div class="te ${e.state || ''}" style="--c:var(--${e.c});left:${l}px;width:${w}px;top:${t}px;height:${hgt}px">${(e.title ?? e.t) === undefined ? '' : `<b>${e.title ?? e.t}</b>`}${hgt > 34 && !o.compact ? `<small>${e.s}–${e.e}</small>` : ''}${e.rz ? '<i class="rz"></i>' : ''}</div>`;
  }).join('')).join('');
  const nowY = now ? (hm(now) - h0) * hh : null;
  const shade = nowY !== null ? `<div class="shade" style="height:${nowY}px;left:${g - 4}px"></div>` : '';
  const nowEl = nowY !== null ? `<div class="now" style="top:${nowY}px;${nowCol !== null ? `left:${g + nowCol * cw - 2}px;right:${390 - g - (nowCol + 1) * cw - 2}px` : ''}">${nowCol === null || nowCol === 0 ? `<em>${now}</em>` : ''}</div>` : '';
  return `<div class="tl" style="top:${top}px;height:${H}px">${hours}${tzs}${shade}${evs}${extra}${nowEl}</div>`;
}

// ---------- cards on the page ----------
export const us = (arr) => arr.map((u) => `<span class="usc">${u}</span>`).join('');
export const notes = (arr) => `<ul class="nt">${arr.map((n) => `<li>${n}</li>`).join('')}</ul>`;

export function phoneCard(o) {
  const mk = (t) => `<div class="pw ${t}"><div class="ph" data-t="${t}" role="img" aria-label="${o.title}, ${o.state}, ${t} mode">${o.screen}</div></div>`;
  return `<article class="card" id="${o.id}"><div class="pair">${mk('light')}${mk('dark')}</div><div class="meta"><div class="mt"><span class="sid">${o.id}</span><h3>${o.title}</h3><span class="st">${o.state}</span></div><div class="usr">${us(o.us)}</div>${notes(o.notes)}</div></article>`;
}
export function boardCard(o) {
  const mk = (t) => `<div class="bd ${t}" data-t="${t}" style="max-width:${o.w || 560}px">${o.inner}</div>`;
  return `<article class="card board" id="${o.id}"><div class="pair bpair">${mk('light')}${mk('dark')}</div><div class="meta"><div class="mt"><span class="sid">${o.id}</span><h3>${o.title}</h3><span class="st">${o.state || 'Board'}</span></div><div class="usr">${us(o.us)}</div>${notes(o.notes)}</div></article>`;
}

export const PAGE_CSS = `
.wrap{max-width:1360px;margin:0 auto;padding-inline:clamp(16px,4vw,48px);padding-block:48px 120px}
.eyebrow{font-family:var(--mono);font-size:12px;letter-spacing:.06em;text-transform:uppercase;color:var(--ink2)}
.pg h1{font-family:var(--serif);font-weight:400;font-size:clamp(38px,6vw,72px);line-height:1;letter-spacing:-.02em;text-wrap:balance;max-width:16ch;margin-top:14px}
.pg h1 i{color:var(--brassInk)}
.lede{max-width:64ch;color:var(--ink2);font-size:17px;margin-top:18px}
.ctrl{display:flex;flex-wrap:wrap;gap:14px;align-items:center;margin-top:26px}
.sg{display:inline-flex;background:var(--sunk);border-radius:12px;padding:3px}
.sg button{font:500 13px var(--sans);border:0;background:transparent;color:var(--ink2);height:32px;padding:0 14px;border-radius:9px;cursor:pointer}
.sg button[aria-pressed="true"]{background:var(--surface);color:var(--ink);box-shadow:0 1px 2px rgba(22,34,29,.15)}
.sg button:focus-visible,.pnav a:focus-visible{outline:2px solid var(--ink);outline-offset:2px}
.pnav{position:sticky;top:env(safe-area-inset-top,0px);z-index:50;background:color-mix(in srgb,var(--bg) 92%,transparent);backdrop-filter:blur(8px);margin-inline:calc(-1*clamp(16px,4vw,48px));padding:10px clamp(16px,4vw,48px);display:flex;gap:6px;overflow-x:auto;border-bottom:1px solid var(--line);margin-top:34px}
.pnav a{flex:none;font-size:13px;color:var(--ink2);text-decoration:none;padding:6px 11px;border-radius:9px;white-space:nowrap}
.pnav a:hover{background:var(--sunk);color:var(--ink)}
section.fl2{margin-top:72px;scroll-margin-top:70px}
.fl2 header{display:grid;gap:8px;max-width:72ch}
.fl2 h2{font-family:var(--serif);font-weight:400;font-size:clamp(28px,3.6vw,42px);line-height:1.08;letter-spacing:-.01em;text-wrap:balance}
.fl2 header p{color:var(--ink2)}
.usr{display:flex;flex-wrap:wrap;gap:5px}
.usc{font-family:var(--mono);font-size:11.5px;padding:2px 7px;border-radius:7px;background:var(--brassT);color:var(--brassInk)}
.cards{display:flex;flex-wrap:wrap;gap:44px 36px;margin-top:30px;align-items:flex-start}
.card{display:flex;flex-direction:column;gap:14px;min-width:0;flex:0 1 auto}
.pair{display:flex;gap:16px;flex-wrap:wrap}
[data-show="light"] .pw.dark,[data-show="dark"] .pw.light,[data-show="light"] .bd.dark,[data-show="dark"] .bd.light{display:none}
.card .meta{max-width:calc(2*273px + 16px);min-width:0;display:grid;gap:9px}
[data-show="light"] .card .meta,[data-show="dark"] .card .meta{max-width:273px}
.mt{display:flex;align-items:baseline;gap:9px;flex-wrap:wrap}
.mt h3{font-family:var(--serif);font-weight:500;font-size:20px;line-height:1.15}
.sid{font-family:var(--mono);font-size:12px;color:var(--ink2)}
.st{font-size:11.5px;font-weight:500;letter-spacing:.04em;text-transform:uppercase;color:var(--ink2);background:var(--sunk);padding:2px 7px;border-radius:7px}
.nt{display:grid;gap:5px;font-size:13.5px;color:var(--ink2)}
.nt li{padding-left:14px;position:relative}
.nt li::before{content:'';position:absolute;left:0;top:.62em;width:6px;height:2px;background:var(--brass);border-radius:1px}
.nt li b{color:var(--ink);font-weight:500}
.card.board{flex:1 1 100%}
.bpair{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,340px),560px));gap:16px;align-items:start}
.card.board .meta{max-width:80ch}
table.t{border-collapse:collapse;width:100%;font-size:14px}
table.t th{font-weight:500;text-align:left;color:var(--ink2);font-size:12px;letter-spacing:.05em;text-transform:uppercase;padding:8px 12px 8px 0;border-bottom:1px solid var(--line);white-space:nowrap}
table.t td{padding:10px 12px 10px 0;border-bottom:1px solid var(--line);vertical-align:top}
.tw{overflow-x:auto;margin-top:18px}
`;

export const CTRL_JS = `<script>(function(){var root=document.querySelector('[data-show]');function set(v){root.setAttribute('data-show',v);document.querySelectorAll('.sg button').forEach(function(b){b.setAttribute('aria-pressed',b.dataset.v===v?'true':'false')});}document.querySelectorAll('.sg button').forEach(function(b){b.addEventListener('click',function(){set(b.dataset.v)})});})();</script>`;

export function page({ title, eyebrow, h1, lede, sections, nav, extraCss = '', pre = '', controls = true }) {
  const ctrl = controls ? `<div class="ctrl"><div class="sg" role="group" aria-label="Show frames in"><button type="button" data-v="both" aria-pressed="true">Light and dark</button><button type="button" data-v="light" aria-pressed="false">Light</button><button type="button" data-v="dark" aria-pressed="false">Dark</button></div><span class="eyebrow">Frames drawn at 390 × 844 and scaled</span></div>` : '';
  return `<title>${title}</title>
${FONT_LINK}
<style>${BASE_CSS}${PAGE_CSS}${extraCss}</style>
<div class="wrap pg" data-show="both">
<p class="eyebrow">${eyebrow}</p>
<h1>${h1}</h1>
<p class="lede">${lede}</p>
${ctrl}
<nav class="pnav" aria-label="Sections">${nav.map(([id, l]) => `<a href="#${id}">${l}</a>`).join('')}</nav>
${pre}
${sections}
</div>
${CTRL_JS}`;
}

export function section(id, eyebrow, h2, p, cardsHtml) {
  return `<section class="fl2" id="${id}"><header><span class="eyebrow">${eyebrow}</span><h2>${h2}</h2><p>${p}</p></header><div class="cards">${cardsHtml}</div></section>`;
}
