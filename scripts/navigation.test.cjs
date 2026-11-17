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
 const f = fixture(); f.run("shell(heading('Workspace','Projects',''))");
 const html = f.element('#root').innerHTML;
 assert.match(html, /id="workspace-switch" popovertarget="workspace-menu"/);
 assert.match(html, /id="account-switch" popovertarget="account-menu"/);
 assert.match(html, /Create workspace/);
 assert.match(html, /Workspace settings/);
 assert.match(html, /Account settings/);
 assert.match(html, /Switch to light theme/);
 assert.match(html, /aria-label="Open navigation"/);
 assert.doesNotMatch(html, /primary-nav|app-header|contextbar|breadcrumbs/);
 assert.equal((html.match(/<header class="heading"/g)||[]).length,1);
});

test('project sidebar exposes direct project sections and no app tabs', () => {
 const f=fixture('#project/p1/source');f.run("shell(heading('','Source',''),{id:'p1',name:'Storefront',summary:'2 containers · 2 running',deployment_status:'running'})");
 const html=f.element('#root').innerHTML;
 assert.match(html,/href="#workspace\/w1"[^>]*>.*All projects/);
 for(const section of ['deployments','logs','containers','domains','source','environment','settings'])assert.ok(html.includes('href="#project/p1/'+section+'"'));
 assert.match(html,/href="#project\/p1\/source" aria-current="page"/);
 assert.doesNotMatch(html,/Applications|App settings|Workspace settings|data-tab|contextbar|breadcrumbs|project-heading/);
 const sidebar=html.slice(html.indexOf('<aside'),html.indexOf('</aside>'));
 assert.match(sidebar,/id="project-summary"[^>]*>2 containers · 2 running/);
 assert.match(sidebar,/id="project-state"/);
 assert.match(sidebar,/id="deploy-project"/);
 assert.match(sidebar,/class="sidebar-source"/);
 assert.equal((html.match(/<header class="heading"/g)||[]).length,1);
});

test('workspace and account names are escaped in menu markup', () => {
 const f = fixture(); f.run("user.username='<script>';workspace.name='<img>';shell(heading('','Projects',''))");
 const html = f.element('#root').innerHTML;
 assert.match(html, /&lt;script&gt;/); assert.match(html, /&lt;img&gt;/);
 assert.doesNotMatch(html, /<script>|<img>/);
});

test('keyboard menu focus scrolls nested menu containers, never the background page',()=>{
 const f=fixture();let focusOptions;
 const menu={scrollTop:0,scrollHeight:350,clientHeight:120,clientTop:1,parentElement:null,getBoundingClientRect:()=>({top:0})};
 const list={scrollTop:0,scrollHeight:600,clientHeight:200,clientTop:0,parentElement:menu,getBoundingClientRect:()=>({top:40-menu.scrollTop})};
 const item={parentElement:list,focus:options=>{focusOptions=options},getBoundingClientRect:()=>({top:500-list.scrollTop-menu.scrollTop,bottom:536-list.scrollTop-menu.scrollTop})};
 f.context.menu=menu;f.context.item=item;f.run('focusMenuItem(menu,item)');
 assert.equal(focusOptions.preventScroll,true);assert.equal(list.scrollTop,296);assert.equal(menu.scrollTop,119);
 assert.equal(item.getBoundingClientRect().bottom,121);
 f.run('focusMenuItem(menu,null)');
});

