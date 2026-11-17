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
 copy: '<rect x="8" y="8" width="13" height="13" rx="2"/><path d="M16 8V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h3"/>',
 expand: '<path d="M8 3H3v5m13-5h5v5M3 16v5h5m13-5v5h-5"/>',
 settings: '<path d="M4 7h16M4 17h16"/><circle cx="8" cy="7" r="3" fill="var(--bg)"/><circle cx="16" cy="17" r="3" fill="var(--bg)"/>',
 search: '<circle cx="10" cy="10" r="6"/><path d="m15 15 6 6"/>',
 lock: '<rect x="5" y="10" width="14" height="11" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3m-4 5v2"/>',
 logout: '<path d="M9 4H5a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h4m6-13 5 5-5 5m-6-5h11"/>',
 menu: '<path d="M4 6h16M4 12h16M4 18h16"/>',
 chevron: '<path d="m8 9 4-4 4 4m-8 6 4 4 4-4"/>',
 back: '<path d="M19 12H5m6-6-6 6 6 6"/>',
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
 if(!$('#theme-toggle'))return;
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
function sidebarNavigation({project,apps=[],app,section='deployments'}={}){
 const item=(href,shape,label,current=false)=>'<a class="sidebar-link '+(current?'active':'')+'" '+(current?'aria-current="page"':'')+' href="'+href+'">'+icon(shape)+'<span>'+label+'</span></a>';
 if(!project)return '<div class="sidebar-label">Workspace</div>'+item('#workspace/'+esc(workspace?.id||''),'grid','Projects',location.hash!=='#workspaces')+'<button class="sidebar-link" id="sidebar-new-project">'+icon('plus')+'New project</button>';
 return item('#workspace/'+esc(project.workspace_id),'back','All projects')+'<div class="sidebar-project"><span class="sidebar-label">Project</span><strong title="'+esc(project.name)+'">'+esc(project.name)+'</strong></div>'+
  (app?'<label class="sidebar-app-picker">Application <span class="count">'+apps.length+'</span><select id="sidebar-app" aria-label="Application">'+apps.map(a=>'<option value="'+esc(a.id)+'" '+(a.id===app.id?'selected':'')+'>'+esc(a.name)+'</option>').join('')+'</select></label><div class="sidebar-label">Application controls</div>'+applicationViews.filter(v=>v[0]!=='webhook').map(([name,shape,label])=>item('#app/'+esc(app.id)+'/'+name,shape,label,section===name)).join(''):'')+
  '<div class="sidebar-label project-nav-label">Project controls</div>'+item('#'+(app?'app/'+esc(app.id):'project/'+esc(project.id))+'/webhook','branch','Source & Git',section==='webhook');
}
function shell(crumb,content,context={}){
 const workspaceName=workspace?.name||'Choose workspace';
 $('#root').innerHTML='<div class="dashboard sidebar-dashboard"><aside class="sidebar" id="sidebar" aria-label="Dashboard navigation"><div class="sidebar-top"><a href="#'+(workspace?'workspace/'+esc(workspace.id):'workspaces')+'" class="brand">'+brand()+'</a><button class="icon-button sidebar-close" id="close-sidebar" aria-label="Close navigation">'+icon('close')+'</button></div><button class="sidebar-selector" id="workspace-menu-toggle" aria-haspopup="dialog" aria-expanded="false" aria-controls="workspace-menu"><span class="workspace-symbol">'+icon('layers')+'</span><span class="selector-copy"><small>Workspace</small><strong>'+esc(workspaceName)+'</strong></span>'+icon('chevron')+'</button><nav class="sidebar-navigation" aria-label="'+(context.project?'Project':'Workspace')+' navigation">'+sidebarNavigation(context)+'</nav><div class="sidebar-bottom"><span class="sidebar-instance">'+icon('server')+'Self-hosted <span class="dot"></span></span><button class="sidebar-selector account-selector" id="account-menu-toggle" aria-haspopup="dialog" aria-expanded="false" aria-controls="account-menu"><span class="avatar">'+esc(user.username[0].toUpperCase())+'</span><span class="selector-copy"><strong>'+esc(user.username)+'</strong><small>Personal account</small></span>'+icon('chevron')+'</button></div></aside><button class="sidebar-scrim" id="sidebar-scrim" aria-label="Close navigation" hidden></button><div class="dashboard-body"><main class="main"><div class="contextbar"><button class="icon-button mobile-nav-toggle" id="open-sidebar" aria-label="Open navigation" aria-expanded="false" aria-controls="sidebar">'+icon('menu')+'</button><div class="breadcrumbs">'+crumb+'</div><span class="instance-label">'+icon('lock')+'Private infrastructure</span></div><div class="content">'+content+'</div></main><footer class="app-footer"><span>Startship <span class="footer-separator">/</span> Deployment platform</span><span>Your infrastructure. No middleman.</span></footer></div></div><div class="sidebar-popover" id="workspace-menu" role="dialog" aria-label="Switch workspace" hidden><div class="popover-title">Your workspaces</div><div class="workspace-menu-list">'+workspaces.map(w=>'<a class="menu-item '+(workspace?.id===w.id?'selected':'')+'" href="#workspace/'+esc(w.id)+'" '+(workspace?.id===w.id?'aria-current="true"':'')+'>'+icon('layers')+'<span>'+esc(w.name)+'</span>'+(workspace?.id===w.id?'<span class="current-dot"></span>':'')+'</a>').join('')+'</div><div class="menu-divider"></div><button class="menu-item" id="menu-create-workspace">'+icon('plus')+'Create workspace</button><a class="menu-item" href="#workspaces">'+icon('grid')+'Manage workspaces</a></div><div class="sidebar-popover" id="account-menu" role="dialog" aria-label="Account options" hidden><div class="popover-title">Signed in as <strong>'+esc(user.username)+'</strong></div><button class="menu-item" id="account-details">'+icon('lock')+'Account details</button>'+themeButton()+'<div class="menu-divider"></div><button class="menu-item" id="logout">'+icon('logout')+'Sign out</button></div>';
 bindSidebarMenus();bindTheme();
 $('#sidebar-new-project')?.addEventListener('click',()=>{setSidebarOpen(false);createProject()});
 $('#sidebar-app')?.addEventListener('change',e=>nav('app/'+e.target.value+'/'+(location.hash.split('/')[2]||context.section||'deployments')));
 $('#menu-create-workspace').onclick=()=>{setSidebarOpen(false);createWorkspace()};
 $('#account-details').onclick=()=>{setSidebarOpen(false);modal('Your account','Your signed-in identity on this Startship server.',field('Username','username',user.username,'text','readonly')+'<p class="small muted">Appearance is saved on this browser. Use the account menu to switch between light and dark.</p>','Done',async()=>{})};
 $('#logout').onclick=async()=>{try{await api('logout',{});user=null;nav('login')}catch(err){toast(err.message)}};
}
function closeSidebarMenus(){
 document.querySelectorAll('.sidebar-popover').forEach(p=>p.hidden=true);
 document.querySelectorAll('.sidebar-selector').forEach(b=>b.setAttribute('aria-expanded','false'));
}
function positionSidebarMenu(menu,button){
 const r=button.getBoundingClientRect(),gap=8,edge=12;
 menu.style.maxHeight=Math.max(100,window.innerHeight-edge*2)+'px';
 const height=menu.getBoundingClientRect().height,below=window.innerHeight-r.bottom-gap-edge,above=r.top-gap-edge;
 let top=below>=height||below>=above?r.bottom+gap:r.top-height-gap;
 menu.style.top=Math.max(edge,Math.min(top,window.innerHeight-height-edge))+'px';
 menu.style.left=Math.max(edge,Math.min(r.left,window.innerWidth-menu.offsetWidth-edge))+'px';
}
function bindSidebarMenus(){
 for(const name of ['workspace','account']){
  const button=$('#'+name+'-menu-toggle'),menu=$('#'+name+'-menu');
  button.onclick=()=>{const open=menu.hidden;closeSidebarMenus();if(open){menu.hidden=false;button.setAttribute('aria-expanded','true');positionSidebarMenu(menu,button);menu.querySelector('a,button')?.focus()}};
  menu.querySelectorAll('a').forEach(a=>a.addEventListener('click',()=>{closeSidebarMenus();setSidebarOpen(false)}));
 }
 $('#open-sidebar').onclick=()=>setSidebarOpen(true);
 $('#close-sidebar').onclick=$('#sidebar-scrim').onclick=()=>setSidebarOpen(false);
 $('.sidebar-navigation').addEventListener('click',e=>{if(e.target.closest('a'))setSidebarOpen(false)});
}
function setSidebarOpen(open){
 closeSidebarMenus();$('.sidebar-dashboard')?.classList.toggle('sidebar-open',open);
 if($('.dashboard-body'))$('.dashboard-body').inert=open;
 if($('#sidebar-scrim'))$('#sidebar-scrim').hidden=!open;
 $('#open-sidebar')?.setAttribute('aria-expanded',String(open));
 if(open)$('#close-sidebar').focus();else $('#open-sidebar')?.focus();
}
document.addEventListener('click',e=>{if(!e.target.closest('.sidebar-popover,.sidebar-selector'))closeSidebarMenus()});
document.addEventListener('keydown',e=>{
 if($('#modal').open)return;
 if(e.key==='Escape'){
  const open=document.querySelector('.sidebar-selector[aria-expanded="true"]');
  if(open){closeSidebarMenus();open.focus()}else if($('.sidebar-open'))setSidebarOpen(false);
 }
 if(e.key==='Tab'){
  const scope=document.querySelector('.sidebar-popover:not([hidden])')||($('.sidebar-open')?$('#sidebar'):null);
  if(!scope)return;
  const controls=[...scope.querySelectorAll('a,button,select')].filter(el=>el.getClientRects().length),first=controls[0],last=controls.at(-1);
  if(e.shiftKey&&document.activeElement===first){e.preventDefault();last?.focus()}
  if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first?.focus()}
 }
});
window.addEventListener('resize',()=>{closeSidebarMenus();if(window.innerWidth>900&&$('.sidebar-open'))setSidebarOpen(false)});
function heading(kicker,title,subtitle,actions=''){return '<div class="heading"><div class="heading-copy"><div class="eyebrow">'+kicker+'</div><h1>'+esc(title)+'</h1><p>'+esc(subtitle)+'</p></div><div class="row heading-actions">'+actions+'</div></div>'}
function auth(setup){
 $('#root').innerHTML='<div class="auth"><header class="auth-header"><a class="brand" href="#login">'+brand()+'</a>'+themeButton()+'</header><section class="auth-story"><div class="auth-index"><span>01 — YOUR INFRASTRUCTURE</span><span>BUILT TO SHIP ↗</span></div><div class="auth-message"><h1>From commit.<br>To production.<br><em>On your terms.</em></h1><p>Deploy your applications on the server you own.<br>A straight line from your code to the world.</p></div><div class="pipeline"><div class="pipeline-head"><span>THE SHORTEST PATH TO LIVE</span>'+icon('arrow')+'</div><div class="pipeline-track"><div><span class="pipeline-node">'+icon('branch')+'</span><strong>Push</strong><span>Your repository</span></div><div><span class="pipeline-node">'+icon('box')+'</span><strong>Build</strong><span>Isolated by default</span></div><div><span class="pipeline-node node-live">'+icon('globe')+'</span><strong>Ship</strong><span>Your own domain</span></div></div><div class="pipeline-command"><span>$</span> git push origin main <span class="command-cursor"></span></div></div></section><section class="auth-form"><form><span class="form-index">'+(setup?'FIRST THINGS FIRST':'YOUR CONTROL ROOM')+'</span><h2>'+(setup?'Set up your account.':'Welcome back.')+'</h2><p>'+(setup?'Create the administrator account for this server.':'Sign in to manage your applications.')+'</p>'+field('Username','username','','text','required autocomplete="username" placeholder="Enter your username"')+field('Password','password','','password','required minlength="8" autocomplete="'+(setup?'new-password':'current-password')+'" placeholder="Enter your password"')+(setup?'<p class="password-help">At least 8 characters. There is no password reset, so keep it somewhere safe.</p>':'')+'<div class="form-error" role="alert"></div><button class="primary" type="submit">'+(setup?'Create account':'Sign in')+icon('arrow')+'</button><div class="form-footnote">'+icon('lock')+'Private access to your infrastructure.</div></form></section><footer class="auth-footer"><span>SELF-HOSTED, NOT LIMITED.</span><span>Startship / Deployment platform</span></footer></div>';
 bindTheme();
 $('form').onsubmit=async e=>{e.preventDefault();const button=e.target.querySelector('button');button.disabled=true;const v=Object.fromEntries(new FormData(e.target));try{if(setup)await api('setup',v);await api('login',v);user=await api('me');nav('')}catch(err){$('.form-error').textContent=err.message}finally{button.disabled=false}};
}
function createWorkspace(){modal('Create workspace','A home for your projects and applications.',field('Workspace name','name','','text','required'),'Create workspace',async f=>{const w=await api('workspaces',Object.fromEntries(f));nav('workspace/'+w.id);toast('Workspace created')})}
function sourceFields(project={}){
 return field('Project repository URL','git_url',project.git_url||'','url','required placeholder="https://github.com/you/project"')+field('Shared branch','branch',project.branch||'main','text','required')+field('Repository token (leave blank to keep existing)','git_token','','password','autocomplete="off"');
}
function sourceData(f){const v={git_url:f.get('git_url'),branch:f.get('branch')};if(f.get('git_token'))v.git_token=f.get('git_token');return v}
function editProjectSource(project){
 modal('Project source','One repository, branch, and credential set for this project. Changes apply to every app on its next deployment.',(project.source_conflict?'<div class="note">The old configuration contains different app sources. Choose the single repository this project should use. No apps or running deployments will be deleted.</div>':'')+sourceFields(project),'Save project source',async f=>{await api('projects/'+project.id,sourceData(f),'PATCH');await render();toast('Project source saved')});
}
function createProject(){
 modal('Create project','Connect one repository. A single-app project builds its root; a monorepo can define apps for frontend, backend, or daemons in that same repository.',field('Project name','name','','text','required placeholder="My Website"')+sourceFields()+'<label>Repository layout<select name="layout"><option value="single">Single application — repository root</option><option value="monorepo">Monorepo — choose app folders</option></select></label>','Create project',async(f,form)=>{
  const p=form._project||await api('projects',{name:f.get('name'),workspace_id:workspace.id,...sourceData(f)});form._project=p;
  if(f.get('layout')==='single'){const app=await api('apps',{project_id:p.id,name:p.name,root_dir:'.',build_type:'auto',port:8080});nav('app/'+app.id)}else nav('project/'+p.id);
  toast('Project created');
 });
}
const types=['auto','dockerfile','static','node','python','go','rust'];
function appFields(app={},wide=false){
 const identity=field('Application name','name',app.name||'','text','required')+field('Root directory in project repository','root_dir',app.root_dir||'.','text','required placeholder="frontend"');
 const build='<div class="split"><label>Build type<select name="build_type">'+types.map(t=>'<option '+((app.build_type||'auto')===t?'selected':'')+'>'+t+'</option>').join('')+'</select></label>'+field('Internal port','port',app.port||8080,'number','required min="1" max="65535"')+'</div>'+field('Test command (optional)','test_command',app.test_command||'','text','placeholder="npm test"');
 const environment='<label>Environment variables<textarea name="env" placeholder="NODE_ENV=production&#10;API_KEY=your-value">'+esc(Object.entries(app.env||{}).map(([k,v])=>k+'='+v).join('\n'))+'</textarea></label>';
 const automatic='<label class="inline-check"><input type="checkbox" name="auto_deploy" '+(app.auto_deploy?'checked':'')+'>Deploy this app on project pushes</label>';
 return wide?'<div class="settings-columns"><section class="panel panel-pad"><h3>Build & runtime</h3>'+identity+build+'</section><section class="panel panel-pad"><h3>Environment</h3>'+environment+automatic+'<p class="small muted">Use one KEY=VALUE per line. Repository, branch, credentials, and webhook are managed once in Project source.</p></section></div>':identity+build+environment+automatic;
}
function appData(f){const v=Object.fromEntries(f);v.port=Number(v.port);v.auto_deploy=f.has('auto_deploy');v.env={};for(const line of f.get('env').split('\n')){if(!line.trim()||line.trim().startsWith('#'))continue;const i=line.indexOf('=');if(i<1||!/^[A-Za-z_][A-Za-z0-9_]*$/.test(line.slice(0,i).trim()))throw Error('Environment variables must use KEY=VALUE, one per line.');v.env[line.slice(0,i).trim()]=line.slice(i+1)}return v}
function addApp(project){
 if(!project.git_url||project.source_conflict){editProjectSource(project);return}
 modal('Add app from project repository','Choose an app or daemon inside the project’s shared repository, not a different source.','<p class="note">'+esc(project.git_url)+' · '+esc(project.branch)+'</p>'+appFields(),'Create application',async f=>{const a=await api('apps',{...appData(f),project_id:project.id});nav('app/'+a.id);toast('Application created')});
}
function detectApps(project,apps=[]){
 if(!project.git_url||project.source_conflict){editProjectSource(project);return}
 modal('Discover apps in this repository','Scan the project’s repository root and first-level folders for frontend, backend, or daemon apps.','<p class="note">'+esc(project.git_url)+' · '+esc(project.branch)+'</p><div id="candidates"></div>','Scan project repository',async(f,form)=>{
  if(!form.dataset.scanned){
   const found=await api('detect',{project_id:project.id});if(!found.length)throw Error('No supported applications found.');
   $('#candidates').innerHTML='<h3>Choose apps to create</h3>'+found.map((c,i)=>{const exists=apps.some(a=>a.root_dir===c.root_dir);return '<label class="candidate"><input type="checkbox" name="candidate" value="'+i+'" '+(exists?'disabled':'checked')+'><span>Root '+rootLabel(c.root_dir)+' <span class="badge">'+esc(c.build_type)+'</span><br><span class="muted small">'+(exists?'Already added':'Port '+c.port)+'</span></span></label>'}).join('');
   form._candidates=found;form.dataset.scanned='yes';form.querySelector('[type=submit]').textContent='Create selected apps';return false;
  }
  const chosen=f.getAll('candidate');if(!chosen.length)throw Error('Select at least one application.');
  for(const i of chosen){const c=form._candidates[Number(i)];await api('apps',{project_id:project.id,name:c.root_dir==='.'?project.name:c.root_dir,...c})}
  toast(chosen.length+' applications created');render();
 });
}
function addRoute(project,apps,selectedId){if(!apps.length){toast('Create an application first');return}modal('Connect a domain','Direct traffic to an app using a hostname and path prefix.',field('Hostname','host','','text','required placeholder="example.com"')+field('Path prefix','path_prefix','/','text','required')+'<label>Application<select name="app_id">'+apps.map(a=>'<option value="'+esc(a.id)+'" '+(a.id===selectedId?'selected':'')+'>'+esc(a.name)+' · :'+a.port+'</option>').join('')+'</select></label><label><input type="checkbox" name="strip_prefix">Strip prefix before forwarding</label><div class="note">DNS: point the A record of this hostname to this server’s public IP.</div>','Connect domain',async f=>{await api('routes',{...Object.fromEntries(f),project_id:project.id,strip_prefix:f.has('strip_prefix')});toast('Domain connected');render()})}
const applicationViews=[['deployments','layers','Deployments'],['domains','globe','Domains'],['settings','settings','App settings'],['webhook','branch','Source & Git']];
function projectGitContent(project){
 const url=location.origin+'/api/webhooks/github/'+project.id;
 return '<div class="section-head"><div><h2>Project source & Git</h2><span class="small muted">Shared by every application in this project.</span></div><button id="edit-project-source">'+icon('settings')+'Edit source</button></div><div class="integration-layout"><div class="panel panel-pad"><h2>Project GitHub webhook</h2><p class="small">One source for <strong>'+esc(project.name)+'</strong>: '+esc(project.git_url||'Not configured')+' · '+esc(project.branch||'main')+'. Pushes deploy the apps with Auto-deploy enabled.</p><label>Webhook URL<div class="copy-row"><input id="hook-url" readonly value="'+esc(url)+'"><button data-copy="hook-url">Copy</button></div></label><label>Webhook secret<div class="copy-row"><input id="hook-secret" readonly value="'+esc(project.webhook_secret)+'"><button data-copy="hook-secret">Copy</button></div></label></div><div class="panel panel-pad integration-guide"><h2>Connect the project repository</h2><ol><li>Open your repository’s Settings → Webhooks → Add webhook.</li><li>Paste this URL and secret; choose <code>application/json</code> and “Just the push event”.</li><li>Enable Auto-deploy for the apps that should rebuild on project pushes.</li></ol><p class="small muted">Use your publicly reachable dashboard domain. GitHub cannot reach localhost.</p></div></div>';
}
function bindProjectGit(project){
 $('#edit-project-source').onclick=()=>editProjectSource(project);
 document.querySelectorAll('[data-copy]').forEach(b=>b.onclick=async()=>{try{await navigator.clipboard.writeText($('#'+b.dataset.copy).value);toast('Copied to clipboard')}catch{toast('Select and copy the value manually')}});
}
function deploymentStatus(app,deployments){return deployments.find(d=>d.id===app.active_deployment)?.status}
async function projectPage(id,selectedId='',initialTab='deployments'){
 const version=viewVersion;
 const [p,apps,routes,deployments]=await Promise.all([api('projects/'+id),api('apps?project_id='+id),api('routes?project_id='+id),api('deployments')]);
 if(version!==viewVersion)return;
 workspace=workspaces.find(w=>w.id===p.workspace_id);
 if(workspace)localStorage.setItem('workspace',workspace.id);
 const app=apps.find(a=>a.id===selectedId)||apps[0],running=apps.filter(a=>deploymentStatus(a,deployments)==='running').length;
 shell('<a href="#workspace/'+esc(p.workspace_id)+'">Projects</a> &nbsp; / &nbsp; '+esc(p.name),
  heading('Project',p.name,apps.length+' app'+(apps.length===1?'':'s')+' · '+running+' running · '+routes.length+' route'+(routes.length===1?'':'s'),'<button id="detect">'+icon('search')+'Detect apps</button><button id="add-app">'+icon('plus')+'Add app</button>')+
  '<div class="project-source-summary"><span>'+icon('branch')+'<strong>Project source</strong> '+esc(p.git_url||'Not configured')+'</span><span>'+esc(p.branch||'')+'</span></div>'+(p.source_conflict?'<div class="note source-warning">Existing apps have different sources. Choose one project repository before editing or deploying apps.</div>':'')+
  (app?'<section class="workbench" id="app-workbench" aria-label="'+esc(app.name)+' controls"></section>':
   initialTab==='webhook'?projectGitContent(p):empty('Choose apps from this repository','Add an app at the repository root, or detect frontend, backend, and daemon folders inside this project’s monorepo.')),
  {project:p,apps,app,section:initialTab});
 $('#add-app').onclick=()=>addApp(p);$('#detect').onclick=()=>detectApps(p,apps);
 if(app)await mountApplication(p,app,apps,routes,initialTab,deployments);
 else if(initialTab==='webhook')bindProjectGit(p);
}
async function appPage(id,tab='deployments'){const version=viewVersion,app=await api('apps/'+id);if(version===viewVersion)await projectPage(app.project_id,id,tab)}
async function mountApplication(project,app,apps,allRoutes,initialTab,deployments){
 const version=viewVersion,isCurrent=()=>version===viewVersion&&!!$('#app-workbench');
 let activeTab='',loading=false,tabVersion=0,expandedLog=false;
 const ownRoutes=()=>allRoutes.filter(r=>r.app_id===app.id);
 $('#app-workbench').innerHTML='<div class="app-toolbar"><div class="app-identity"><h2>'+esc(app.name)+'</h2><span id="active-status"></span></div><div class="app-actions">'+(ownRoutes().length?'<a class="button" href="'+esc(ownRoutes()[0].url)+'" target="_blank" rel="noreferrer">Open app '+icon('arrow')+'</a>':'')+'<button id="redeploy" title="Restart the active release with current settings, without building">Redeploy</button><button id="rebuild-no-cache">Rebuild without cache</button><button class="primary" id="deploy">'+icon('arrow')+'Deploy now</button></div></div>'+
 '<div class="app-details"><span>'+icon('folder')+'Root '+rootLabel(app.root_dir)+'</span><span>'+icon('server')+esc(app.build_type)+' · :'+app.port+'</span><label class="inline-check"><input type="checkbox" id="quick-auto-deploy" '+(app.auto_deploy?'checked':'')+'>Auto-deploy</label></div>'+
 '<div class="view-caption"><span id="view-title"></span><span class="poll-status" id="poll-status">'+icon('activity')+'Live updates · 2s</span></div><div id="tab-content"></div>';
 $('#quick-auto-deploy').onchange=async e=>{const input=e.currentTarget,value=input.checked;input.disabled=true;try{Object.assign(app,await api('apps/'+app.id,{auto_deploy:value},'PATCH'));const field=$('#settings-form [name="auto_deploy"]');if(field)field.checked=value;toast('Auto-deploy '+(value?'enabled':'disabled'))}catch(err){input.checked=!value;toast(err.message)}finally{input.disabled=false}};
 $('#deploy').disabled=$('#rebuild-no-cache').disabled=$('#quick-auto-deploy').disabled=!project.git_url||project.source_conflict;
 $('#redeploy').disabled=!app.active_deployment;
 async function queueRelease(button,action,body={}){button.disabled=true;try{const d=await api('apps/'+app.id+'/'+action,body);selectedDeployment=d.id;toast(action==='redeploy'?'Redeploy queued':'Deployment queued');if(isCurrent())await tab('deployments',true)}catch(err){toast(err.message)}finally{if(button.isConnected)button.disabled=false}}
 $('#deploy').onclick=e=>queueRelease(e.currentTarget,'deploy');
 $('#rebuild-no-cache').onclick=e=>queueRelease(e.currentTarget,'deploy',{no_cache:true});
 $('#redeploy').onclick=e=>queueRelease(e.currentTarget,'redeploy');
 async function showDeployments(){
  if(loading)return;loading=true;
  try{
   const ds=await api('deployments?app_id='+app.id);if(!isCurrent()||activeTab!=='deployments')return;
   const previousLog=$('#build-log'),previousScroll=previousLog?.scrollTop||0,logHeight=previousLog?.style.height||'',follow=!previousLog||previousLog.scrollHeight-previousLog.scrollTop-previousLog.clientHeight<40;
   const left=$('#deployment-history')?.scrollLeft||0,top=$('#deployment-history')?.scrollTop||0,focused=document.activeElement?.closest('.select-deploy')?.dataset.id,focusedControl=document.activeElement?.id;
   const latest=ds.slice().reverse();if(!latest.some(d=>d.id===selectedDeployment))selectedDeployment=latest[0]?.id||'';
   $('#tab-content').innerHTML='<div class="deployment-layout"><section class="release-history"><div class="section-head"><h2>Releases<span class="count">'+latest.length+'</span></h2><span class="small muted">Select a release to inspect</span></div>'+
    (latest.length?'<div class="panel table-panel" id="deployment-history"><table><thead><tr><th>Deployment</th><th>Status</th><th>Commit</th><th>Created</th></tr></thead><tbody>'+latest.map(d=>'<tr class="'+(d.id===selectedDeployment?'selected':'')+'"><td><button class="select-deploy" aria-label="Inspect deployment '+esc(d.id.slice(0,8))+'" aria-pressed="'+(d.id===selectedDeployment)+'" data-id="'+esc(d.id)+'">'+icon('box')+esc(d.id.slice(0,8))+'</button></td><td>'+badge(d.status)+'</td><td class="muted commit">'+esc(d.commit?.slice(0,7)||'Pending')+'</td><td class="muted">'+esc(new Date(d.created).toLocaleString())+'</td></tr>').join('')+'</tbody></table></div>':empty('No releases yet','Deploy this application to see its build output.'))+
    '<div class="release-endpoints"><div class="section-head"><h2>Public endpoints</h2><button class="text-button" id="manage-domains">'+(ownRoutes().length?'Manage':'Connect domain')+icon('arrow')+'</button></div>'+
    (ownRoutes().length?ownRoutes().map(r=>'<a class="endpoint" href="'+esc(r.url)+'" target="_blank" rel="noreferrer">'+icon('globe')+'<span>'+esc(r.url)+'</span>'+icon('arrow')+'</a>').join(''):'<p class="small muted">No domain connected to this application.</p>')+'</div></section>'+
    '<section class="panel log-panel '+(expandedLog?'expanded':'')+'"><div class="log-head"><span class="log-title">'+icon('terminal')+'Build output <span class="muted" id="log-id"></span></span><div class="log-actions"><button class="text-button" id="redeploy-release" disabled>Redeploy this release</button><button class="text-button" id="toggle-log" aria-expanded="'+expandedLog+'" aria-controls="build-log">'+icon('expand')+(expandedLog?'Compact':'Expand')+'</button><button class="text-button" id="copy-log">'+icon('copy')+'Copy</button></div></div><pre class="log" id="build-log">Waiting for a deployment…</pre></section></div>';
   const table=$('#deployment-history');if(table){table.scrollLeft=left;table.scrollTop=top}
   if(logHeight)$('#build-log').style.height=logHeight;
   document.querySelectorAll('.select-deploy').forEach(b=>b.onclick=async()=>{selectedDeployment=b.dataset.id;try{await showDeployments()}catch(err){toast(err.message)}});
   if(focused)document.querySelector('.select-deploy[data-id="'+focused+'"]')?.focus({preventScroll:true});
   if(['copy-log','toggle-log','manage-domains','redeploy-release'].includes(focusedControl))$('#'+focusedControl)?.focus({preventScroll:true});
   $('#toggle-log').onclick=e=>{expandedLog=!expandedLog;$('.log-panel').classList.toggle('expanded',expandedLog);$('#build-log').style.height='';e.currentTarget.setAttribute('aria-expanded',String(expandedLog));e.currentTarget.innerHTML=icon('expand')+(expandedLog?'Compact':'Expand')};
   $('#manage-domains').onclick=()=>ownRoutes().length?tab('domains',true).catch(err=>toast(err.message)):addRoute(project,apps,app.id);
   $('#copy-log').onclick=async()=>{try{await navigator.clipboard.writeText($('#build-log').textContent);toast('Build output copied')}catch{toast('Select and copy the build output manually')}};
   const current=latest.find(d=>['queued','building','testing','starting'].includes(d.status))||latest.find(d=>d.status==='running')||latest[0];
   $('#active-status').innerHTML=badge(current?.status);
   $('#redeploy').disabled=!latest.some(d=>d.status==='running');
   if(selectedDeployment){const d=await api('deployments/'+selectedDeployment);if(!isCurrent()||activeTab!=='deployments')return;$('#build-log').textContent=d.log||'Deployment '+d.status+'…';$('#log-id').textContent=' / '+d.id.slice(0,8);$('#build-log').scrollTop=follow?$('#build-log').scrollHeight:previousScroll;$('#redeploy-release').disabled=!d.redeployable;$('#redeploy-release').onclick=e=>queueRelease(e.currentTarget,'redeploy',{deployment_id:d.id})}
  }finally{loading=false}
 }
 function showDomains(){
  const routes=ownRoutes();
  $('#tab-content').innerHTML='<div class="section-head"><div><h2>Domains & routes<span class="count">'+routes.length+'</span></h2><span class="small muted">Traffic connected to '+esc(app.name)+'</span></div><button class="primary" id="add-route">'+icon('plus')+'Connect domain</button></div>'+
   (routes.length?'<div class="panel table-panel"><table><thead><tr><th>Public router URL</th><th>Internal port</th><th>Path prefix</th><th>Forwarding</th><th>Actions</th></tr></thead><tbody>'+routes.map(r=>'<tr><td><a href="'+esc(r.url)+'" target="_blank" rel="noreferrer">'+esc(r.url)+' '+icon('arrow')+'</a></td><td>:'+app.port+'</td><td>'+esc(r.path_prefix)+'</td><td>'+(r.strip_prefix?'Strip prefix':'Preserve prefix')+'</td><td><button class="danger delete-route" data-id="'+esc(r.id)+'" aria-label="Delete route '+esc(r.url)+'">Delete</button></td></tr>').join('')+'</tbody></table></div>':empty('Give this app an address','Connect a hostname and path prefix to route public traffic here.'))+
   '<div class="note">Point your domain’s A record to this server’s public IP. Routes use the public router address, not the internal container port.</div>';
  $('#add-route').onclick=()=>addRoute(project,apps,app.id);
  document.querySelectorAll('.delete-route').forEach(b=>b.onclick=async()=>{b.disabled=true;try{await api('routes/'+b.dataset.id,null,'DELETE');await render();toast('Route deleted')}catch(err){b.disabled=false;toast(err.message)}});
 }
 function showSettings(){
  $('#tab-content').innerHTML='<form id="settings-form" class="settings-form"><div class="section-head"><div><h2>Application settings</h2><span class="small muted">Changes apply on the next deployment.</span></div><button class="primary" type="submit">Save changes</button></div>'+appFields(app,true)+'<div class="form-error" role="alert"></div></form>';
  $('#settings-form').onsubmit=async e=>{e.preventDefault();const button=e.target.querySelector('[type="submit"]');button.disabled=true;try{Object.assign(app,await api('apps/'+app.id,appData(new FormData(e.target)),'PATCH'));await render();toast('Settings saved')}catch(err){if(isCurrent())$('#settings-form .form-error').textContent=err.message}finally{button.disabled=false}};
 }
 function showWebhook(){
  $('#tab-content').innerHTML=projectGitContent(project);bindProjectGit(project);
 }
 async function tab(name,updateURL=false){
  const token=++tabVersion,requested=applicationViews.some(t=>t[0]===name)?name:'deployments';
  activeTab=requested;clearInterval(timer);
  if(updateURL)history.replaceState(null,'','#app/'+app.id+'/'+activeTab);
  document.querySelectorAll('.sidebar-navigation a').forEach(a=>{const current=a.hash==='#app/'+app.id+'/'+activeTab;a.classList.toggle('active',current);if(current)a.setAttribute('aria-current','page');else a.removeAttribute('aria-current')});
  $('#view-title').textContent=applicationViews.find(v=>v[0]===activeTab)[2];
  $('#poll-status').hidden=activeTab!=='deployments';
  if(requested==='deployments'){await showDeployments();if(token===tabVersion&&isCurrent()&&activeTab==='deployments')timer=setInterval(()=>showDeployments().catch(err=>{if(isCurrent())toast(err.message)}),2000)}
  if(requested==='domains')showDomains();
  if(requested==='settings')showSettings();
  if(requested==='webhook')showWebhook();
 }
 $('#active-status').innerHTML=badge(deploymentStatus(app,deployments)||deployments.filter(d=>d.app_id===app.id).at(-1)?.status);
 await tab(initialTab);
}
function collectionCard(item,kind,apps=[],deployments=[]){
 const current=kind==='workspace'&&workspace?.id===item.id,projectApps=apps.filter(a=>a.project_id===item.id);
 const created=item.created?new Date(item.created).toLocaleDateString(undefined,{month:'short',day:'numeric'}):'';
 if(kind==='project'){
  const running=projectApps.filter(a=>deploymentStatus(a,deployments)==='running').length;
  const detail=projectApps.length+' app'+(projectApps.length===1?'':'s')+(projectApps.length?' · '+[...new Set(projectApps.map(a=>a.build_type))].map(esc).join(', '):' · Connect a repository');
  return '<a class="project-row" data-project-name="'+esc(item.name.toLowerCase())+'" href="#project/'+esc(item.id)+'"><span class="card-icon">'+icon('grid')+'</span><span class="project-row-copy"><h3>'+esc(item.name)+'</h3><span>'+detail+'</span></span><span class="badge '+(running?'running':'idle')+'">'+(running?running+' running':projectApps.length?'Not deployed':'No apps')+'</span>'+icon('arrow')+'</a>';
 }
 return '<a class="card link" href="#workspace/'+esc(item.id)+'"><div class="card-top"><div class="card-icon">'+icon('layers')+'</div>'+(current?'<span class="badge running">Current</span>':'<span class="card-arrow">'+icon('arrow')+'</span>')+'</div><h3>'+esc(item.name)+'</h3><div class="card-footer"><span>'+(created?'Created '+esc(created):'Workspace')+'</span><span>Open workspace '+icon('arrow')+'</span></div></a>';
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
 workspaces=await api('workspaces');if(version!==viewVersion)return;workspace=workspaces.find(w=>w.id===parts[1])||workspaces.find(w=>w.id===localStorage.getItem('workspace'))||workspaces[0];
 if(!parts[0]&&workspace){nav('workspace/'+workspace.id);return}
 if(parts[0]==='project'){await projectPage(parts[1],'',parts[2]);return}
 if(parts[0]==='app'){await appPage(parts[1],parts[2]);return}
 if(parts[0]==='workspace'&&workspace){
  localStorage.setItem('workspace',workspace.id);
  const [projects,allApps,allDeployments]=await Promise.all([api('projects?workspace_id='+workspace.id),api('apps'),api('deployments')]);
  if(version!==viewVersion)return;
  const apps=allApps.filter(a=>projects.some(p=>p.id===a.project_id));
  const deployments=allDeployments.filter(d=>apps.some(a=>a.id===d.app_id));
  const running=apps.filter(a=>deployments.find(d=>d.id===a.active_deployment)?.status==='running').length;
  shell(esc(workspace.name),
   heading('Workspace overview','Projects','Your applications, from first commit to latest release.','<button id="new-project" class="primary">'+icon('plus')+'New project</button>')+
   '<div class="stats"><div class="stat"><div><span>Total projects</span><strong>'+projects.length+'</strong></div>'+icon('grid')+'</div><div class="stat"><div><span>Applications</span><strong>'+apps.length+'</strong></div>'+icon('box')+'</div><div class="stat stat-positive"><div><span>Running applications</span><strong>'+running+'</strong></div>'+icon('activity')+'</div></div>'+
   '<div class="workspace-layout"><section><div class="section-head"><h2>All projects<span class="count">'+projects.length+'</span></h2><label class="search-field">'+icon('search')+'<input type="search" id="project-search" placeholder="Find a project…" aria-label="Find a project"></label></div>'+
   (projects.length?'<div class="panel project-directory">'+projects.map(p=>collectionCard(p,'project',apps,deployments)).join('')+'</div><p class="note" id="no-project-matches" hidden>No projects match your search.</p>':empty('No projects yet','Create a project, then connect your first repository.'))+'</section>'+releasePanel(deployments,apps)+'</div>');
  $('#project-search').oninput=e=>{let matches=0;document.querySelectorAll('[data-project-name]').forEach(a=>{a.hidden=!a.dataset.projectName.includes(e.target.value.trim().toLowerCase());if(!a.hidden)matches++});if($('#no-project-matches'))$('#no-project-matches').hidden=matches>0};
  $('#new-project').onclick=createProject;return;
 }
 shell('Workspaces',
  heading('Infrastructure','Workspaces','Organize the projects running on your server.','<button class="primary" id="new-workspace">'+icon('plus')+'New workspace</button>')+
  '<div class="section-head"><h2>All workspaces<span class="count">'+workspaces.length+'</span></h2></div>'+
  (workspaces.length?'<div class="grid">'+workspaces.map(w=>collectionCard(w,'workspace')).join('')+'</div>':empty('No workspaces yet','Create a workspace to organize your projects.')));
 $('#new-workspace').onclick=createWorkspace;
 }catch(e){if(version===viewVersion)$('#root').innerHTML='<div class="error-page"><h1>Something went wrong.</h1><p>'+esc(e.message)+'</p><a class="button" href="#workspaces">Back to workspaces</a></div>'}
}
window.addEventListener('hashchange',render);render();
