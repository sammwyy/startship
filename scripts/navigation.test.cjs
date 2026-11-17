const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { test } = require('node:test');
const vm = require('node:vm');

const source = readFileSync(require('node:path').join(__dirname, '../web/app.js'), 'utf8');

function fixture(hash = '#workspace/w1') {
 const elements = new Map();
 const element = selector => {
  if (!elements.has(selector)) elements.set(selector, {
   innerHTML: '', hidden: false, dataset: {}, attributes: {},
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
 assert.match(html, /Account details/);
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