test('single and multiple containers have the same project-owned deployment controls',async()=>{
 for(const count of [0,1,3]){
  const f=fixture('#project/p1');f.context.fakeAPI=async path=>path==='projects/p1'?{id:'p1',name:'Repo',workspace_id:'w2',git_url:'file:///repo'}:path==='containers?project_id=p1'?Array.from({length:count},(_,i)=>({id:'c'+i,name:'web',status:'running'})):[];
  f.run('api=fakeAPI');await f.run("projectPage('p1')");
  const html=f.element('#root').innerHTML;
  assert.match(html,/id="deploy-project"/);assert.match(html,/Project releases/);
  assert.doesNotMatch(html,/application-switch|app-workbench|Add app|Create application/);
  assert.match(html,new RegExp(count+' container'));
  assert.match(html,/<h1>Deployments<\/h1>/);
  assert.match(html,/id="deploy-project-mobile"/);
  assert.equal(f.element('#deploy-project').disabled,f.element('#deploy-project-mobile').disabled);
  assert.equal((html.match(/id="open-navigation"/g)||[]).length,1);
  assert.doesNotMatch(html,/contextbar|breadcrumbs|project-heading/);
  assert.equal(f.stored.get('workspace'),'w2');
 }
});
test('source and old Git links work before detection without app setup',async()=>{
 for(const section of ['source','git','webhook']){
  const f=fixture('#project/p1/'+section);f.context.fakeAPI=async path=>path==='projects/p1'?{id:'p1',name:'Repo',workspace_id:'w1'}:[];
  f.run('api=fakeAPI');await f.run("projectPage('p1','"+section+"')");
  const html=f.element('#root').innerHTML;assert.match(html,/GitHub webhook/);assert.match(html,/Project source/);assert.doesNotMatch(html,/Create application/);
 }
});
test('domain target selection only appears when a project has multiple containers',()=>{
 const f=fixture();f.run('modal=(...args)=>{globalThis.dialog=args};addRoute({id:"p1"},[{id:"c1",name:"web",port:8080}])');
 assert.doesNotMatch(f.context.dialog[2],/<select name="container_id"/);assert.match(f.context.dialog[2],/type="hidden" name="container_id"/);
 f.run('addRoute({id:"p1"},[{id:"c1",name:"web",port:8080},{id:"c2",name:"api",port:3000}])');
 assert.match(f.context.dialog[2],/<select name="container_id"/);assert.doesNotMatch(f.context.dialog[2],/Application/);
});
test('project creation never creates separately configured apps',async()=>{
 const f=fixture();const calls=[];f.context.fakeAPI=async(...args)=>{calls.push(args);return{id:'p1'}};
 f.run('api=fakeAPI;modal=(...args)=>{globalThis.dialog=args};toast=()=>{};createProject()');
 assert.doesNotMatch(f.context.dialog[2],/name="layout"|application/);
 await f.context.dialog[4](new Map([['name','Test'],['git_url','file:///repo'],['branch','main']]));
 assert.equal(calls.length,1);assert.equal(calls[0][0],'projects');assert.equal(f.context.location.hash,'project/p1');
});
test('shared environment parsing preserves values and rejects invalid names',()=>{
 const f=fixture();assert.equal(f.run('environmentData("TOKEN=a=b\\n#comment").TOKEN'),'a=b');assert.throws(()=>f.run('environmentData("BAD NAME=value")'),/KEY=VALUE/);
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

test('project polling updates running counts and grouped release entries without tables',async()=>{
 const f=fixture('#project/p1');f.context.setInterval=callback=>{f.context.poll=callback;return 1};
 f.context.liveContainers=[{id:'c1',name:'web'},{id:'c2',name:'api'}];
 f.context.liveReleases=[{id:'release1',project_id:'p1',status:'building',created:'2026-11-17T09:00:00Z',commit:'<img>',container_releases:['d1','d2']}];
 const calls=[];f.context.fakeAPI=async path=>{calls.push(path);if(path==='projects/p1')return{id:'p1',name:'Repo',workspace_id:'w1',git_url:'file:///repo'};if(path==='containers?project_id=p1')return f.context.liveContainers;if(path.startsWith('releases?'))return f.context.liveReleases;if(path==='releases/release1')return{...f.context.liveReleases[0],log:'output'};return[]};
 f.run('api=fakeAPI');await f.run("projectPage('p1')");
 let html=f.element('#root').innerHTML;assert.match(html,/class="release-entry select-deploy/);assert.match(html,/&lt;img&gt;/);assert.doesNotMatch(html,/<table>/);
 f.context.liveContainers=[{id:'c1',status:'running',active_deployment:'d1'},{id:'c2',status:'running',active_deployment:'d2'}];f.context.liveReleases[0].status='running';
 await f.context.poll();assert.equal(f.element('#project-summary').textContent,'2 containers · 2 running · 0 domain routes');
 f.context.liveContainers[1].status='failed';await f.context.poll();assert.equal(f.element('#project-summary').textContent,'2 containers · 1 running · 0 domain routes');
 assert.ok(calls.filter(path=>path==='releases?project_id=p1&logs=false').length>=3);
});
