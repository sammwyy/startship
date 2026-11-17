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
   classList: { toggle() {}, add() {}, remove() {} },
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
 vm.runInContext("user={username:'sammwy',permissions:['workspaces.create','users.manage','audit.view']};workspaces=[{id:'w1',name:'Production'},{id:'w2',name:'Sandbox'}];workspace=workspaces[0];bindTheme=()=>{};bindSidebar=()=>{}", context);
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
 for(const section of ['deployments','logs','containers','traffic','domains','source','environment','settings'])assert.ok(html.includes('href="#project/p1/'+section+'"'));
 assert.match(html,/href="#project\/p1\/source" aria-current="page"/);
 assert.doesNotMatch(html,/Applications|App settings|Workspace settings|data-tab|contextbar|breadcrumbs|project-heading/);
 const sidebar=html.slice(html.indexOf('<aside'),html.indexOf('</aside>'));
 assert.doesNotMatch(sidebar,/project-summary|project-state|deploy-project|sidebar-deployment/);
 assert.match(sidebar,/class="sidebar-source"/);
 assert.equal((html.match(/<header class="heading"/g)||[]).length,1);
});

test('traffic uses the shared project sidebar without replacing project routes',async()=>{
 const f=fixture('#project/p1/traffic');
 vm.runInContext(readFileSync(require('node:path').join(__dirname,'../web/traffic.js'),'utf8'),f.context);
 f.context.fakeAPI=async path=>path==='projects/p1'?{id:'p1',name:'Repo',workspace_id:'w2'}:{};
 f.run('api=fakeAPI;trafficContent=()=>"Metrics loaded"');
 await f.run("trafficPage('p1')");
 const html=f.element('#root').innerHTML;
 assert.match(html,/href="#project\/p1\/traffic" aria-current="page"/);
 assert.match(html,/Live traffic/);assert.doesNotMatch(html,/traffic-sidebar|traffic-layout/);
 assert.match(html,/id="open-navigation" aria-label="Open navigation" aria-haspopup="dialog"/);
 assert.equal((html.match(/<header class="heading"/g)||[]).length,1);
 assert.equal((html.match(/<aside/g)||[]).length,1);
 assert.equal(f.stored.get('workspace'),'w2');
 assert.equal(f.element('#traffic-data').innerHTML,'Metrics loaded');
 f.context.fakeAPI=async path=>path==='setup'?{}:path==='workspaces'?[{id:'w2',name:'Sandbox'}]:{};
 f.run('api=fakeAPI;projectPage=async(...args)=>{globalThis.projectRoute=args}');
 for(const [hash,section,runtime] of [['#project/p1',undefined,undefined],['#project/p1/source','source',undefined],['#project/p1/logs/c1','logs','c1']]){
  f.context.location.hash=hash;await f.run('render()');
  assert.deepEqual(Array.from(f.context.projectRoute),['p1',section,runtime]);
 }
});

test('workspace and account names are escaped in menu markup', () => {
 const f = fixture(); f.run("user.username='<script>';workspace.name='<img>';shell(heading('','Projects',''))");
 const html = f.element('#root').innerHTML;
 assert.match(html, /&lt;script&gt;/); assert.match(html, /&lt;img&gt;/);
 assert.doesNotMatch(html, /<script>|<img>/);
});

