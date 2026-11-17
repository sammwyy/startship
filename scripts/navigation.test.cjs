const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { test } = require('node:test');
const vm = require('node:vm');

const source = readFileSync(require('node:path').join(__dirname, '../web/app.js'), 'utf8');

function fixture(hash = '#workspace/w1') {
 const elements = new Map();
 const element = selector => {
  if (!elements.has(selector)) elements.set(selector, {
   innerHTML: '', textContent: '', style: {}, hidden: false, dataset: {}, attributes: {},
   classList: { toggle() {} },
   setAttribute(name, value) { this.attributes[name] = value; },
   removeAttribute(name) { delete this.attributes[name]; },
  });
  return elements.get(selector);
 };
 const tabs = ['deployments', 'domains', 'settings'].map(tab => {
  const node = element('tab-' + tab); node.dataset.tab = tab; return node;
 });
 const stored = new Map();
 const location = { hash, origin: 'http://localhost:8080' };
 const context = vm.createContext({
  document: { querySelector: element, querySelectorAll: selector => selector === '[data-tab]' ? tabs : [], documentElement: { dataset: { theme: 'dark' } } },
  window: { addEventListener() {} }, location,
  localStorage: { getItem: key => stored.get(key), setItem: (key, value) => stored.set(key, value) },
  history: { pushState: (_, __, value) => { location.hash = value; } },
  clearInterval() {}, setInterval() {}, setTimeout() {},
 });
 vm.runInContext(source.replace(/render\(\);\s*$/, ''), context);
 vm.runInContext("user={username:'sammwy'};workspaces=[{id:'w1',name:'Production'},{id:'w2',name:'Sandbox'}];workspace=workspaces[0];bindTheme=()=>{};bindSidebar=()=>{}", context);
 return { context, element, tabs, stored, run: code => vm.runInContext(code, context) };
}

test('workspace shell puts switching and account controls in the sidebar, not a top navigation', () => {
 const f = fixture(); f.run("shell('Production','Projects')");
 const html = f.element('#root').innerHTML;
 assert.match(html, /id="workspace-switch" popovertarget="workspace-menu"/);
 assert.match(html, /id="account-switch" popovertarget="account-menu"/);
 assert.match(html, /Create workspace/);
 assert.match(html, /Workspace settings/);
 assert.match(html, /Account settings/);
 assert.match(html, /Switch to light theme/);
 assert.match(html, /aria-label="Open navigation"/);
 assert.doesNotMatch(html, /primary-nav|app-header/);
});

test('project sidebar returns to its workspace and separates project Git from app views', () => {
 const f = fixture('#project/p1/git');
 f.run("shell('Project Git','Git',{id:'p1',name:'Storefront'})");
 const html = f.element('#root').innerHTML;
 assert.match(html, /href="#workspace\/w1"[^>]*>.*All projects/);
 assert.match(html, /href="#project\/p1\/git" aria-current="page"/);
 assert.doesNotMatch(html, /Workspace settings/);
});

test('workspace and account names are escaped in menu markup', () => {
 const f = fixture(); f.run("user.username='<script>';workspace.name='<img>';shell('','')");
 const html = f.element('#root').innerHTML;
 assert.match(html, /&lt;script&gt;/); assert.match(html, /&lt;img&gt;/);
 assert.doesNotMatch(html, /<script>|<img>/);
});

test('project Git is reachable before a project has any apps, including old webhook links', async () => {
 for (const tab of ['git', 'webhook']) {
  const f = fixture('#project/p1/' + tab);
  f.context.fakeAPI = path => Promise.resolve(path === 'projects/p1' ? { id: 'p1', name: 'Empty repo', workspace_id: 'w2', branch: 'main' } : []);
  f.run('api=fakeAPI'); await f.run(`projectPage('p1','','${tab}')`);
  assert.match(f.element('#root').innerHTML, /Project GitHub webhook/);
  assert.match(f.element('#root').innerHTML, /href="#workspace\/w2"/);
  assert.equal(f.stored.get('workspace'), 'w2');
 }
});

test('single-app and multi-app projects mount the same controls without a card switcher', async () => {
 for (const count of [1, 3]) {
  const f = fixture('#project/p1');
  f.context.fakeAPI = path => Promise.resolve(path === 'projects/p1' ? { id: 'p1', name: 'Repo', workspace_id: 'w1' } : path === 'apps?project_id=p1' ? Array.from({ length: count }, (_, i) => ({ id: 'a' + i })) : []);
  f.run('api=fakeAPI;mountApplication=async(p,a)=>{globalThis.mounted=a.id}');
  await f.run("projectPage('p1')");
  assert.match(f.element('#root').innerHTML, /id="app-workbench"/);
  assert.doesNotMatch(f.element('#root').innerHTML, /service-switcher/);
  assert.equal(f.context.mounted, 'a0');
 }
});

test('application tabs are deep links and switching apps preserves the current tab', async () => {
 const f = fixture('#app/a1/settings');
 f.run("globalThis.p={id:'p1',git_url:'https://example.com/repo'};globalThis.a={id:'a1',name:'Web',root_dir:'web',port:3000,build_type:'node'}");
 await f.run("mountApplication(p,a,[a,{id:'a2',name:'API'}],[],'settings',[])");
 const html = f.element('#app-workbench').innerHTML;
 assert.match(html, /aria-label="Application"/);
 assert.match(html, /href="#app\/a1\/domains"/);
 assert.doesNotMatch(html, /Project Git/);
 assert.equal(f.tabs[2].attributes['aria-current'], 'page');
 f.element('#application-switch').onchange({ target: { value: 'a2' } });
 assert.equal(f.context.location.hash, 'app/a2/settings');
 f.tabs[1].onclick({ preventDefault() {} });
 assert.equal(f.context.location.hash, '#app/a1/domains');
 assert.equal(f.tabs[1].attributes['aria-current'], 'page');
 assert.equal(f.tabs[2].attributes['aria-current'], undefined);
});

