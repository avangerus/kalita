// Documentation checks only. Does not run Kalita or validate future runtime guarantees.
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const next = path.resolve(__dirname, '..');
const catalogPath = path.join(__dirname, 'TASKS.json');
const reportPath = path.join(next, 'VALIDATION.json');
const catalog = JSON.parse(fs.readFileSync(catalogPath, 'utf8'));
const errors = [];
const check = (ok, message) => { if (!ok) errors.push(message); };
const nonempty = x => typeof x === 'string' && x.trim().length > 0;
const strings = (x, min = 1) => Array.isArray(x) && x.length >= min && x.every(nonempty);
const ids = (items, label) => {
  const result = new Set();
  for (const item of items) {
    check(nonempty(item.id), `${label}: missing id`);
    check(!result.has(item.id), `${label}: duplicate ${item.id}`);
    result.add(item.id);
  }
  return result;
};
const releases = ids(catalog.releases, 'release');
const iterations = ids(catalog.iterations, 'iteration');
const tasks = ids(catalog.tasks, 'task');
check(catalog.schema_version === '1.0' && catalog.baseline === 'N1', 'Unsupported catalog baseline/schema');
check(releases.size === 8 && iterations.size === 16 && tasks.size === 80, 'N1 must contain 8 gates, 16 iterations, 80 tasks');
const statuses = new Set(['planned', 'in_progress', 'blocked', 'done', 'deferred']);
for (const r of catalog.releases) {
  check(strings(r.exit_criteria, 2), `${r.id}: missing release exit criteria`);
  check(statuses.has(r.status), `${r.id}: invalid status`);
}
for (const i of catalog.iterations) {
  check(releases.has(i.release), `${i.id}: unknown release ${i.release}`);
  check(nonempty(i.goal) && strings(i.exit_criteria, 2), `${i.id}: incomplete iteration`);
  check(catalog.tasks.some(t => t.iteration === i.id), `${i.id}: no tasks`);
}
for (const t of catalog.tasks) {
  check(/^KAL-\d{3}$/.test(t.id), `Invalid task id ${t.id}`);
  check(iterations.has(t.iteration) && releases.has(t.release), `${t.id}: unknown iteration/release`);
  const iteration = catalog.iterations.find(i => i.id === t.iteration);
  check(iteration && iteration.release === t.release, `${t.id}: inconsistent release`);
  for (const key of ['title', 'area', 'owner_role', 'executor_model', 'reviewer']) check(nonempty(t[key]), `${t.id}: missing ${key}`);
  for (const key of ['deliverables', 'verification', 'contract_refs', 'requirements']) check(strings(t[key]), `${t.id}: missing ${key}`);
  check(strings(t.acceptance, 2), `${t.id}: fewer than two acceptance criteria`);
  check(['S', 'M', 'L'].includes(t.effort) && ['P0', 'P1', 'P2'].includes(t.priority), `${t.id}: invalid effort/priority`);
  check(statuses.has(t.status), `${t.id}: invalid status`);
  check(t.condition === null || nonempty(t.condition), `${t.id}: invalid condition`);
  for (const ref of t.contract_refs) check(fs.existsSync(path.resolve(next, ref)), `${t.id}: missing contract ${ref}`);
}
const checkDAG = (items, known, label) => {
  const state = new Map();
  const visit = id => {
    if (state.get(id) === 1) { errors.push(`${label}: cycle at ${id}`); return; }
    if (state.get(id) === 2) return;
    state.set(id, 1);
    const item = items.find(i => i.id === id);
    check(Array.isArray(item.depends_on), `${label} ${id}: missing dependency array`);
    for (const dep of item.depends_on || []) {
      check(known.has(dep), `${label} ${id}: unknown dependency ${dep}`);
      check(dep !== id, `${label} ${id}: self dependency`);
      if (known.has(dep)) visit(dep);
    }
    state.set(id, 2);
  };
  items.forEach(item => visit(item.id));
};
checkDAG(catalog.releases, releases, 'release');
checkDAG(catalog.iterations, iterations, 'iteration');
checkDAG(catalog.tasks, tasks, 'task');
const requirements = Array.from({ length: 12 }, (_, i) => `N-${String(i + 1).padStart(2, '0')}`);
for (const req of requirements) check(catalog.tasks.some(t => t.requirements.includes(req)), `${req}: no task coverage`);
for (const t of catalog.tasks) for (const req of t.requirements) check(requirements.includes(req), `${t.id}: unknown invariant ${req}`);