test('workspace and account selectors are single-line controls without repeated captions',()=>{
 const f=fixture();f.run("shell(heading('','Projects',''))");
 const html=f.element('#root').innerHTML;
 for(const id of ['workspace-switch','account-switch']){
  const selector=html.match(new RegExp('<button[^>]*id="'+id+'"[^>]*>[\\s\\S]*?</button>'))[0];
  assert.match(selector,/class="selector-name"/);
  assert.match(selector,/aria-label="(?:Workspace|Account):/);
  assert.doesNotMatch(selector,/<small>|<strong>|selector-copy|workspace-mark/);
 }
 assert.doesNotMatch(html,/Personal account|Self-hosted instance|sidebar-instance/);
});

test('sidebar collapse persists across navigation and exposes its expanded state',()=>{
 const f=fixture(),changes=[];
 f.element('.sidebar-layout').classList.toggle=(name,value)=>changes.push([name,value]);
 f.run("shell(heading('','Projects',''))");
 assert.doesNotMatch(f.element('#root').innerHTML,/class="dashboard sidebar-layout sidebar-collapsed"/);
 assert.match(f.element('#root').innerHTML,/id="toggle-sidebar"[^>]*aria-controls="desktop-sidebar" aria-expanded="true" aria-label="Collapse sidebar"/);
 f.run('setSidebarCollapsed(true)');
 assert.equal(f.stored.get('startship-sidebar-collapsed'),'true');
 assert.deepEqual(changes.pop(),['sidebar-collapsed',true]);
 assert.equal(f.element('#toggle-sidebar').attributes['aria-expanded'],'false');
 assert.equal(f.element('#toggle-sidebar').attributes['aria-label'],'Expand sidebar');
 f.run("shell(heading('','Source',''),{id:'p1',name:'Repo'})");
 assert.match(f.element('#root').innerHTML,/class="dashboard sidebar-layout sidebar-collapsed"/);
 assert.match(f.element('#root').innerHTML,/id="toggle-sidebar"[^>]*aria-expanded="false" aria-label="Expand sidebar"/);
 f.run('setSidebarCollapsed(false)');
 assert.equal(f.stored.get('startship-sidebar-collapsed'),'false');
 assert.deepEqual(changes.pop(),['sidebar-collapsed',false]);
 assert.equal(f.element('#toggle-sidebar').attributes['aria-expanded'],'true');
});

test('icon-only navigation retains accessible labels and native hover titles',()=>{
 const f=fixture('#project/p1/source');f.run("shell(heading('','Source',''),{id:'p1',name:'Repo'})");
 const html=f.element('#root').innerHTML;
 for(const label of ['All projects','Deployments','Logs','Containers','Domains','Source','Environment','Settings'])
  assert.ok(html.includes('aria-label="'+label+'" title="'+label+'"'));
 assert.match(html,/aria-label="Startship" title="Startship"/);
 assert.match(html,/title="Workspace: Production"/);
 assert.match(html,/title="Account: sammwy"/);
});

test('content capsule floats on the sidebar background and mobile keeps full navigation labels',()=>{
 const styles=readFileSync(require('node:path').join(__dirname,'../web/style.css'),'utf8');
 assert.match(styles,/\.dashboard\.sidebar-layout \{[^}]*background: var\(--terminal\)/);
 assert.match(styles,/#desktop-sidebar \{[^}]*background: var\(--terminal\)/);
 assert.match(styles,/\.dashboard-body \{[^}]*margin: 8px 8px 8px 0[^}]*border-radius: 14px[^}]*background: var\(--bg\)/);
 const desktop=styles.slice(styles.indexOf('@media (min-width: 761px)'),styles.indexOf('@media (max-width: 1100px)'));
 assert.match(desktop,/\.dashboard\.sidebar-collapsed \{ grid-template-columns: 72px minmax\(0, 1fr\)/);
 assert.match(desktop,/\.sidebar-collapsed \.sidebar-nav a > span[^}]*display: none/);
 assert.match(styles,/@media \(max-width: 760px\) \{[\s\S]*?\.sidebar-fold \{ display: none/);
});

test('dashboard fills the viewport while content and sidebar navigation scroll independently',()=>{
 const styles=readFileSync(require('node:path').join(__dirname,'../web/style.css'),'utf8');
 assert.match(styles,/:root:has\(\.sidebar-layout\) \{[^}]*overflow: hidden/);
 assert.match(styles,/\.dashboard\.sidebar-layout \{[^}]*height: 100dvh[^}]*margin: 0[^}]*overflow: hidden/);
 assert.match(styles,/#desktop-sidebar \{[^}]*height: 100%/);
 assert.match(styles,/\.sidebar \{[^}]*height: 100%/);
 assert.match(styles,/\.sidebar-body \{[^}]*flex: 1[^}]*min-height: 0[^}]*overflow-y: auto/);
 assert.match(styles,/\.sidebar-layout \.main \{[^}]*min-height: 0[^}]*overflow-y: auto[^}]*overscroll-behavior: contain/);
 assert.doesNotMatch(styles.match(/\.sidebar-bottom \{([^}]+)/)[1],/margin-top: auto/);
 assert.match(styles,/\.sidebar-selector \{[^}]*border: 0/);
 assert.match(styles,/#navigation-drawer \.sidebar-selector \{[^}]*min-height: 44px/);
 assert.doesNotMatch(styles,/sidebar-deployment|sidebar-instance|selector-copy/);
 const f=fixture();f.run("shell(heading('','Projects',''))");
 const html=f.element('#root').innerHTML;
 assert.ok(html.indexOf('<footer class="app-footer">')<html.indexOf('</main>'));
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
  assert.equal((html.match(/id="deploy-project"/g)||[]).length,1);
  assert.doesNotMatch(html,/deploy-project-mobile/);
  assert.match(html,/<p id="project-summary" aria-live="polite">/);
  assert.equal((html.match(/id="open-navigation"/g)||[]).length,1);
  assert.doesNotMatch(html,/contextbar|breadcrumbs|project-heading/);
  assert.equal(f.stored.get('workspace'),'w2');
 }
});