test('account settings confirms passwords, saves only requested credentials, and refreshes the account', async () => {
 const f = fixture();
 f.run('modal=(...args)=>{globalThis.accountDialog=args};render=async()=>{};toast=()=>{};accountSettings()');
 const fields = f.context.accountDialog[2], save = f.context.accountDialog[4];
 assert.match(fields, /name="username"/);
 assert.match(fields, /name="current_password"[^>]*required autocomplete="current-password"/);
 assert.match(fields, /name="new_password"/);
 assert.match(fields, /name="confirm_password"/);
 const changes = new Map([['username', 'renamed'], ['current_password', 'current-password'], ['new_password', ''], ['confirm_password', 'different']]);
 await assert.rejects(save(changes, {}), /New passwords do not match/);
 let request, resets = 0;
 f.context.fakeAPI = async (...args) => { request = args; return { id: 'u1', username: 'renamed' }; };
 f.run('api=fakeAPI');
 changes.set('confirm_password', '');
 await save(changes, { reset() { resets++; } });
 assert.equal(request[0], 'me'); assert.equal(request[2], 'PATCH');
 assert.equal(request[1].current_password, 'current-password');
 assert.equal(request[1].username, 'renamed');
 assert.equal(Object.hasOwn(request[1], 'new_password'), false);
 assert.equal(f.run('user.username'), 'renamed');
 assert.equal(resets, 1); assert.equal(f.run('accountSaving'), false);
 changes.set('new_password', 'replacement-password'); changes.set('confirm_password', 'replacement-password');
 await save(changes, { reset() {} });
 assert.equal(request[1].new_password, 'replacement-password');
 f.context.fakeAPI = async () => { throw Error('current password is incorrect'); };
 f.run('api=fakeAPI');
 await assert.rejects(save(changes, {}), /current password is incorrect/);
 assert.equal(f.run('accountSaving'), false);
});

test('credential rotation ignores stale polling authorization errors without ignoring real sign-out', async () => {
 const f = fixture();
 f.context.fetch = () => new Promise(resolve => { f.context.respond = resolve; });
 const pending = f.run("api('deployments?logs=false')");
 f.run("user={username:'updated'}");
 f.context.respond({ ok: false, status: 401, json: async () => ({ error: 'expired' }) });
 await assert.rejects(pending, /expired/);
 assert.equal(f.run('user.username'), 'updated');
 const duringSave = f.run("accountSaving=true;api('deployments?logs=false')");
 f.context.respond({ ok: false, status: 401, json: async () => ({ error: 'expired' }) });
 await assert.rejects(duringSave, /expired/);
 assert.equal(f.run('user.username'), 'updated');
 const signedOut = f.run("accountSaving=false;api('me')");
 f.context.respond({ ok: false, status: 401, json: async () => ({ error: 'expired' }) });
 await assert.rejects(signedOut, /expired/);
 assert.equal(f.run('user'), null); assert.equal(f.context.location.hash, 'login');
});

test('deployment polling refreshes all project running counts and keeps releases in compact entries', async () => {
 const f = fixture('#app/a1/deployments');
 f.context.setInterval = callback => { f.context.poll = callback; return 1; };
 f.context.liveApps = [{ id: 'a1', name: 'Web' }, { id: 'a2', name: 'API' }];
 f.context.liveDeployments = [{ id: 'release-1', app_id: 'a1', status: 'building', created: '2026-11-17T09:00:00Z', commit: '<img>' }];
 const requests = [];
 f.context.fakeAPI = async path => {
  requests.push(path);
  if (path === 'apps?project_id=p1') return f.context.liveApps;
  if (path === 'deployments?logs=false') return f.context.liveDeployments;
  return { id: 'release-1', log: 'Build output', status: 'building' };
 };
 f.run('api=fakeAPI');
 await f.run("mountApplication({id:'p1',git_url:'file:///repo'},liveApps[0],liveApps,[],'deployments',liveDeployments)");
 assert.equal(f.element('#project-summary').textContent, '2 apps · 0 running · 0 routes');
 const html = f.element('#tab-content').innerHTML;
 assert.match(html, /class="release-entry select-deploy/);
 assert.match(html, /aria-pressed="true"/);
 assert.match(html, /&lt;img&gt;/);
 assert.doesNotMatch(html, /<table>/);
 f.context.liveApps = [{ id: 'a1', active_deployment: 'release-1' }, { id: 'a2', active_deployment: 'release-2' }];
 f.context.liveDeployments = [{ id: 'release-1', app_id: 'a1', status: 'running' }, { id: 'release-2', app_id: 'a2', status: 'running' }];
 await f.context.poll();
 assert.equal(f.element('#project-summary').textContent, '2 apps · 2 running · 0 routes');
 assert.doesNotMatch(f.element('#tab-content').innerHTML, /data-id="release-2"/);
 f.context.liveDeployments[1].status = 'stopped';
 await f.context.poll();
 assert.equal(f.element('#project-summary').textContent, '2 apps · 1 running · 0 routes');
 assert.ok(requests.filter(path => path === 'deployments?logs=false').length >= 3);
});
