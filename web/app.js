'use strict';
const $ = s => document.querySelector(s);
const esc = v => String(v ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const rootLabel = root => esc(!root || root==='.' ? '/ (repo root)' : root);
const icons = {
 arrow: '<path d="M7 17 17 7M7 7h10v10"/>',
 next: '<path d="M5 12h14m-5-5 5 5-5 5"/>',
 plus: '<path d="M12 5v14M5 12h14"/>',
 grid: '<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>',
 layers: '<path d="m12 3 10 5-10 5L2 8l10-5Zm-9 9 9 5 9-5M3 16l9 5 9-5"/>',
 box: '<path d="m12 3 9 5v9l-9 5-9-5V8l9-5Zm0 10v9M3 8l9 5 9-5M7.5 5.5l9 5"/>',
 server: '<rect x="3" y="3" width="18" height="7" rx="2"/><rect x="3" y="14" width="18" height="7" rx="2"/><path d="M7 6.5h.01M7 17.5h.01M16 6.5h2M16 17.5h2"/>',
 branch: '<circle cx="6" cy="5" r="2"/><circle cx="6" cy="19" r="2"/><circle cx="18" cy="5" r="2"/><path d="M6 7v10M18 7a8 8 0 0 1-8 8H6"/>',
 folder: '<path d="M3 7a2 2 0 0 1 2-2h5l2 2h7a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z"/>',
 globe: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a20 20 0 0 1 0 18 20 20 0 0 1 0-18Z"/>',
 activity: '<path d="M2 12h5l3-8 4 16 3-8h5"/>',
 terminal: '<path d="m7 8 4 4-4 4m7 0h4"/><rect x="2" y="3" width="20" height="18" rx="3"/>',
 settings: '<path d="M4 7h16M4 17h16"/><circle cx="8" cy="7" r="3" fill="var(--bg)"/><circle cx="16" cy="17" r="3" fill="var(--bg)"/>',
 search: '<circle cx="10" cy="10" r="6"/><path d="m15 15 6 6"/>',
 lock: '<rect x="5" y="10" width="14" height="11" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3m-4 5v2"/>',
 logout: '<path d="M9 4H5a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h4m6-13 5 5-5 5m-6-5h11"/>',
 menu: '<path d="M4 6h16M4 12h16M4 18h16"/>',
 sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5"/>',
 moon: '<path d="M20.7 13A9 9 0 0 1 11 3.3 9 9 0 1 0 20.7 13Z"/>',
 close: '<path d="m6 6 12 12M6 18 18 6"/>'
};
function icon(name){return '<svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">'+(icons[name]||icons.box)+'</svg>'}
function brand(){return '<span class="mark">'+icon('arrow')+'</span><span>Startship</span>'}
let user, workspaces=[], workspace, timer, selectedDeployment='', viewVersion=0;
function themeButton(){
 const light=document.documentElement.dataset.theme==='light';
 return '<button class="theme-toggle" id="theme-toggle" aria-label="Switch to '+(light?'dark':'light')+' theme" title="Switch to '+(light?'dark':'light')+' theme">'+icon(light?'moon':'sun')+'<span>'+(light?'Dark':'Light')+'</span></button>';
}
function bindTheme(){
 $('#theme-toggle').onclick=()=>{
  const theme=document.documentElement.dataset.theme==='light'?'dark':'light';
  document.documentElement.dataset.theme=theme;
  try{localStorage.setItem('startship-theme',theme)}catch{}
  $('#theme-toggle').outerHTML=themeButton();bindTheme();$('#theme-toggle').focus();
 };
}
async function api(path,body,method){const r=await fetch('/api/'+path,{method:method||(body?'POST':'GET'),headers:{'Content-Type':'application/json'},body:body?JSON.stringify(body):undefined});const v=await r.json();if(!r.ok){if(r.status===401&&user){user=null;location.hash='login'}throw Error(v.error||'Request failed')}return v}
function toast(text){$('#toast').textContent=text;$('#toast').style.display='block';setTimeout(()=>$('#toast').style.display='none',3500)}
function badge(status){return '<span class="badge '+esc(status||'idle')+'">'+esc(status||'Not deployed')+'</span>'}
function empty(title,text){return '<div class="panel empty"><span class="card-icon">'+icon('box')+'</span><strong>'+esc(title)+'</strong>'+esc(text)+'</div>'}
function field(label,name,value='',type='text',extra=''){return '<label>'+esc(label)+'<input name="'+name+'" type="'+type+'" value="'+esc(value)+'" '+extra+'></label>'}
function nav(hash){if(location.hash==='#'+hash)render();else location.hash=hash}
function modal(title,description,fields,submit,onSubmit){
 const d=$('#modal');d.setAttribute('aria-labelledby','modal-title');d.innerHTML='<div class="row"><h2 id="modal-title">'+esc(title)+'</h2><button class="icon-button" type="button" id="close-modal" aria-label="Close">'+icon('close')+'</button></div><p class="small">'+esc(description)+'</p><form><div class="modal-body">'+fields+'<div class="form-error" role="alert"></div></div><div class="form-actions"><button type="button" id="cancel-modal">Cancel</button><button class="primary" type="submit">'+esc(submit)+'</button></div></form>';
 $('#close-modal').onclick=$('#cancel-modal').onclick=()=>d.close();
 d.querySelector('form').onsubmit=async e=>{e.preventDefault();const button=d.querySelector('[type=submit]');button.disabled=true;d.querySelector('.form-error').textContent='';try{if(await onSubmit(new FormData(e.target),e.target)!==false)d.close()}catch(err){d.querySelector('.form-error').textContent=err.message}finally{button.disabled=false}};d.showModal();d.querySelector('form input,form select,form textarea')?.focus();
}
function shell(crumb,content){
 const onWorkspaces=!/^#(workspace|project|app)\//.test(location.hash);
 $('#root').innerHTML='<div class="dashboard"><header class="app-header"><a href="#workspaces" class="brand">'+brand()+'</a><nav class="primary-nav" aria-label="Main navigation"><a class="'+(onWorkspaces?'':'active')+'" '+(!onWorkspaces?'aria-current="page"':'')+' href="#workspace/'+esc(workspace?.id||'')+'">'+icon('grid')+'Projects</a><a class="'+(onWorkspaces?'active':'')+'" '+(onWorkspaces?'aria-current="page"':'')+' href="#workspaces">'+icon('layers')+'Workspaces</a></nav><div class="header-controls">'+themeButton()+'<a class="icon-button docs-link" href="https://docs.docker.com/" target="_blank" rel="noreferrer" aria-label="Docker documentation" title="Docker documentation">'+icon('server')+'</a><span class="avatar" title="'+esc(user.username)+'">'+esc(user.username[0].toUpperCase())+'</span><button class="icon-button" id="logout" title="Sign out" aria-label="Sign out">'+icon('logout')+'</button></div></header><main class="main"><div class="contextbar"><label class="workspace-picker">'+icon('layers')+'<select id="workspace-switch" aria-label="Workspace">'+workspaces.map(w=>'<option value="'+esc(w.id)+'" '+(workspace?.id===w.id?'selected':'')+'>'+esc(w.name)+'</option>').join('')+'</select></label><span class="context-divider">/</span><div class="breadcrumbs">'+crumb+'</div><span class="instance-label">'+icon('server')+'Self-hosted</span></div><div class="content">'+content+'</div></main><footer class="app-footer"><span>Startship <span class="footer-separator">/</span> Deployment platform</span><span>Your infrastructure. No middleman.</span></footer></div>';
 bindTheme();
 $('#workspace-switch').onchange=e=>nav('workspace/'+e.target.value);
 $('#logout').onclick=async()=>{await api('logout',{});user=null;nav('login')};
}
function heading(kicker,title,subtitle,actions=''){return '<div class="heading"><div class="heading-copy"><div class="eyebrow">'+kicker+'</div><h1>'+esc(title)+'</h1><p>'+esc(subtitle)+'</p></div><div class="row heading-actions">'+actions+'</div></div>'}
function auth(setup){
 $('#root').innerHTML='<div class="auth"><header class="auth-header"><a class="brand" href="#login">'+brand()+'</a>'+themeButton()+'</header><section class="auth-story"><div class="auth-index"><span>01 — YOUR INFRASTRUCTURE</span><span>BUILT TO SHIP ↗</span></div><div class="auth-message"><h1>From commit.<br>To production.<br><em>On your terms.</em></h1><p>Deploy your applications on the server you own.<br>A straight line from your code to the world.</p></div><div class="pipeline"><div class="pipeline-head"><span>THE SHORTEST PATH TO LIVE</span>'+icon('arrow')+'</div><div class="pipeline-track"><div><span class="pipeline-node">'+icon('branch')+'</span><strong>Push</strong><span>Your repository</span></div><div><span class="pipeline-node">'+icon('box')+'</span><strong>Build</strong><span>Isolated by default</span></div><div><span class="pipeline-node node-live">'+icon('globe')+'</span><strong>Ship</strong><span>Your own domain</span></div></div><div class="pipeline-command"><span>$</span> git push origin main <span class="command-cursor"></span></div></div></section><section class="auth-form"><form><span class="form-index">'+(setup?'FIRST THINGS FIRST':'YOUR CONTROL ROOM')+'</span><h2>'+(setup?'Set up your account.':'Welcome back.')+'</h2><p>'+(setup?'Create the administrator account for this server.':'Sign in to manage your applications.')+'</p>'+field('Username','username','','text','required autocomplete="username" placeholder="Enter your username"')+field('Password','password','','password','required minlength="8" autocomplete="'+(setup?'new-password':'current-password')+'" placeholder="Enter your password"')+(setup?'<p class="password-help">At least 8 characters. There is no password reset, so keep it somewhere safe.</p>':'')+'<div class="form-error" role="alert"></div><button class="primary" type="submit">'+(setup?'Create account':'Sign in')+icon('arrow')+'</button><div class="form-footnote">'+icon('lock')+'Private access to your infrastructure.</div></form></section><footer class="auth-footer"><span>SELF-HOSTED, NOT LIMITED.</span><span>Startship / Deployment platform</span></footer></div>';
 bindTheme();
 $('form').onsubmit=async e=>{e.preventDefault();const button=e.target.querySelector('button');button.disabled=true;const v=Object.fromEntries(new FormData(e.target));try{if(setup)await api('setup',v);await api('login',v);user=await api('me');nav('workspaces')}catch(err){$('.form-error').textContent=err.message}finally{button.disabled=false}};
}
function createWorkspace(){modal('Create workspace','A home for your projects and applications.',field('Workspace name','name','','text','required'),'Create workspace',async f=>{const w=await api('workspaces',Object.fromEntries(f));nav('workspace/'+w.id);toast('Workspace created')})}
function createProject(){modal('Create project','Group related services, sites, and domains together.',field('Project name','name','','text','required placeholder="My Website"'),'Create project',async f=>{const p=await api('projects',{name:f.get('name'),workspace_id:workspace.id});nav('project/'+p.id);toast('Project created')})}
const types=['auto','dockerfile','static','node','python','go','rust'];
function appFields(app={}){
 return field('Application name','name',app.name||'','text','required')+field('Git repository URL','git_url',app.git_url||'','url','required placeholder="https://github.com/you/project"')+'<div class="split">'+field('Branch','branch',app.branch||'main','text','required')+field('Root directory','root_dir',app.root_dir||'.','text','required')+'</div>'+field('Private repository token (optional; leave blank to keep existing)','git_token','','password','autocomplete="off"')+'<div class="split"><label>Build type<select name="build_type">'+types.map(t=>'<option '+((app.build_type||'auto')===t?'selected':'')+'>'+t+'</option>').join('')+'</select></label>'+field('Internal port','port',app.port||8080,'number','required min="1" max="65535"')+'</div><label>Environment variables<textarea name="env" placeholder="NODE_ENV=production&#10;API_KEY=your-value">'+esc(Object.entries(app.env||{}).map(([k,v])=>k+'='+v).join('\n'))+'</textarea></label>'+field('Test command (optional)','test_command',app.test_command||'','text','placeholder="npm test"')+'<label><input type="checkbox" name="auto_deploy" '+(app.auto_deploy?'checked':'')+'>Auto-deploy on git push</label>';
}
function appData(f){const v=Object.fromEntries(f);v.port=Number(v.port);v.auto_deploy=f.has('auto_deploy');v.env={};for(const line of f.get('env').split('\n')){if(!line.trim()||line.trim().startsWith('#'))continue;const i=line.indexOf('=');if(i<1||!/^[A-Za-z_][A-Za-z0-9_]*$/.test(line.slice(0,i).trim()))throw Error('Environment variables must use KEY=VALUE, one per line.');v.env[line.slice(0,i).trim()]=line.slice(i+1)}if(!v.git_token)delete v.git_token;return v}
function addApp(project){modal('Add app from Git','We’ll build your code in an isolated container.',appFields(),'Create application',async f=>{const a=await api('apps',{...appData(f),project_id:project.id});nav('app/'+a.id);toast('Application created')})}
function detectApps(project){
 modal('Discover your applications','Scan the repository root and first-level folders for deployable apps.',field('Git repository URL','git_url','','url','required')+field('Branch','branch','main')+field('Private repository token (optional)','git_token','','password')+'<div id="candidates"></div>','Detect apps',async(f,form)=>{
 if(!form.dataset.scanned){
 const found=await api('detect',Object.fromEntries(f));if(!found.length)throw Error('No supported applications found.');
 $('#candidates').innerHTML='<h3>Choose apps to create</h3>'+found.map((c,i)=>'<label class="candidate"><input type="checkbox" name="candidate" value="'+i+'" checked><span>Root '+rootLabel(c.root_dir)+' <span class="badge">'+esc(c.build_type)+'</span><br><span class="muted small">Port '+c.port+'</span></span></label>').join('');
 form._candidates=found;form.dataset.scanned='yes';form.querySelector('[type=submit]').textContent='Create selected apps';return false;
 }
 const chosen=f.getAll('candidate');if(!chosen.length)throw Error('Select at least one application.');
 for(const i of chosen){const c=form._candidates[Number(i)];await api('apps',{project_id:project.id,git_url:f.get('git_url'),git_token:f.get('git_token'),branch:f.get('branch'),name:c.root_dir==='.'?project.name:c.root_dir,...c})}
 toast(chosen.length+' applications created');render();
 });
}
function addRoute(project,apps){if(!apps.length){toast('Create an application first');return}modal('Connect a domain','Direct traffic to an app using a hostname and path prefix.',field('Hostname','host','','text','required placeholder="example.com"')+field('Path prefix','path_prefix','/','text','required')+'<label>Application<select name="app_id">'+apps.map(a=>'<option value="'+esc(a.id)+'">'+esc(a.name)+' · :'+a.port+'</option>').join('')+'</select></label><label><input type="checkbox" name="strip_prefix">Strip prefix before forwarding</label><div class="note">DNS: point the A record of this hostname to this server’s public IP.</div>','Connect domain',async f=>{await api('routes',{...Object.fromEntries(f),project_id:project.id,strip_prefix:f.has('strip_prefix')});toast('Domain connected');render()})}
async function projectPage(id){
 const [p,apps,routes,deployments]=await Promise.all([api('projects/'+id),api('apps?project_id='+id),api('routes?project_id='+id),api('deployments')]);workspace=workspaces.find(w=>w.id===p.workspace_id);
 const status=a=>deployments.find(d=>d.id===a.active_deployment)?.status;
 shell('<a href="#workspace/'+workspace.id+'">'+esc(workspace.name)+'</a> &nbsp; / &nbsp; '+esc(p.name),
 heading('Project overview',p.name,'Applications and traffic, in one view.','<button id="detect">'+icon('search')+'Detect apps</button><button class="primary" id="add-app">'+icon('plus')+'Add app</button>')+
 '<div class="stats"><div class="stat"><div><span>Applications</span><strong>'+apps.length+'</strong></div>'+icon('box')+'</div><div class="stat stat-positive"><div><span>Running applications</span><strong>'+apps.filter(a=>status(a)==='running').length+'</strong></div>'+icon('activity')+'</div><div class="stat"><div><span>Connected routes</span><strong>'+routes.length+'</strong></div>'+icon('globe')+'</div></div>'+
 '<div class="section-head"><h2>Applications<span class="count">'+apps.length+'</span></h2><span class="small muted">Git-connected services</span></div>'+
 (apps.length?'<div class="grid">'+apps.map(a=>'<a class="card link" href="#app/'+a.id+'"><div class="card-top"><div class="card-icon">'+icon('box')+'</div>'+badge(status(a))+'</div><h3>'+esc(a.name)+'</h3><div class="card-description">'+esc(a.git_url)+'</div><div class="app-meta"><span>'+icon('branch')+esc(a.branch)+'</span><span>'+icon('folder')+rootLabel(a.root_dir)+'</span></div><div class="card-footer"><span>'+esc(a.build_type)+' · Port '+a.port+'</span><span>View application '+icon('arrow')+'</span></div></a>').join('')+'</div>':empty('Your first app starts here','Connect a Git repository or discover apps in a monorepo.'))+
 '<section class="section"><div class="section-head"><div><h2>Domains & routes<span class="count">'+routes.length+'</span></h2><span class="small muted">Public endpoints for this project</span></div><button id="add-route">'+icon('plus')+'Connect domain</button></div>'+
 (routes.length?'<div class="panel table-panel"><table><thead><tr><th>Public router URL</th><th>Application (internal port)</th><th>Prefix</th><th>Actions</th></tr></thead><tbody>'+routes.map(r=>'<tr><td><a href="'+esc(r.url)+'" target="_blank" rel="noopener noreferrer" aria-label="Open '+esc(r.url)+'">'+esc(r.url)+' '+icon('arrow')+'</a></td><td>'+esc(apps.find(a=>a.id===r.app_id)?.name)+' <span class="muted">:'+apps.find(a=>a.id===r.app_id)?.port+'</span></td><td>'+ (r.strip_prefix?'Stripped':'Preserved')+'</td><td><button class="danger delete-route" data-id="'+r.id+'">Delete</button></td></tr>').join('')+'</tbody></table></div>':empty('No domains connected yet','Add a hostname and route traffic to your app.'))+'<details class="note"><summary>DNS & routing setup</summary><p>Point the A record of each host to this server’s public IP. Open a router URL above to visit your app; internal container ports are not published. URLs use this server’s configured router port, not the dashboard port. If a gateway maps different public ports, use its public URL instead.</p></details></section>');
 $('#add-app').onclick=()=>addApp(p);$('#detect').onclick=()=>detectApps(p);$('#add-route').onclick=()=>addRoute(p,apps);
 document.querySelectorAll('.delete-route').forEach(b=>b.onclick=async()=>{try{await api('routes/'+b.dataset.id,null,'DELETE');render();toast('Route deleted')}catch(e){toast(e.message)}});
}
async function appPage(id){
 const app=await api('apps/'+id),p=await api('projects/'+app.project_id),routes=(await api('routes?project_id='+app.project_id)).filter(r=>r.app_id===id);workspace=workspaces.find(w=>w.id===p.workspace_id);
 shell('<a href="#project/'+p.id+'">'+esc(p.name)+'</a> &nbsp; / &nbsp; '+esc(app.name),
 heading('Application',app.name,app.git_url,'<span id="active-status"></span>'+(routes.length?'<a class="button" href="'+esc(routes[0].url)+'" target="_blank" rel="noreferrer">Open app '+icon('arrow')+'</a>':'')+'<button class="primary" id="deploy">'+icon('arrow')+'Deploy now</button>')+
 '<div class="tabs" aria-label="Application views"><button class="active" data-tab="deployments" aria-pressed="true">'+icon('layers')+'Deployments</button><button data-tab="settings" aria-pressed="false">'+icon('settings')+'Settings</button><button data-tab="webhook" aria-pressed="false">'+icon('branch')+'Git integration</button></div><div id="tab-content"></div>');
 $('#deploy').onclick=async e=>{const button=e.currentTarget;button.disabled=true;try{const d=await api('apps/'+id+'/deploy',{});selectedDeployment=d.id;toast('Deployment queued');await showDeployments()}catch(err){toast(err.message)}finally{button.disabled=false}};
 let activeTab='deployments';
 async function showDeployments(){
 const previousLog=$('#build-log');const previousScroll=previousLog?.scrollTop||0;const follow=!previousLog||previousLog.scrollHeight-previousLog.scrollTop-previousLog.clientHeight<40;
 const ds=await api('deployments?app_id='+id);if(activeTab!=='deployments'||!$('#tab-content'))return;
 const previousTableScroll=$('#deployment-history')?.scrollLeft||0;
 const previousTableTop=$('#deployment-history')?.scrollTop||0;
 const latest=ds.slice().reverse();if(!latest.some(d=>d.id===selectedDeployment))selectedDeployment=latest[0]?.id||'';
 $('#tab-content').innerHTML='<div class="section-head"><h2>Release history</h2><span class="small muted">Branch '+esc(app.branch)+' &nbsp; · &nbsp; Root '+rootLabel(app.root_dir)+' &nbsp; · &nbsp; Port '+app.port+'</span></div><div class="deployment-layout"><div>'+
 (latest.length?'<div class="panel table-panel" id="deployment-history"><table><thead><tr><th>Deployment</th><th>Status</th><th>Commit</th><th>Created</th></tr></thead><tbody>'+latest.map(d=>'<tr class="'+(d.id===selectedDeployment?'selected':'')+'"><td><button class="select-deploy" aria-pressed="'+(d.id===selectedDeployment)+'" data-id="'+d.id+'">'+icon('box')+d.id.slice(0,8)+'</button></td><td>'+badge(d.status)+'</td><td class="muted commit">'+esc(d.commit?.slice(0,7)||'Pending')+'</td><td class="muted">'+new Date(d.created).toLocaleString()+'</td></tr>').join('')+'</tbody></table></div>':empty('Ready when you are','Deploy your first release to see build output here.'))+
 '</div><section class="section panel"><div class="log-head"><span class="log-title">'+icon('terminal')+'Build output <span class="muted" id="log-id"></span></span><span class="live"><span class="dot"></span>Live · 2s</span></div><pre class="log" id="build-log">Waiting for a deployment…</pre></section></div>';
 if($('#deployment-history')){$('#deployment-history').scrollLeft=previousTableScroll;$('#deployment-history').scrollTop=previousTableTop}
 document.querySelectorAll('.select-deploy').forEach(b=>b.onclick=async()=>{selectedDeployment=b.dataset.id;await showDeployments()});
 const current=latest.find(d=>['queued','building','testing','starting'].includes(d.status))||latest.find(d=>d.status==='running')||latest[0];$('#active-status').innerHTML=badge(current?.status);
 await updateLog();
 if(!follow&&$('#build-log'))$('#build-log').scrollTop=previousScroll;
 }
 async function updateLog(){if(activeTab!=='deployments'||!selectedDeployment||!$('#build-log'))return;const d=await api('deployments/'+selectedDeployment);if(!$('#build-log'))return;const log=$('#build-log');const atEnd=log.scrollHeight-log.scrollTop-log.clientHeight<40;log.textContent=d.log||'Deployment '+d.status+'…';$('#log-id').textContent=' / '+d.id.slice(0,8);if(atEnd)log.scrollTop=log.scrollHeight}
 async function tab(name){
 activeTab=name;clearInterval(timer);document.querySelectorAll('[data-tab]').forEach(b=>{b.classList.toggle('active',b.dataset.tab===name);b.setAttribute('aria-pressed',String(b.dataset.tab===name))});
 if(name==='deployments'){await showDeployments();timer=setInterval(()=>{if(location.hash==='#app/'+id)showDeployments().catch(e=>toast(e.message))},2000)}
 if(name==='settings'){$('#tab-content').innerHTML='<div class="panel panel-pad" style="max-width:730px"><h2>Application settings</h2><p class="small">Changes apply on the next deployment. Existing credentials are preserved when left blank.</p><form id="settings-form">'+appFields(app)+'<div class="form-error"></div><div class="form-actions"><button class="primary">Save changes</button></div></form></div>';$('#settings-form').onsubmit=async e=>{e.preventDefault();try{Object.assign(app,await api('apps/'+id,appData(new FormData(e.target)),'PATCH'));toast('Settings saved')}catch(err){$('.form-error').textContent=err.message}}}
 if(name==='webhook'){const url=location.origin+'/api/webhooks/github/'+id;$('#tab-content').innerHTML='<div class="panel panel-pad" style="max-width:760px"><h2>Push code. We’ll take it from here.</h2><p>Connect GitHub to automatically build and deploy every push to <strong>'+esc(app.branch)+'</strong>.</p><label>Webhook URL<div class="copy-row"><input id="hook-url" readonly value="'+esc(url)+'"><button data-copy="hook-url">Copy</button></div></label><label>Webhook secret<div class="copy-row"><input id="hook-secret" readonly value="'+esc(app.webhook_secret)+'"><button data-copy="hook-secret">Copy</button></div></label><div class="note">In GitHub: repository → Settings → Webhooks → Add webhook. Paste this URL, choose application/json, paste the secret, and select “Just the push event”. Enable auto-deploy in application settings.<br><br>Use your publicly reachable dashboard domain for the webhook URL; localhost is not reachable by GitHub.</div></div>';document.querySelectorAll('[data-copy]').forEach(b=>b.onclick=async()=>{try{await navigator.clipboard.writeText($('#'+b.dataset.copy).value);toast('Copied to clipboard')}catch{toast('Select and copy the value manually')}})}
 }
 document.querySelectorAll('[data-tab]').forEach(b=>b.onclick=()=>tab(b.dataset.tab).catch(e=>toast(e.message)));
 await tab('deployments');
}
function collectionCard(item,kind,apps=[]){
 const current=kind==='workspace'&&workspace?.id===item.id;
 const projectApps=apps.filter(a=>a.project_id===item.id);
 const created=item.created?new Date(item.created).toLocaleDateString(undefined,{month:'short',day:'numeric'}):'';
 const detail=kind==='workspace'?'Workspace':projectApps.length+' application'+(projectApps.length===1?'':'s')+(projectApps.length?' · '+[...new Set(projectApps.map(a=>a.build_type))].map(esc).join(', '):' · Ready to deploy');
 return '<a class="card link" href="#'+kind+'/'+esc(item.id)+'"><div class="card-top"><div class="card-icon">'+icon(kind==='workspace'?'layers':'grid')+'</div>'+(current?'<span class="badge running">Current</span>':'<span class="card-arrow">'+icon('arrow')+'</span>')+'</div><h3>'+esc(item.name)+'</h3><div class="card-description">'+detail+'</div><div class="card-footer"><span>'+(created?'Created '+esc(created):'Not deployed')+'</span><span>Open '+kind+' '+icon('arrow')+'</span></div></a>';
}
function releasePanel(deployments,apps){
 const latest=deployments.slice().sort((a,b)=>new Date(b.created)-new Date(a.created)),recent=latest.slice(0,32).reverse();
 return '<aside class="panel activity-panel"><div class="section-head"><h2>Deployment activity</h2>'+icon('activity')+'</div><div class="activity-total"><strong>'+deployments.length+'</strong><span>total releases</span></div>'+
 (recent.length?'<div class="release-pulse" aria-label="Recent deployment statuses">'+recent.map(d=>'<span class="'+esc(d.status)+'" title="'+esc(d.status)+' · '+esc(new Date(d.created).toLocaleString())+'"></span>').join('')+'</div><div class="pulse-caption"><span>Older</span><span>Latest '+recent.length+' releases</span></div>':'<p class="activity-empty">Your first release starts with a deploy.</p>')+
 '<div class="release-list"><h2>Recent releases</h2>'+latest.slice(0,4).map(d=>'<a class="release-item" href="#app/'+esc(d.app_id)+'"><div><strong>'+esc(apps.find(a=>a.id===d.app_id)?.name||'Application')+'</strong><small>'+esc(d.commit?.slice(0,7)||d.id.slice(0,7))+' · '+esc(new Date(d.created).toLocaleDateString(undefined,{month:'short',day:'numeric'}))+'</small></div>'+badge(d.status)+'</a>').join('')+(!latest.length?'<p class="activity-empty">No deployments in this workspace.</p>':'')+'</div></aside>';
}
async function render(){
 clearInterval(timer);const version=++viewVersion;
 try{
 const setup=await api('setup');if(version!==viewVersion)return;if(setup.required){auth(true);return}
 if(!user){try{user=await api('me')}catch{auth(false);return}}
 const parts=location.hash.slice(1).split('/');if(parts[0]==='login'){auth(false);return}
 workspaces=await api('workspaces');workspace=workspaces.find(w=>w.id===parts[1])||workspaces.find(w=>w.id===localStorage.getItem('workspace'))||workspaces[0];
 if(parts[0]==='project'){await projectPage(parts[1]);return}
 if(parts[0]==='app'){await appPage(parts[1]);return}
 if(parts[0]==='workspace'&&workspace){
  localStorage.setItem('workspace',workspace.id);
  const [projects,allApps,allDeployments]=await Promise.all([api('projects?workspace_id='+workspace.id),api('apps'),api('deployments')]);
  const apps=allApps.filter(a=>projects.some(p=>p.id===a.project_id));
  const deployments=allDeployments.filter(d=>apps.some(a=>a.id===d.app_id));
  const running=apps.filter(a=>deployments.find(d=>d.id===a.active_deployment)?.status==='running').length;
  shell(esc(workspace.name),
   heading('Workspace overview','Projects','Your applications, from first commit to latest release.','<button id="new-project" class="primary">'+icon('plus')+'New project</button>')+
   '<div class="stats"><div class="stat"><div><span>Total projects</span><strong>'+projects.length+'</strong></div>'+icon('grid')+'</div><div class="stat"><div><span>Applications</span><strong>'+apps.length+'</strong></div>'+icon('box')+'</div><div class="stat stat-positive"><div><span>Running applications</span><strong>'+running+'</strong></div>'+icon('activity')+'</div></div>'+
   '<div class="workspace-layout"><section><div class="section-head"><h2>All projects<span class="count">'+projects.length+'</span></h2><span class="small muted">'+esc(workspace.name)+'</span></div>'+
   (projects.length?'<div class="grid project-grid">'+projects.map(p=>collectionCard(p,'project',apps)).join('')+'</div>':empty('No projects yet','Create a project, then connect your first repository.'))+'</section>'+releasePanel(deployments,apps)+'</div>');
  $('#new-project').onclick=createProject;return;
 }
 shell('Workspaces',
  heading('Infrastructure','Workspaces','Organize the projects running on your server.','<button class="primary" id="new-workspace">'+icon('plus')+'New workspace</button>')+
  '<div class="section-head"><h2>All workspaces<span class="count">'+workspaces.length+'</span></h2></div>'+
  (workspaces.length?'<div class="grid">'+workspaces.map(w=>collectionCard(w,'workspace')).join('')+'</div>':empty('No workspaces yet','Create a workspace to organize your projects.')));
 $('#new-workspace').onclick=createWorkspace;
 }catch(e){$('#root').innerHTML='<div class="error-page"><h1>Something went wrong.</h1><p>'+esc(e.message)+'</p><a class="button" href="#workspaces">Back to workspaces</a></div>'}
}
window.addEventListener('hashchange',render);render();