const files = [];
const walk = dir => {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) walk(full);
    else if (entry.name.endsWith('.md')) files.push(full);
  }
};
walk(next);
const textByPath = new Map(files.map(f => [f, fs.readFileSync(f, 'utf8')]));
const noCode = s => s.replace(/```[\s\S]*?```/g, '');
const anchors = file => {
  const text = noCode(textByPath.get(file) || fs.readFileSync(file, 'utf8'));
  const found = new Set();
  const duplicates = new Map();
  for (const line of text.split('\n')) {
    const heading = line.match(/^#{1,6}\s+(.+)$/);
    if (!heading) continue;
    const base = heading[1].trim().toLowerCase().replace(/[^\p{L}\p{N}_\- ]/gu, '').replace(/ /g, '-');
    const count = duplicates.get(base) || 0;
    found.add(count ? `${base}-${count}` : base);
    duplicates.set(base, count + 1);
  }
  for (const match of text.matchAll(/<a\s+id="([^"]+)"\s*>/g)) found.add(match[1]);
  return found;
};
let links = 0;
let fragments = 0;
for (const [file, raw] of textByPath) {
  const text = noCode(raw);
  for (const match of text.matchAll(/\[[^\]\n]+\]\(([^)\n]+)\)/g)) {
    let target = match[1].trim();
    if (/^(https?:|mailto:|app:)/i.test(target)) continue;
    if (target.startsWith('<') && target.endsWith('>')) target = target.slice(1, -1);
    const [ref, hash] = target.split('#');
    const absolute = ref ? path.resolve(path.dirname(file), decodeURIComponent(ref)) : file;
    links++;
    if (absolute === reportPath) continue; // This invocation writes the report after checks.
    check(fs.existsSync(absolute), `${path.relative(next, file)}: broken link ${target}`);
    if (hash && fs.existsSync(absolute)) {
      fragments++;
      check(anchors(absolute).has(decodeURIComponent(hash)), `${path.relative(next, file)}: missing anchor ${target}`);
    }
  }
  for (const match of text.matchAll(/\bKAL-\d{3}\b/g)) check(tasks.has(match[0]), `${path.relative(next, file)}: unknown task reference ${match[0]}`);
}
const taskMarkdown = textByPath.get(path.join(__dirname, 'TASKS.md'));
const iterationMarkdown = textByPath.get(path.join(__dirname, 'ITERATIONS.md'));
for (const t of catalog.tasks) {
  const start = taskMarkdown.indexOf(`<a id="${t.id.toLowerCase()}"></a>`);
  const end = taskMarkdown.indexOf('\n<a id=', start + 1);
  const block = taskMarkdown.slice(start, end === -1 ? undefined : end);
  check(start >= 0 && block.includes(`### ${t.id} — ${t.title}`), `${t.id}: missing Markdown entry`);
  for (const value of [...t.acceptance, ...t.verification, ...t.deliverables]) check(block.includes(value), `${t.id}: Markdown does not match catalog: ${value}`);
  check(iterationMarkdown.includes(`[${t.id}](TASKS.md#${t.id.toLowerCase()})`), `${t.id}: missing iteration link`);
}
const summary = {
  baseline: 'N1',
  checked_at: new Date().toISOString(),
  result: errors.length ? 'failed' : 'passed',
  scope: 'Documentation structure and catalog consistency only; no runtime tests or future capability proof',
  counts: { markdown_files: files.length, local_links: links, anchors: fragments, tasks: tasks.size, iterations: iterations.size, releases: releases.size, invariants: requirements.length },
  checks: ['Required catalog fields/statuses', 'Unique IDs', 'Release/iteration/task dependency DAG', 'Iteration/release consistency', 'Invariant task coverage', 'Local links and referenced anchors', 'Known task references', 'Task Markdown matches JSON deliverables/acceptance/verification'],
  tasks_json_sha256: crypto.createHash('sha256').update(fs.readFileSync(catalogPath)).digest('hex'),
  status_counts: catalog.tasks.reduce((result, t) => { result[t.status] = (result[t.status] || 0) + 1; return result; }, {}),
  errors
};
fs.writeFileSync(reportPath, JSON.stringify(summary, null, 2) + '\n', 'utf8');
process.stdout.write(JSON.stringify(summary, null, 2) + '\n');
process.exitCode = errors.length ? 1 : 0;
