import fs from 'fs';
const out = new URL('..', import.meta.url).pathname;
const targets = process.argv.slice(2);
for (const t of targets) {
  const m = await import(`./${t}.mjs`);
  const names={'hifi-1':'hifi-1-connect-accounts','hifi-2':'hifi-2-agenda-day','hifi-3':'hifi-3-events','hifi-4':'hifi-4-settings-alfather-widgets','ia':'ia-wireframes'};
  fs.writeFileSync(`${out}${names[t]||t}.html`, m.default);
  console.log(t, (m.default.length / 1024).toFixed(0) + 'KB');
}