test('deploy action is local to Deployments, never repeated across project sections',async()=>{
 for(const section of ['deployments','logs','containers','domains','source','environment','settings']){
  const f=fixture('#project/p1/'+section);f.context.fakeAPI=async path=>path==='projects/p1'?{id:'p1',name:'Repo',workspace_id:'w1',git_url:'file:///repo'}:[];
  f.run('api=fakeAPI');await f.run("projectPage('p1','"+section+"')");
  const html=f.element('#root').innerHTML;
  const header=html.match(/<header class="heading">[\s\S]*?<\/header>/)[0];
  assert.equal(header.includes('id="deploy-project"'),section==='deployments');
  assert.doesNotMatch(html.slice(html.indexOf('<aside'),html.indexOf('</aside>')),/deploy-project/);
  if(section!=='deployments'){
   assert.equal(f.element('#deploy-project').onclick,undefined);
   assert.equal(f.element('#deploy-project').disabled,undefined);
  }
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
 assert.equal(f.element('#deploy-project').disabled,true);
 f.context.liveContainers=[{id:'c1',status:'running',active_deployment:'d1'},{id:'c2',status:'running',active_deployment:'d2'}];f.context.liveReleases[0].status='running';
 await f.context.poll();assert.equal(f.element('#project-summary').textContent,'2 containers · 2 running · 0 domain routes');
 assert.equal(f.element('#deploy-project').disabled,false);
 f.context.liveContainers[1].status='failed';await f.context.poll();assert.equal(f.element('#project-summary').textContent,'2 containers · 1 running · 0 domain routes');
 assert.ok(calls.filter(path=>path==='releases?project_id=p1&logs=false').length>=3);
});

test('sidebar shows platform pages and creation controls only with the matching permissions', () => {
 const f = fixture(); f.run("shell(heading('Workspace','Projects',''))");
 let html = f.element('#root').innerHTML;
 for (const link of ['#workspace/w1/members', '#roles', '#users', '#audit']) assert.ok(html.includes('href="' + link + '"'), link);
 f.run("user={username:'guest',permissions:[]};shell(heading('Workspace','Projects',''))");
 html = f.element('#root').innerHTML;
 assert.ok(html.includes('href="#roles"'));
 assert.doesNotMatch(html, /href="#users"|href="#audit"|Create workspace/);
});
